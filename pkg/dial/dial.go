// Package dial is the client half of the holt: a persistent
// attach loop that dials the hub over a WebSocket, serves an
// http.Handler over the reverse tunnel, and redials with jittered
// backoff until the context ends or the hub sends a terminal GoAway.
//
// The WebSocket is the tunnel's carrier because it passes through
// what gRPC cannot: CDN public hostnames (Cloudflare included),
// access proxies, and HTTP/1.1-only edges. wss:// puts TLS under the
// socket exactly like https; extra headers on the upgrade request
// carry whatever the edge in front of the hub wants (a bearer token,
// a Cloudflare Access service token, …).
package dial

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math/rand/v2"
	"net"
	"net/http"
	"net/url"
	"time"

	"github.com/coder/websocket"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
	"golang.org/x/net/http2"

	"github.com/openotters/holt/internal/wire"
	"github.com/openotters/holt/pkg/tunneltype"
)

const (
	backoffBase = 500 * time.Millisecond
	backoffCap  = 30 * time.Second

	// readIdleTimeout makes the inner HTTP/2 server ping through the
	// tunnel so a wedged hub-side session is detected end-to-end.
	readIdleTimeout = 30 * time.Second

	// DefaultKeepalive paces WebSocket-level pings from the peer.
	// Proxies drop idle sockets (Cloudflare after ~100 s of silence);
	// pinging well under that keeps a quiet tunnel attached.
	DefaultKeepalive = 40 * time.Second
)

// Options wires one attach loop.
type Options struct {
	// URL is the hub's tunnel endpoint: ws:// (plaintext) or wss://
	// (TLS to the edge, system roots). http:// and https:// are
	// accepted as aliases so pre-WebSocket tunnel URLs keep working.
	URL string

	// Header goes out with the WebSocket upgrade request: the
	// Authorization bearer the hub's middleware verifies, plus
	// anything an authenticating proxy in front wants (e.g.
	// CF-Access-Client-Id / CF-Access-Client-Secret).
	Header http.Header

	// HTTPClient overrides the client used for the upgrade request
	// (custom roots, proxies). Default: a private client with its own
	// connection pool. Between redial attempts Run closes the
	// client's idle connections so every attempt dials fresh.
	HTTPClient *http.Client

	// Keepalive paces WebSocket pings; 0 means DefaultKeepalive, a
	// negative value disables them (the inner HTTP/2 PING remains).
	Keepalive time.Duration

	// Handler is served over the tunnel — the hub dials it as if it
	// were a normal HTTP server.
	Handler http.Handler
	// Version is the peer build version, sent in Hello (observability).
	Version string
	Logger  *zap.Logger

	// MeterProvider builds the peer's own instruments (attaches,
	// failed attempts by reason, session duration, and a gauge that
	// is 1 while a tunnel is up). Optional: without it the global
	// provider is used, a no-op until an SDK is installed.
	MeterProvider metric.MeterProvider

	// TunnelType is what this peer carries, declared at attach so the
	// hub records it, reports it, and can refuse what it cannot serve.
	// Empty means http, or https when TLSConfig is set: the payload is
	// TLS end to end then, which is what https names here.
	TunnelType tunneltype.Type

	// TLSConfig, when set, encrypts the payload end-to-end INSIDE the
	// tunnel: after the plaintext holt handshake the peer runs a
	// TLS server over the stream and serves Handler over HTTPS. The
	// hub must dial with a matching client config (hub.WithPeerTLS).
	// This is independent of any TLS on the outer WebSocket — it
	// stays encrypted even if that hop is plaintext or terminated
	// at a proxy. NextProtos is forced to h2.
	TLSConfig *tls.Config
}

// tunnelType is what this peer declares at attach: the explicit
// setting, or https when the payload is encrypted end to end and http
// otherwise.
func (o Options) tunnelType() tunneltype.Type {
	if o.TunnelType != "" {
		return o.TunnelType
	}

	if o.TLSConfig != nil {
		return tunneltype.HTTPS
	}

	return tunneltype.HTTP
}

// NormalizeURL maps a tunnel URL to its WebSocket form: ws and wss
// pass through, http becomes ws and https becomes wss (so tokens
// minted before the WebSocket carrier keep working). Any other
// scheme, or a missing host, is an error.
func NormalizeURL(raw string) (string, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("holt: invalid tunnel URL %q: %w", raw, err)
	}

	switch u.Scheme {
	case "ws", "wss":
	case "http":
		u.Scheme = "ws"
	case "https":
		u.Scheme = "wss"
	default:
		return "", fmt.Errorf("holt: tunnel URL scheme must be ws, wss, http, or https, got %q", u.Scheme)
	}

	if u.Host == "" {
		return "", fmt.Errorf("holt: tunnel URL has no host: %q", raw)
	}

	return u.String(), nil
}

