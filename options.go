package holt

import (
	"net"

	"go.uber.org/zap"

	"github.com/openotters/holt/internal/client"
	"github.com/openotters/holt/internal/server"
	"github.com/openotters/holt/internal/utils"
)

// SharedOption configures either half: it is accepted by both
// NewServer and NewClient.
type SharedOption interface {
	Option
	ClientOption
}

// sharedOption implements SharedOption over both configs.
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

// endpointOption implements EndpointOption over the shared config.
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
