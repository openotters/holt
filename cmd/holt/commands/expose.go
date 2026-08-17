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
	"time"

	"connectrpc.com/connect"
	c "github.com/merlindorin/go-shared/pkg/cmd"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"

	holtv1 "github.com/openotters/holt/api/v1"

	"github.com/openotters/holt/cmd/holt/internal/httpsrv"
	"github.com/openotters/holt/cmd/holt/internal/hubmetrics"
	"github.com/openotters/holt/cmd/holt/internal/style"
	"github.com/openotters/holt/pkg/dial"
	"github.com/openotters/holt/pkg/peername"
	"github.com/openotters/holt/pkg/reqlog"
	"github.com/openotters/holt/pkg/revproxy"
	"github.com/openotters/holt/pkg/token"
)

// Expose tunnels a LOCAL HTTP service through the hub — the
// ngrok-style "make localhost:3000 reachable" client. It joins a hub
// with a token from `holt enroll`, then reverse-proxies every request
// that arrives over the tunnel to the local address. The local service
// needs no changes and no public port.
type Expose struct {
	Target string `arg:"" help:"Local address to forward tunneled requests to, e.g. localhost:3000."`

	// Without a token, expose enrolls itself against the configured
	// hub (--admin-url / --profile, same resolution as every other
	// command), so the common case is one command with no ceremony.
	Token string `help:"Join token from 'holt enroll' (or set HOLT_TOKEN). Omit it to enroll automatically."`
	Peer  string `help:"Peer name to enroll as when no token is given (default: a generated name like brisk-otter)."`

	// Local hop only: appliances (routers, NAS, IPMI) serve HTTPS with
	// a self-signed certificate no system root will verify, which the
	// proxy answers with a 502. This turns verification off for that
	// one hop; it never touches the tunnel or the hub.
	Insecure bool `help:"Skip TLS verification of an https target (self-signed appliances). Local hop only, never the tunnel." env:"HOLT_EXPOSE_INSECURE"`

	// The peer keeps its own instruments (attaches, failed attempts by
	// reason, session duration, attached gauge). They answer "is this
	// peer flapping" from the side that knows, which the hub cannot:
	// it only ever sees the attempts that reached it.
	Metrics     bool   `help:"Serve this peer's Prometheus metrics on /metrics."`
	MetricsAddr string `help:"Metrics listener address." default:"127.0.0.1:7210"`

	// --admin-url / --header / --profile / --config: where to enroll,
	// and the headers for whatever authenticates in front of the hub.
	// The same headers go out with the tunnel handshake.
	adminConn
}