// Run attaches to the hub and serves Handler over the tunnel,
// redialing with jittered exponential backoff (500 ms doubling to
// 30 s) until ctx is cancelled or the hub sends a terminal GoAway. A
// successful handshake resets the backoff. Returns nil on a terminal
// GoAway (clean exit) and ctx.Err() on shutdown.
func Run(ctx context.Context, opts Options) error {
	logger := opts.Logger.Named("holt-dial")

	wsURL, urlErr := NormalizeURL(opts.URL)
	if urlErr != nil {
		return urlErr
	}

	// The upgrade client gets its own connection pool by default so
	// closing idle connections below never touches unrelated traffic
	// on http.DefaultClient's shared pool.
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Transport: newTransport()}
	}

	obs := newMetrics(opts.MeterProvider)
	backoff := backoffBase

	for {
		attached, err := attachOnce(ctx, wsURL, opts, obs, logger)

		if ctx.Err() != nil {
			return ctx.Err()
		}

		if reason := wire.GoAwayReason(err); wire.TerminalReason(reason) {
			logger.Info("hub detached the tunnel; not redialing", zap.String("reason", reason))

			return nil
		}

		if attached {
			backoff = backoffBase
		}

		// Drop pooled keep-alive connections so the next attempt dials
		// fresh. A pooled connection can outlive the endpoint it was
		// good for — a hub that restarted, or another process that
		// answered the port while the hub was down — and would pin
		// every redial to that dead or wrong peer.
		opts.HTTPClient.CloseIdleConnections()

		logger.Warn("tunnel detached; redialing",
			zap.Error(err), zap.Duration("backoff", backoff))

		//nolint:gosec // G404: jitter, not crypto
		sleep := backoff/2 + rand.N(backoff/2+1)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(sleep):
		}

		backoff = min(backoff*2, backoffCap)
	}
}

// newTransport is the dialer's private pool: http.DefaultTransport's
// shape, but owned here.
func newTransport() *http.Transport {
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		return t.Clone()
	}

	return &http.Transport{}
}

// attachOnce performs one attach: WebSocket dial, handshake, then
// serves Handler over the stream until it ends. attached reports
// whether the handshake completed (used to reset backoff).
func attachOnce(
	ctx context.Context, wsURL string, opts Options, obs *metrics, logger *zap.Logger,
) (bool, error) {
	c, resp, err := websocket.Dial(ctx, wsURL, &websocket.DialOptions{
		HTTPHeader: opts.Header,
		HTTPClient: opts.HTTPClient,
	})
	if err != nil {
		if resp != nil {
			if resp.Body != nil {
				_ = resp.Body.Close()
			}

			// The status is the story: 401/403 is the hub (or the
			// access layer in front) refusing the credential, 3xx is
			// usually an auth wall bouncing to a login page.
			obs.recordFailure(ctx, reasonDial)

			return false, fmt.Errorf("holt: websocket dial: %w (http %d)", err, resp.StatusCode)
		}

		obs.recordFailure(ctx, reasonDial)

		return false, fmt.Errorf("holt: websocket dial: %w", err)
	}
	defer func() { _ = c.CloseNow() }()

	fs := wire.NewWSStream(ctx, c)

	if hsErr := wire.ClientHandshake(fs, opts.Version, opts.tunnelType().Proto()); hsErr != nil {
		obs.recordFailure(ctx, reasonHandshake)

		return false, hsErr
	}

	// Attached: mark it up, and record how long it lasts, whichever
	// way the session ends below.
	attachedAt := time.Now()

	obs.recordAttach(ctx)

	defer func() { obs.recordDetach(ctx, attachedAt) }()

	logger.Info("tunnel attached")

	conn := wire.NewConn(fs,
		wire.WithCloseFunc(func() error { return c.Close(websocket.StatusNormalClosure, "") }),
		wire.WithSides("peer", "hub"))

	stopPings := startKeepalive(ctx, c, opts.Keepalive, logger)
	defer stopPings()

	// Optional payload encryption: wrap the raw tunnel in a TLS
	// server and complete the handshake before serving h2 over it.
	// The handshake is driven explicitly (not lazily by ServeConn) so
	// it rendezvous deterministically with the hub's client-side
	// handshake — ServeConn writes the server preface first, which
	// would otherwise race the TLS record layer. The holt
	// handshake above already completed in plaintext; TLS protects
	// the payload, not the tunnel framing.
	var served net.Conn = conn
	if opts.TLSConfig != nil {
		tlsCfg := opts.TLSConfig.Clone()
		tlsCfg.NextProtos = []string{"h2"}
		tlsConn := tls.Server(conn, tlsCfg)

		if hsErr := tlsConn.HandshakeContext(ctx); hsErr != nil {
			return true, fmt.Errorf("holt: tunnel TLS handshake: %w", hsErr)
		}

		served = tlsConn
	}

	srv := &http2.Server{ReadIdleTimeout: readIdleTimeout}
	srv.ServeConn(served, &http2.ServeConnOpts{
		Context: ctx,
		Handler: opts.Handler,
	})

	if lastErr := conn.LastError(); lastErr != nil {
		return true, lastErr
	}

	return true, errors.New("holt: session ended")
}

// startKeepalive pings the WebSocket every interval so proxies with
// idle timeouts (Cloudflare ~100 s) keep the carrier open through
// quiet stretches. A failed ping closes the socket, which unblocks
// the session above into a redial. The returned stop func ends the
// loop; it also exits when ctx does.
func startKeepalive(ctx context.Context, c *websocket.Conn, every time.Duration, logger *zap.Logger) func() {
	if every < 0 {
		return func() {}
	}

	if every == 0 {
		every = DefaultKeepalive
	}

	pingCtx, cancel := context.WithCancel(ctx)

	go func() {
		ticker := time.NewTicker(every)
		defer ticker.Stop()

		for {
			select {
			case <-pingCtx.Done():
				return
			case <-ticker.C:
				oneCtx, oneCancel := context.WithTimeout(pingCtx, every)
				err := c.Ping(oneCtx)
				oneCancel()

				if err != nil {
					if pingCtx.Err() == nil {
						logger.Warn("websocket keepalive failed; closing carrier", zap.Error(err))
						_ = c.CloseNow()
					}

					return
				}
			}
		}
	}()

	return cancel
}
