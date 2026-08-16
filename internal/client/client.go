package client

import (
	"context"
	"crypto/tls"
	"net/http"
	"time"

	"go.uber.org/zap"

	"github.com/openotters/holt/internal/dial"
)

// Client is the assembled peer: a persistent attach loop that dials
// the hub and serves an http.Handler back through the tunnel,
// redialing with jittered backoff. It is NewServer's counterpart for
// the client side — a thin veneer over the dial package's attach
// loop.
//
//	c := holt.NewClient("wss://hub.example.com/attach", handler,
//		holt.WithBearerToken(token),
//	)
//
//	err := c.Run(ctx) // attaches, serves, redials; cancel ctx to stop
type Client struct {
	opts dial.Options
}

// Option configures a Client; every SharedOption is one too.
type Option interface{ ApplyClient(*Client) }

// clientOption implements Option.
type clientOption func(*Client)

func (f clientOption) ApplyClient(c *Client) { f(c) }

// New wires a peer that attaches to the hub's tunnel endpoint at
// hubURL — ws:// or wss:// (http/https are accepted as aliases) —
// and serves handler over the tunnel, as if it were a normal HTTP
// server. Nothing dials until Run.
func New(hubURL string, handler http.Handler, opts ...Option) *Client {
	c := &Client{opts: dial.Options{
		URL:     hubURL,
		Handler: handler,
		Logger:  zap.NewNop(),
	}}

	for _, opt := range opts {
		opt.ApplyClient(c)
	}

	return c
}

// WithLogger sets the logger. Default: no logging.
func WithLogger(logger *zap.Logger) Option {
	return clientOption(func(c *Client) { c.opts.Logger = logger })
}

// Run attaches to the hub and serves the handler over the tunnel,
// redialing with jittered exponential backoff until ctx is cancelled
// or the hub sends a terminal GoAway. Returns nil on a terminal
// GoAway (clean exit) and ctx.Err() on shutdown.
func (c *Client) Run(ctx context.Context) error { return dial.Run(ctx, c.opts) }

// WithBearerToken sends token as the Authorization bearer on the
// attach request — the counterpart of the server's WithAuthBearer.
func WithBearerToken(token string) Option {
	return clientOption(func(c *Client) { c.header().Set("Authorization", "Bearer "+token) })
}

// WithHeader adds a header to the WebSocket upgrade request —
// whatever the edge in front of the hub wants (e.g.
// CF-Access-Client-Id / CF-Access-Client-Secret). Repeatable.
func WithHeader(key, value string) Option {
	return clientOption(func(c *Client) { c.header().Add(key, value) })
}

// header returns the upgrade-request headers, allocated on first use.
func (c *Client) header() http.Header {
	if c.opts.Header == nil {
		c.opts.Header = http.Header{}
	}

	return c.opts.Header
}

// WithHTTPClient overrides the client used for the upgrade request
// (custom roots, proxies). Default http.DefaultClient.
func WithHTTPClient(httpClient *http.Client) Option {
	return clientOption(func(c *Client) { c.opts.HTTPClient = httpClient })
}

// WithKeepalive paces WebSocket-level pings so proxies with idle
// timeouts keep a quiet tunnel open. Default dial.DefaultKeepalive; a
// negative value disables them (the inner HTTP/2 PING remains).
func WithKeepalive(every time.Duration) Option {
	return clientOption(func(c *Client) { c.opts.Keepalive = every })
}

// WithVersion sets the peer build version sent in Hello
// (observability).
func WithVersion(version string) Option {
	return clientOption(func(c *Client) { c.opts.Version = version })
}

// WithTunnelTLS encrypts the payload end-to-end INSIDE the tunnel:
// after the plaintext holt handshake the peer runs a TLS server over
// the stream and serves the handler over HTTPS. The hub must dial
// with a matching client config (hub.WithPeerTLS). Independent of any
// TLS on the outer WebSocket hop.
func WithTunnelTLS(cfg *tls.Config) Option {
	return clientOption(func(c *Client) { c.opts.TLSConfig = cfg })
}
