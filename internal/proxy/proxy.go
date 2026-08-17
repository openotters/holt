// Package proxy declares the endpoint that reaches peers through
// their tunnels: where it serves and how it routes. The data plane
// underneath is package revproxy.
package proxy

import (
	"github.com/openotters/holt/internal/utils"
	"github.com/openotters/holt/pkg/reqlog"
	"github.com/openotters/holt/pkg/revproxy"
)

// Proxy is the endpoint that reaches peers through their tunnels.
// Build it with NewProxy.
type Proxy struct {
	utils.Endpoint

	Opts []revproxy.Option

	// Routing and Domain hold the configured strategy; the server
	// resolves them at Run, so a bad pair fails before binding.
	Routing revproxy.Routing
	Domain  string

	// Resolvers are custom resolvers appended after the strategy's.
	Resolvers []revproxy.Resolver
}

// Option configures a Proxy; every EndpointOption is one too.
type Option interface{ ApplyProxy(*Proxy) }

// proxyOption implements Option.
type proxyOption func(*Proxy)

func (f proxyOption) ApplyProxy(p *Proxy) { f(p) }

// NewProxy declares the peer-reaching reverse proxy on addr. By
// default it routes on the x-tunnel-peer header; WithRouting adds
// subdomain routing, WithErrorHook observability.
func NewProxy(addr string, opts ...Option) *Proxy {
	p := &Proxy{Endpoint: utils.Endpoint{Addr: addr}}

	for _, opt := range opts {
		opt.ApplyProxy(p)
	}

	return p
}

// WithRouting sets how the proxy picks the target peer; domain is the
// base domain for the subdomain strategies. The pair is resolved when
// the server runs — an unusable combination is an error there, not a
// proxy that routes nothing. See revproxy.Routing.
func WithRouting(routing revproxy.Routing, domain string) Option {
	return proxyOption(func(p *Proxy) { p.Routing, p.Domain = routing, domain })
}

// WithResolvers appends custom resolvers to the chain, tried after
// the ones WithRouting names (or instead of the default when no
// strategy is configured). Repeatable.
func WithResolvers(resolvers ...revproxy.Resolver) Option {
	return proxyOption(func(p *Proxy) { p.Resolvers = append(p.Resolvers, resolvers...) })
}

// WithErrorHook observes requests the proxy could not serve, with a
// low-cardinality reason. See revproxy.ErrorHook.
func WithErrorHook(hook revproxy.ErrorHook) Option {
	return proxyOption(func(p *Proxy) { p.Opts = append(p.Opts, revproxy.WithErrorHook(hook)) })
}

// WithRequestHook reports every request the proxy carried, once the
// response is done. See reqlog.Hook.
func WithRequestHook(hook reqlog.Hook) Option {
	return proxyOption(func(p *Proxy) { p.Opts = append(p.Opts, revproxy.WithRequestHook(hook)) })
}
