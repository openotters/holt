package commands

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	c "github.com/merlindorin/go-shared/pkg/cmd"
	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/metadata"

	"github.com/openotters/holt/cmd/holt/internal/style"
	"github.com/openotters/holt/cmd/holt/internal/token"
	"github.com/openotters/holt/dial"
)

// Expose tunnels a LOCAL HTTP service through the hub — the
// ngrok-style "make localhost:3000 reachable" client. It joins a hub
// with a token from `holt enroll`, then reverse-proxies every request
// that arrives over the tunnel to the local address. The local service
// needs no changes and no public port.
type Expose struct {
	Target string `arg:"" help:"Local address to forward tunneled requests to, e.g. localhost:3000."`
	Token  string `help:"Join token from 'holt enroll' (or set HOLT_TOKEN)." required:""`
}

// Run attaches to the hub and serves the reverse proxy until the
// context is cancelled (Ctrl-C) or the hub sends a terminal GoAway.
func (e *Expose) Run(ctx context.Context, _ *c.Commons, logger *zap.Logger, out *style.Output) error {
	logger = logger.Named("expose")

	jt, err := token.Decode(e.Token)
	if err != nil {
		return err
	}

	proxy, targetURL, err := localProxy(e.Target)
	if err != nil {
		return err
	}

	cc, err := dialHub(jt)
	if err != nil {
		return err
	}
	defer func() { _ = cc.Close() }()

	if out.Pretty {
		fmt.Print(style.Banner("exposing "+targetURL.String(), []style.BannerRow{
			{Key: "peer", Value: jt.Peer, Hint: "your identity on the hub"},
			{Key: "hub", Value: jt.HubAddr, Hint: "attached over TLS, redials automatically"},
			{Key: "reach", Value: "curl -H 'x-tunnel-peer: " + jt.Peer + "'", Hint: "against the hub proxy address"},
		}, ""))
	} else {
		logger.Info("exposing local service over the tunnel",
			zap.String("peer", jt.Peer), zap.String("target", targetURL.String()))
	}

	err = dial.Run(ctx, dial.Options{
		Conn:    cc,
		Handler: proxy,
		Version: "holt-expose",
		Logger:  logger,
	})

	// Ctrl-C is a normal way to stop exposing, not an error to print.
	if errors.Is(err, context.Canceled) {
		return nil
	}

	return err
}

// localProxy builds a reverse proxy to the local target. A bare
// host:port is treated as http://host:port.
func localProxy(target string) (http.Handler, *url.URL, error) {
	if !strings.Contains(target, "://") {
		target = "http://" + target
	}

	u, err := url.Parse(target)
	if err != nil {
		return nil, nil, fmt.Errorf("invalid target %q: %w", target, err)
	}

	// Forward tunneled requests to the local target, keeping the
	// inbound path/query; the local server sees its own Host.
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(u)
			pr.Out.Host = u.Host
		},
	}

	return proxy, u, nil
}

// dialHub builds the TLS-pinned, JWT-authenticated connection to the hub.
func dialHub(jt token.JoinToken) (*grpc.ClientConn, error) {
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(jt.CAPEM) {
		return nil, fmt.Errorf("token carries an invalid hub certificate")
	}

	serverName := jt.HubAddr
	if host, _, splitErr := net.SplitHostPort(jt.HubAddr); splitErr == nil {
		serverName = host
	}

	creds := credentials.NewTLS(&tls.Config{
		RootCAs:    pool,
		ServerName: serverName,
		MinVersion: tls.VersionTLS13,
	})

	return grpc.NewClient(jt.HubAddr,
		grpc.WithTransportCredentials(creds),
		grpc.WithUnaryInterceptor(bearer(jt.JWT)),
		grpc.WithStreamInterceptor(bearerStream(jt.JWT)))
}

func bearer(jwt string) grpc.UnaryClientInterceptor {
	return func(
		ctx context.Context, method string, req, reply any,
		cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption,
	) error {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+jwt)

		return invoker(ctx, method, req, reply, cc, opts...)
	}
}

func bearerStream(jwt string) grpc.StreamClientInterceptor {
	return func(
		ctx context.Context, desc *grpc.StreamDesc, cc *grpc.ClientConn,
		method string, streamer grpc.Streamer, opts ...grpc.CallOption,
	) (grpc.ClientStream, error) {
		ctx = metadata.AppendToOutgoingContext(ctx, "authorization", "Bearer "+jwt)

		return streamer(ctx, desc, cc, method, opts...)
	}
}
