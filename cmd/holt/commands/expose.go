package commands

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	c "github.com/merlindorin/go-shared/pkg/cmd"
	"go.uber.org/zap"

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
	Target string   `arg:"" help:"Local address to forward tunneled requests to, e.g. localhost:3000."`
	Token  string   `help:"Join token from 'holt enroll' (or set HOLT_TOKEN)." required:""`
	Header []string `help:"Extra header 'Name: Value' sent with the WebSocket handshake (repeatable) — e.g. a Cloudflare Access service token in front of the tunnel hostname." name:"header"`
}

// Run attaches to the hub and serves the reverse proxy until the
// context is cancelled (Ctrl-C) or the hub sends a terminal GoAway.
func (e *Expose) Run(ctx context.Context, commons *c.Commons, logger *zap.Logger, out *style.Output) error {
	logger = logger.Named("expose")

	jt, err := token.Decode(e.Token)
	if err != nil {
		return err
	}

	proxy, targetURL, err := localProxy(e.Target)
	if err != nil {
		return err
	}

	header, err := dialHeader(jt, e.Header)
	if err != nil {
		return err
	}

	if out.Pretty {
		fmt.Print(style.Banner("exposing "+targetURL.String(), []style.BannerRow{
			{Key: "peer", Value: jt.Peer, Hint: "your identity on the hub"},
			{Key: "hub", Value: jt.TunnelURL, Hint: "redials automatically"},
			{Key: "reach", Value: "curl -H 'x-tunnel-peer: " + jt.Peer + "'", Hint: "against the hub proxy address"},
		}, ""))
	} else {
		logger.Info("exposing local service over the tunnel",
			zap.String("peer", jt.Peer), zap.String("target", targetURL.String()))
	}

	wsURL, err := jt.WSURL()
	if err != nil {
		return err
	}

	// The build version rides along so the console can flag peers
	// lagging behind the hub (plain "holt-expose" told it nothing).
	err = dial.Run(ctx, dial.Options{
		URL:     wsURL,
		Header:  header,
		Handler: proxy,
		Version: "holt-expose " + commons.Version.Version(),
		Logger:  logger,
	})

	// Ctrl-C is a normal way to stop exposing, not an error to print.
	if errors.Is(err, context.Canceled) {
		return nil
	}

	return err
}

// dialHeader assembles the WebSocket upgrade headers: the peer's JWT
// as the bearer the hub verifies, plus any operator-supplied headers
// for whatever sits in front of the tunnel hostname.
func dialHeader(jt token.JoinToken, extra []string) (http.Header, error) {
	header := http.Header{}
	header.Set("Authorization", "Bearer "+jt.JWT)

	for _, h := range extra {
		name, value, ok := splitHeader(h)
		if !ok {
			return nil, fmt.Errorf("invalid --header %q, want 'Name: Value'", h)
		}

		header.Set(name, value)
	}

	return header, nil
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
