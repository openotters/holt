package holt

import (
	"github.com/openotters/holt/hub/proxy"
)

// Proxy is the endpoint that reaches peers through their tunnels.
// Build it with NewProxy.
type Proxy struct {
	endpoint

	opts []proxy.Option
}

// ProxyOption configures a Proxy; every EndpointOption is one too.
type ProxyOption interface{ applyProxy(*Proxy) }

// proxyOption implements ProxyOption.
type proxyOption func(*Proxy)

func (f proxyOption) applyProxy(p *Proxy) { f(p) }

// NewProxy declares the peer-reaching reverse proxy on addr. By
// default it routes on the x-tunnel-peer header; WithRouting adds
// subdomain routing, WithErrorHook observability.
func NewProxy(addr string, opts ...ProxyOption) *Proxy {
	p := &Proxy{endpoint: endpoint{addr: addr}}

	for _, opt := range opts {
		opt.applyProxy(p)
	}

	return p
}

// WithRouting sets how the proxy picks the target peer; domain is the
// base domain for the subdomain strategies. See proxy.Routing.
func WithRouting(routing proxy.Routing, domain string) ProxyOption {
	return proxyOption(func(p *Proxy) { p.opts = append(p.opts, proxy.WithRouting(routing, domain)) })
}

// WithErrorHook observes requests the proxy could not serve, with a
// low-cardinality reason. See proxy.ErrorHook.
func WithErrorHook(hook proxy.ErrorHook) ProxyOption {
	return proxyOption(func(p *Proxy) { p.opts = append(p.opts, proxy.WithErrorHook(hook)) })
}
