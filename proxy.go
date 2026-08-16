package holt

import (
	"github.com/openotters/holt/internal/proxy"
	"github.com/openotters/holt/internal/revproxy"
)

// Proxy is the endpoint that reaches peers through their tunnels.
// Build it with NewProxy.
type Proxy = proxy.Proxy

// ProxyOption configures a Proxy; every EndpointOption is one too.
type ProxyOption = proxy.Option

// NewProxy declares the peer-reaching reverse proxy on addr. By
// default it routes on the x-tunnel-peer header; WithRouting adds
// subdomain routing, WithErrorHook observability.
func NewProxy(addr string, opts ...ProxyOption) *Proxy { return proxy.NewProxy(addr, opts...) }

// Routing selects how the proxy picks the target peer from a request.
type Routing = revproxy.Routing

// The routing strategies.
const (
	RoutingHeader    = revproxy.RoutingHeader
	RoutingSubdomain = revproxy.RoutingSubdomain
	RoutingBoth      = revproxy.RoutingBoth
)

// WithRouting sets how the proxy picks the target peer; domain is the
// base domain for the subdomain strategies.
func WithRouting(routing Routing, domain string) ProxyOption {
	return proxy.WithRouting(routing, domain)
}

// ErrorHook observes requests the proxy could not serve, with a
// low-cardinality reason.
type ErrorHook = revproxy.ErrorHook

// WithErrorHook observes requests the proxy could not serve.
func WithErrorHook(hook ErrorHook) ProxyOption { return proxy.WithErrorHook(hook) }
