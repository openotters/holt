package commands

import (
	"context"
	"crypto/tls"
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

	// Local hop only: appliances (routers, NAS, IPMI) serve HTTPS with
	// a self-signed certificate no system root will verify, which the
	// proxy answers with a 502. This turns verification off for that
	// one hop; it never touches the tunnel or the hub.
	Insecure bool `help:"Skip TLS verification of an https target (self-signed appliances). Local hop only, never the tunnel." env:"HOLT_EXPOSE_INSECURE"`
}

// Run attaches to the hub and serves the reverse proxy until the
// context is cancelled (Ctrl-C) or the hub sends a terminal GoAway.
func (e *Expose) Run(ctx context.Context, commons *c.Commons, logger *zap.Logger, out *style.Output) error {
	logger = logger.Named("expose")

	jt, err := token.Decode(e.Token)
	if err != nil {
		return err
	}

	proxy, targetURL, err := localProxy(e.Target, e.Insecure)
	if err != nil {
		return err
	}

	// Never let this be a quiet default: say it on every start,
	// whatever the log format, and say it again in the banner below.
	if e.Insecure {
		if targetURL.Scheme == "https" {
			logger.Warn("TLS verification disabled for the target; anyone on the path to it can read or alter the traffic",
				zap.String("target", targetURL.String()))
		} else {
			logger.Warn("--insecure has no effect on a plaintext target",
				zap.String("target", targetURL.String()))
		}
	}

	header, err := dialHeader(jt, e.Header)
	if err != nil {
		return err
	}

	if out.Pretty {
		rows := []style.BannerRow{
			{Key: "peer", Value: jt.Peer, Hint: "your identity on the hub"},
			{Key: "hub", Value: jt.TunnelURL, Hint: "redials automatically"},
			{Key: "reach", Value: "curl -H 'x-tunnel-peer: " + jt.Peer + "'", Hint: "against the hub proxy address"},
		}
		if e.Insecure && targetURL.Scheme == "https" {
			rows = append(rows, style.BannerRow{
				Key: "tls", Value: "NOT verified", Hint: "--insecure: the target's certificate is trusted blindly",
			})
		}

		fmt.Print(style.Banner("exposing "+targetURL.String(), rows, ""))
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
// host:port is treated as http://host:port. With insecure set, an
// https target's certificate is not verified (see insecureTransport).
func localProxy(target string, insecure bool) (http.Handler, *url.URL, error) {
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

	// Only an https target has a certificate to skip; leaving http
	// and the verifying path on the default transport keeps the
	// change to exactly the hop the operator asked for.
	if insecure && u.Scheme == "https" {
		proxy.Transport = insecureTransport()
	}

	return proxy, u, nil
}

// insecureTransport clones the default transport (keeping its proxy
// support, timeouts, and pooling) and turns off certificate
// verification for the target hop. This is the --insecure flag's only
// effect: the tunnel to the hub, and the hub's own TLS, are dialled
// elsewhere and stay verified.
func insecureTransport() *http.Transport {
	base, ok := http.DefaultTransport.(*http.Transport)
	if !ok {
		base = &http.Transport{}
	}

	tr := base.Clone()
	//nolint:gosec // G402: the point of --insecure, opt-in per invocation and warned about at startup.
	tr.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}

	return tr
}
