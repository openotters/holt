package holt

import (
	"net"
	"net/http"
)

// Middleware wraps an endpoint's handler — auth on the tunnel,
// instrumentation on the proxy, anything http.
type Middleware func(http.Handler) http.Handler

// endpoint is what Tunnel and Proxy share: where to serve (an address
// to bind, or a listener the caller already bound — TLS, a systemd
// socket, ":0" in a test) and the middleware around the handler.
type endpoint struct {
	addr       string
	lis        net.Listener
	middleware []Middleware
}

func (e endpoint) configured() bool { return e.addr != "" || e.lis != nil }

// wrap applies the middleware, first-listed outermost.
func (e endpoint) wrap(handler http.Handler) http.Handler {
	for i := len(e.middleware) - 1; i >= 0; i-- {
		handler = e.middleware[i](handler)
	}

	return handler
}

// EndpointOption configures either endpoint kind: it is accepted by
// both NewTunnel and NewProxy.
type EndpointOption interface {
	TunnelOption
	ProxyOption
}

// endpointOption implements EndpointOption over the shared config.
type endpointOption func(*endpoint)

func (f endpointOption) applyTunnel(t *Tunnel) { f(&t.endpoint) }
func (f endpointOption) applyProxy(p *Proxy)   { f(&p.endpoint) }

// WithListener serves the endpoint on a listener the caller bound —
// a tls.Listener for wss, a systemd socket, ":0" in a test — instead
// of binding the endpoint's address. The listener is closed when Run
// returns.
func WithListener(lis net.Listener) EndpointOption {
	return endpointOption(func(e *endpoint) { e.lis = lis })
}

// WithMiddleware wraps the endpoint's handler, first-listed
// outermost. Repeatable; later calls append.
func WithMiddleware(middleware ...Middleware) EndpointOption {
	return endpointOption(func(e *endpoint) { e.middleware = append(e.middleware, middleware...) })
}
