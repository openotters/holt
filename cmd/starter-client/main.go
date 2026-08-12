// Command starter-client is a minimal, self-contained holt peer —
// a template to copy when writing your own client. It joins a hub with
// a token from `holt enroll`, serves one HTTP handler over the
// tunnel, and listens on nothing.
//
// Everything a peer needs is here and nothing else: decode the token,
// dial the hub (JWT auth; the tunnel URL's scheme picks the transport),
// and dial.Run your handler. The join token is decoded inline (a tiny
// base64+JSON struct) so this file has no dependency on the holt CLI's
// internals — copy it and go.
//
//	# on the hub machine:
//	holt hub &
//	holt enroll myservice        # prints a token
//
//	# anywhere:
//	go run ./cmd/starter-client --token <paste>
//
//	# reach it through the hub:
//	curl -H 'x-tunnel-peer: myservice' http://localhost:7002/
package main

import (
	"context"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/metadata"

	"github.com/openotters/holt/dial"
)

// joinToken mirrors what `holt enroll` prints: the hub's tunnel URL and
// the peer's JWT. Kept inline so this starter has no internal imports.
type joinToken struct {
	Peer      string `json:"peer"`
	TunnelURL string `json:"tunnel_url"`
	JWT       string `json:"jwt"`
}

func main() {
	tok := flag.String("token", "", "join token from `holt enroll` (required)")
	flag.Parse()

	if *tok == "" {
		log.Fatal("--token is required (run `holt enroll <name>` to get one)")
	}

	if err := run(*tok); err != nil {
		log.Fatal(err)
	}
}

func run(rawToken string) error {
	jt, err := decodeToken(rawToken)
	if err != nil {
		return err
	}

	logger, _ := zap.NewDevelopment()
	defer func() { _ = logger.Sync() }()

	// ── 1. Resolve the tunnel URL. https dials standard TLS (verified
	//      with the system roots, so it works through a TLS edge like
	//      Cloudflare or an ingress); http dials plaintext h2c. Transport
	//      encryption is the deployment's job — the token has no cert. ──
	addr, serverName, useTLS, err := target(jt.TunnelURL)
	if err != nil {
		return err
	}

	var creds credentials.TransportCredentials
	if useTLS {
		creds = credentials.NewTLS(&tls.Config{ServerName: serverName, MinVersion: tls.VersionTLS12})
	} else {
		creds = insecure.NewCredentials()
	}

	// ── 2. Dial the hub, presenting the JWT on every call. ──────────────
	cc, err := grpc.NewClient(addr,
		grpc.WithTransportCredentials(creds),
		grpc.WithUnaryInterceptor(bearer(jt.JWT)),
		grpc.WithStreamInterceptor(bearerStream(jt.JWT)))
	if err != nil {
		return err
	}
	defer func() { _ = cc.Close() }()

	// ── 3. The handler you expose over the tunnel. Replace with yours. ──
	handler := http.NewServeMux()
	handler.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintf(w, "hello from %s — reached through the tunnel\n", jt.Peer)
	})

	// ── 4. Attach and serve until interrupted (auto-redials). ───────────
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("joined; serving over the tunnel", zap.String("peer", jt.Peer))

	return dial.Run(ctx, dial.Options{
		Conn:    cc,
		Handler: handler,
		Version: "starter-client",
		Logger:  logger,
	})
}

func decodeToken(s string) (joinToken, error) {
	raw, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		return joinToken{}, fmt.Errorf("token is not valid base64: %w", err)
	}

	var jt joinToken
	if unmarshalErr := json.Unmarshal(raw, &jt); unmarshalErr != nil {
		return joinToken{}, fmt.Errorf("token payload is invalid: %w", unmarshalErr)
	}

	return jt, nil
}

// target resolves the tunnel URL into a gRPC dial address, the TLS
// server name to verify, and whether to use TLS. https -> TLS (system
// roots); http -> plaintext h2c.
func target(raw string) (string, string, bool, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", "", false, fmt.Errorf("invalid tunnel_url %q: %w", raw, err)
	}

	var useTLS bool

	switch u.Scheme {
	case "https":
		useTLS = true
	case "http":
		useTLS = false
	default:
		return "", "", false, fmt.Errorf("tunnel_url scheme must be http or https, got %q", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return "", "", false, fmt.Errorf("tunnel_url has no host: %q", raw)
	}

	port := u.Port()
	if port == "" {
		if useTLS {
			port = "443"
		} else {
			port = "80"
		}
	}

	return net.JoinHostPort(host, port), host, useTLS, nil
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
