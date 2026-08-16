package holt

import (
	"github.com/openotters/holt/internal/proxy"
	"github.com/openotters/holt/pkg/revproxy"
)

// Proxy is the endpoint that reaches peers through their tunnels.
// Build it with NewProxy.
type Proxy = proxy.Proxy

// ProxyOption configures a Proxy; every EndpointOption is one too.
type ProxyOption = proxy.Option

// NewProxy declares the peer-reaching reverse proxy on addr. By
// default it routes on the x-tunnel-peer header; WithRouting adds
// subdomain routing, WithResolvers custom resolution, WithErrorHook
// observability.
func NewProxy(addr string, opts ...ProxyOption) *Proxy { return proxy.NewProxy(addr, opts...) }

// Resolver maps an inbound proxy request to the peer it targets, or
// "" when the request names none. The proxy tries its resolvers in
// order and the first peer named wins — implement this to route on
// anything the built-in strategies do not cover.
type Resolver = revproxy.Resolver

// WithResolvers appends custom resolvers to the proxy's chain, tried
// after the ones WithRouting names (or instead of the header default
// when no strategy is configured). Repeatable.
func WithResolvers(resolvers ...Resolver) ProxyOption { return proxy.WithResolvers(resolvers...) }

// Routing selects how the proxy picks the target peer from a request.
type Routing = revproxy.Routing

// The routing strategies.
const (
	RoutingHeader    = revproxy.RoutingHeader
	RoutingSubdomain = revproxy.RoutingSubdomain
	RoutingBoth      = revproxy.RoutingBoth
)

// WithRouting sets how the proxy picks the target peer; domain is the
// base domain for the subdomain strategies. An unusable pair (unknown
// strategy, missing or unused domain) fails Server.Run before
// anything binds.
func WithRouting(routing Routing, domain string) ProxyOption {
	return proxy.WithRouting(routing, domain)
}

// ErrorHook observes requests the proxy could not serve, with a
// low-cardinality reason.
type ErrorHook = revproxy.ErrorHook

// WithErrorHook observes requests the proxy could not serve.
func WithErrorHook(hook ErrorHook) ProxyOption { return proxy.WithErrorHook(hook) }