// Run attaches to the hub and serves the reverse proxy until the
// context is cancelled (Ctrl-C) or the hub sends a terminal GoAway.
func (e *Expose) Run(ctx context.Context, commons *c.Commons, logger *zap.Logger, out *style.Output) error {
	logger = logger.Named("expose")

	rawToken, enrolled, err := e.resolveToken(ctx)
	if err != nil {
		return err
	}

	jt, err := token.Decode(rawToken)
	if err != nil {
		return err
	}

	if enrolled {
		logger.Info("enrolled automatically; pass --token to reuse an existing identity",
			zap.String("peer", jt.Peer))
	}

	local, targetURL, err := localProxy(e.Target, e.Insecure)
	if err != nil {
		return err
	}

	// Never let this be a quiet default: say it on every start,
	// whatever the log format, and say it again in the banner below.
	if e.Insecure {
		if targetURL.Scheme == schemeHTTPS {
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

	// With subdomain routing the peer has a URL of its own, which is
	// the thing an operator actually wants printed. Best effort: a
	// hub that cannot be asked just means one fewer banner row.
	peerURL := e.peerURL(ctx, jt.Peer)

	if out.Pretty {
		rows := []style.BannerRow{
			{Key: "peer", Value: jt.Peer, Hint: "your identity on the hub"},
			{Key: "hub", Value: jt.TunnelURL, Hint: "redials automatically"},
		}
		if peerURL != "" {
			rows = append(rows, style.BannerRow{Key: "url", Value: peerURL, Hint: "reachable now, no header needed"})
		}

		rows = append(rows, style.BannerRow{
			Key:   "reach",
			Value: "curl -H '" + revproxy.RouteHeader + ": " + jt.Peer + "'",
			Hint:  "against the hub proxy address",
		})
		if e.Insecure && targetURL.Scheme == schemeHTTPS {
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

	if e.Metrics {
		stop, mErr := e.serveMetrics(ctx, logger)
		if mErr != nil {
			return mErr
		}

		defer stop()
	}

	// The build version rides along so the console can flag peers
	// lagging behind the hub (plain "holt-expose" told it nothing).
	err = dial.Run(ctx, dial.Options{
		URL:     wsURL,
		Header:  header,
		Handler: local,
		Version: "holt-expose " + commons.Version.Version(),
		Logger:  logger,
		// What the tunnel carried, live, as it happens.
		RequestHook: requestPrinter(out, logger),
	})

	// Ctrl-C is a normal way to stop exposing, not an error to print.
	if errors.Is(err, context.Canceled) {
		return nil
	}

	return err
}

// requestPrinter is the live view of what the peer served. It goes
// through the logger, not around it: a request is one of this
// process's events like an attach or a redial, so it wears the same
// timestamp, level and prefix as the lines above and below it.
//
// Pretty renders the request in columns a human scans; JSON keeps it
// structured, since a collector wants fields rather than a rendered
// line.
func requestPrinter(out *style.Output, logger *zap.Logger) reqlog.Hook {
	if out.Pretty {
		return func(ev reqlog.Event) { logger.Info(style.Request(ev)) }
	}

	return func(ev reqlog.Event) {
		logger.Info("request served",
			zap.String("method", ev.Method), zap.String("path", ev.Path),
			zap.Int("status", ev.Status), zap.Duration("took", ev.Duration))
	}
}

// serveMetrics installs the OTel SDK provider and serves the peer's
// instruments, so a peer is observable on its own rather than only
// through whichever hub it reached.
func (e *Expose) serveMetrics(ctx context.Context, logger *zap.Logger) (func(), error) {
	mp, err := hubmetrics.Provider()
	if err != nil {
		return nil, err
	}

	// Installed before dial.Run builds its instruments, so they bind
	// to this provider rather than the global no-op.
	otel.SetMeterProvider(mp)

	mux := http.NewServeMux()
	mux.Handle("/metrics", hubmetrics.Handler())

	listeners := httpsrv.NewGroup(logger)
	if startErr := listeners.Start(ctx, "metrics", e.MetricsAddr, mux); startErr != nil {
		return nil, startErr
	}

	logger.Info("serving peer metrics", zap.String("addr", e.MetricsAddr))

	return func() {
		listeners.Drain(metricsDrain)
		_ = mp.Shutdown(context.Background())
	}, nil
}

// metricsDrain bounds how long the metrics listener gets to finish a
// scrape on shutdown.
const metricsDrain = 2 * time.Second

// resolveToken returns the join token to attach with: the one given,
// or a freshly minted one. enrolled reports which, so the caller can
// say so.
func (e *Expose) resolveToken(ctx context.Context) (string, bool, error) {
	if e.Token != "" {
		return e.Token, false, nil
	}

	peer := e.Peer
	if peer == "" {
		// The name carries its own uniqueness (a random suffix), so
		// exposing never needs to read the hub's other tunnels — a
		// client may not be allowed to, and should not have to be.
		generated, err := peername.Random()
		if err != nil {
			return "", false, err
		}

		peer = generated
	}

	enroll := &Enroll{Peer: peer, TokenTTL: defaultTokenTTL, adminConn: e.adminConn}

	tok, err := enroll.mint(ctx)
	if err != nil {
		return "", false, fmt.Errorf("enrolling %q: %w (pass --token, or point --admin-url/--profile at a hub)", peer, err)
	}

	return tok, true, nil
}

// defaultTokenTTL matches the hub's own default; it only applies when
// expose mints locally, since a remote hub uses its own --token-ttl.
const defaultTokenTTL = 24 * time.Hour

// schemeHTTPS is the target scheme that has a certificate to verify,
// which is what --insecure turns off.
const schemeHTTPS = "https"

// peerURL asks the hub how it routes, and returns this peer's own
// hostname when the answer is by subdomain. Best effort: any failure
// (no admin endpoint, an older hub, a header-routed hub) yields "".
func (e *Expose) peerURL(ctx context.Context, peer string) string {
	client, err := e.client()
	if err != nil {
		return ""
	}

	infoCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	resp, err := client.Info(infoCtx, connect.NewRequest(&holtv1.InfoRequest{}))
	if err != nil {
		return ""
	}

	domain := resp.Msg.GetProxyDomain()
	if domain == "" {
		return ""
	}

	return schemeHTTPS + "://" + peer + "." + domain + "/"
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
	local := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(u)
			pr.Out.Host = u.Host
		},
	}

	// Only an https target has a certificate to skip; leaving http
	// and the verifying path on the default transport keeps the
	// change to exactly the hop the operator asked for.
	if insecure && u.Scheme == schemeHTTPS {
		local.Transport = insecureTransport()
	}

	return local, u, nil
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
