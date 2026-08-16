// Package proxy declares the endpoint that reaches peers through
// their tunnels: where it serves and how it routes. The data plane
// underneath is package revproxy.
package proxy

import (
	"github.com/openotters/holt/internal/revproxy"
	"github.com/openotters/holt/internal/utils"
)

// Proxy is the endpoint that reaches peers through their tunnels.
// Build it with NewProxy.
type Proxy struct {
	utils.Endpoint

	Opts []revproxy.Option
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
// base domain for the subdomain strategies. See revproxy.Routing.
func WithRouting(routing revproxy.Routing, domain string) Option {
	return proxyOption(func(p *Proxy) { p.Opts = append(p.Opts, revproxy.WithRouting(routing, domain)) })
}

// WithErrorHook observes requests the proxy could not serve, with a
// low-cardinality reason. See revproxy.ErrorHook.
func WithErrorHook(hook revproxy.ErrorHook) Option {
	return proxyOption(func(p *Proxy) { p.Opts = append(p.Opts, revproxy.WithErrorHook(hook)) })
}
