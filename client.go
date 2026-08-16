package holt

import (
	"crypto/tls"
	"net/http"
	"time"

	"github.com/openotters/holt/internal/client"
)

// Client is the assembled peer: a persistent attach loop that dials
// the hub and serves an http.Handler back through the tunnel,
// redialing with jittered backoff. It is NewServer's counterpart for
// the client side.
//
//	c := holt.NewClient("wss://hub.example.com", handler,
//		holt.WithBearerToken(token),
//	)
//
//	err := c.Run(ctx) // attaches, serves, redials; cancel ctx to stop
type Client = client.Client

// ClientOption configures a Client; every SharedOption is one too.
type ClientOption = client.Option

// NewClient wires a peer that attaches to the hub's tunnel endpoint
// at hubURL — ws:// or wss:// (http/https are accepted as aliases) —
// and serves handler over the tunnel, as if it were a normal HTTP
// server. Nothing dials until Run.
func NewClient(hubURL string, handler http.Handler, opts ...ClientOption) *Client {
	return client.New(hubURL, handler, opts...)
}

// WithBearerToken sends token as the Authorization bearer on the
// attach request — the counterpart of the server's WithAuthBearer.
func WithBearerToken(token string) ClientOption { return client.WithBearerToken(token) }

// WithHeader adds a header to the WebSocket upgrade request —
// whatever the edge in front of the hub wants (e.g.
// CF-Access-Client-Id / CF-Access-Client-Secret). Repeatable.
func WithHeader(key, value string) ClientOption { return client.WithHeader(key, value) }

// WithHTTPClient overrides the client used for the upgrade request
// (custom roots, proxies). Default http.DefaultClient.
func WithHTTPClient(httpClient *http.Client) ClientOption { return client.WithHTTPClient(httpClient) }

// WithKeepalive paces WebSocket-level pings so proxies with idle
// timeouts keep a quiet tunnel open. A negative value disables them
// (the inner HTTP/2 PING remains).
func WithKeepalive(every time.Duration) ClientOption { return client.WithKeepalive(every) }

// WithVersion sets the peer build version sent in Hello
// (observability).
func WithVersion(version string) ClientOption { return client.WithVersion(version) }

// WithTunnelTLS encrypts the payload end-to-end INSIDE the tunnel:
// after the plaintext holt handshake the peer runs a TLS server over
// the stream and serves the handler over HTTPS. The hub must dial
// with a matching client config (WithPeerTLS). Independent of any TLS
// on the outer WebSocket hop.
func WithTunnelTLS(cfg *tls.Config) ClientOption { return client.WithTunnelTLS(cfg) }
