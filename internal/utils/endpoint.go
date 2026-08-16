// Package utils holds the plumbing the Tunnel and Proxy endpoint
// declarations share.
package utils

import (
	"net"
	"net/http"
)

// Middleware wraps an endpoint's handler — auth on the tunnel,
// instrumentation on the proxy, anything http.
type Middleware func(http.Handler) http.Handler

// Endpoint is what Tunnel and Proxy share: where to serve (an address
// to bind, or a listener the caller already bound — TLS, a systemd
// socket, ":0" in a test) and the middleware around the handler.
type Endpoint struct {
	Addr       string
	Lis        net.Listener
	Middleware []Middleware
}

func (e Endpoint) Configured() bool { return e.Addr != "" || e.Lis != nil }

// Wrap applies the middleware, first-listed outermost.
func (e Endpoint) Wrap(handler http.Handler) http.Handler {
	for i := len(e.Middleware) - 1; i >= 0; i-- {
		handler = e.Middleware[i](handler)
	}

	return handler
}
