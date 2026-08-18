package holt

import (
	"net"

	"go.uber.org/zap"

	"github.com/openotters/holt/internal/client"
	"github.com/openotters/holt/internal/proxy"
	"github.com/openotters/holt/internal/server"
	"github.com/openotters/holt/internal/utils"
	"github.com/openotters/holt/pkg/reqlog"
)

// SharedOption configures either half: it is accepted by both
// NewServer and NewClient.
type SharedOption interface {
	Option
	ClientOption
}

type sharedOption struct {
	server Option
	client ClientOption
}

func (o sharedOption) ApplyServer(s *Server) { o.server.ApplyServer(s) }
func (o sharedOption) ApplyClient(c *Client) { o.client.ApplyClient(c) }

// WithLogger sets the logger. Default: no logging.
func WithLogger(logger *zap.Logger) SharedOption {
	return sharedOption{
		server: server.WithLogger(logger),
		client: client.WithLogger(logger),
	}
}

// Middleware wraps an endpoint's handler — auth on the tunnel,
// instrumentation on the proxy, anything http.
type Middleware = utils.Middleware

// EndpointOption configures either endpoint kind: it is accepted by
// both NewTunnel and NewProxy.
type EndpointOption interface {
	TunnelOption
	ProxyOption
}

type endpointOption func(*utils.Endpoint)

func (f endpointOption) ApplyTunnel(t *Tunnel) { f(&t.Endpoint) }
func (f endpointOption) ApplyProxy(p *Proxy)   { f(&p.Endpoint) }

// WithListener serves the endpoint on a listener the caller bound —
// a tls.Listener for wss, a systemd socket, ":0" in a test — instead
// of binding the endpoint's address. The listener is closed when Run
// returns.
func WithListener(lis net.Listener) EndpointOption {
	return endpointOption(func(e *utils.Endpoint) { e.Lis = lis })
}

// WithMiddleware wraps the endpoint's handler, first-listed
// outermost. Repeatable; later calls append.
func WithMiddleware(middleware ...Middleware) EndpointOption {
	return endpointOption(func(e *utils.Endpoint) { e.Middleware = append(e.Middleware, middleware...) })
}

// RequestEvent is one request crossing a tunnel: what it was, what
// came back, how long it took, and (on the hub) which peer it went to.
type RequestEvent = reqlog.Event

// RequestHook receives one RequestEvent per request, after the
// response. It runs on the request's goroutine, so keep it cheap.
type RequestHook = reqlog.Hook

// WatchOption configures a live request view. It fits either end: on
// NewProxy the hub reports every request it carried for every peer, on
// NewClient the peer reports the ones its own handler served.
type WatchOption interface {
	ProxyOption
	ClientOption
}

type watchOption struct {
	proxy  ProxyOption
	client ClientOption
}

func (o watchOption) ApplyProxy(p *Proxy)   { o.proxy.ApplyProxy(p) }
func (o watchOption) ApplyClient(c *Client) { o.client.ApplyClient(c) }

// WithRequestHook reports every request as it completes — the live
// view the CLI prints, available to anything embedding either half.
// Nothing is stored; what the hook does with the event is yours.
// The hub's duration includes the tunnel hop, the peer's does not.
func WithRequestHook(hook RequestHook) WatchOption {
	return watchOption{
		proxy:  proxy.WithRequestHook(hook),
		client: client.WithRequestHook(hook),
	}
}
