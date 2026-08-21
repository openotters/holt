// Package proxy is the hub's data plane: an http.Handler that picks
// the peer a request targets — by x-tunnel-peer header, or by
// subdomain of a base domain — and dials it through that peer's
// tunnel. A request that names no peer, or names one that is not
// attached, never reaches a backend: it gets a bare page that says
// nothing about the hub.
//
// Header routing needs no configuration:
//
//	mux.Handle("/", proxy.New(registry))
//
// where registry is a *hub.Registry (or anything else satisfying
// Peers). Subdomain routing and an observability hook are options:
//
//	resolvers, err := proxy.RoutingBoth.Resolvers("peers.example.com")
//	// handle err — a bad strategy/domain pair fails here, at boot
//	p := proxy.New(registry,
//		proxy.WithResolvers(resolvers...),
//		proxy.WithErrorHook(func(ctx context.Context, reason string) {
//			errors.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
//		}),
//	)
//
// The request log is a wrapper, not an option: the proxy carries
// requests and announces where each one went (see RouteHeader), and
// whoever wants the stream composes it on top:
//
//	handler := reqlog.Middleware(hook, p, reqlog.WithPeerHeader(proxy.RouteHeader))
//
// Transport encryption is the deployment's job: put a TLS edge,
// ingress, or mesh in front of the hub, same as its other listeners.
package revproxy

import (
	"context"
	"net/http"
	"net/http/httputil"

	"go.opentelemetry.io/otel/metric"
)

// Peers is the live-tunnel half of the hub the proxy dials through;
// *hub.Registry satisfies it.
type Peers interface {
	Attached(peer string) bool
	RoundTripper(peer string) http.RoundTripper
}

// ErrorHook observes requests that could not be proxied. It is called
// with the request context and a low-cardinality reason, which makes it
// a metric counter's attribute as-is. The response the caller sees is
// unaffected: the hook only watches.
type ErrorHook func(ctx context.Context, reason string)

// The reasons an ErrorHook is called with.
const (
	// ReasonNoPeer is a request that named no peer at all: no route
	// header, and no host under the base domain. The value stays
	// "no-header" so existing dashboards keep their series.
	ReasonNoPeer = "no-header"
	// ReasonNotAttached is a named peer with no live tunnel.
	ReasonNotAttached = "not-attached"
	// ReasonTransport is a live tunnel that failed mid-request.
	ReasonTransport = "transport"
)

// Proxy routes inbound requests to attached peers. Build it with New;
// the zero value is not usable.
//
// The data plane is a middleware chain around a routing core: every
// request crosses the stages, then routing picks the tunnel. The
// built-in observation (metrics, the request log) is itself stages in
// that chain, not a special case in the core.
type Proxy struct {
	peers     Peers
	resolvers []Resolver
	onError   ErrorHook
	metrics   *metrics
	reverse   *httputil.ReverseProxy
	handler   http.Handler
}

// Middleware is one stage of the data plane: it wraps the handler
// below it and sees every request on the way through.
type Middleware func(http.Handler) http.Handler

// config is what the options build up. It only feeds New's compile
// step: once the chain is built, the configuration is spent, which is
// why none of it lives on the Proxy.
type config struct {
	resolvers []Resolver
	onError   ErrorHook
	chain     []Middleware
	metrics   *metrics
}

// Option configures a Proxy.
type Option func(*config)

// WithResolvers sets how the target peer is picked: resolvers are
// tried in order, first peer named wins. Repeatable; later calls
// append, and any call replaces the header-routing default.
func WithResolvers(resolvers ...Resolver) Option {
	return func(c *config) { c.resolvers = append(c.resolvers, resolvers...) }
}

// WithErrorHook registers an observer for requests that could not be
// proxied. See ErrorHook.
func WithErrorHook(hook ErrorHook) Option {
	return func(c *config) { c.onError = hook }
}

// WithMiddleware adds stages to the data plane, in the order given.
// They run inside the built-in instruments and outside routing: a
// stage may mutate the request (add headers, rewrite), the resolvers
// see what the stages left, and a stage that does not call next
// short-circuits the proxy. Observation is a stage like any other:
// wrap the Proxy, or add it here (reqlog.Middleware, for one).
func WithMiddleware(mw ...Middleware) Option {
	return func(c *config) { c.chain = append(c.chain, mw...) }
}

// WithMeterProvider sets the OTel MeterProvider the data-plane
// instruments are built from. Default: the global provider, a no-op
// until an SDK is installed.
func WithMeterProvider(mp metric.MeterProvider) Option {
	return func(c *config) { c.metrics = newMetrics(mp) }
}

// New builds a proxy over the given peers. Without options it routes on
// the x-tunnel-peer header alone.
func New(peers Peers, opts ...Option) *Proxy {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}

	if len(cfg.resolvers) == 0 {
		cfg.resolvers = []Resolver{ResolveByHeader()}
	}

	if cfg.metrics == nil {
		cfg.metrics = newMetrics(nil)
	}

	p := &Proxy{
		peers:     peers,
		resolvers: cfg.resolvers,
		onError:   cfg.onError,
		metrics:   cfg.metrics,
		reverse:   nil, // built below, it closes over p
		handler:   nil,
	}

	p.reverse = &httputil.ReverseProxy{
		// The tunnel is the address: the peer serves whatever handler it
		// attached with, so the outbound URL only has to be well-formed.
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.Out.URL.Scheme = "http"
			pr.Out.URL.Host = peerHost
			pr.Out.Host = peerHost
		},
		Transport:     peerTransport{proxy: p},
		FlushInterval: -1, // stream responses through immediately
		ErrorHandler:  p.serveError,
	}

	// Compile the chain, innermost first, in an order the options
	// cannot change: instruments outermost, then the configured stages,
	// then routing at the core.
	var handler http.Handler = http.HandlerFunc(p.route)
	for i := len(cfg.chain) - 1; i >= 0; i-- {
		handler = cfg.chain[i](handler)
	}

	p.handler = p.metrics.middleware(handler)

	return p
}

// peerHost is the placeholder authority on proxied requests: the tunnel
// decides the destination, not the URL.
const peerHost = "peer.invalid"

// ServeHTTP routes the request to the peer it names; one that names
// none gets the landing page, never a 502.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	p.handler.ServeHTTP(w, r)
}

// route is the core of the chain: it sends the request down the peer's
// tunnel, or serves the landing page when it named none. Either way it
// normalises RouteHeader to the outcome, which is how anything
// wrapping the proxy learns where a request went (see RouteHeader).
func (p *Proxy) route(w http.ResponseWriter, r *http.Request) {
	peer := p.peer(r)
	if peer == "" {
		// A header the resolvers did not accept must not read as a
		// routed peer afterwards.
		r.Header.Del(RouteHeader)
		p.record(r.Context(), ReasonNoPeer)
		writePage(w, r, http.StatusBadRequest)

		return
	}

	// Normalise a subdomain hit onto the header so everything
	// downstream routes the one way.
	r.Header.Set(RouteHeader, peer)
	p.reverse.ServeHTTP(w, r)
}

// peer runs the resolver chain; first resolver that names a peer wins.
func (p *Proxy) peer(r *http.Request) string {
	for _, resolver := range p.resolvers {
		if peer := resolver.Peer(r); peer != "" {
			return peer
		}
	}

	return ""
}

func (p *Proxy) record(ctx context.Context, reason string) {
	p.metrics.recordError(ctx, reason)

	if p.onError != nil {
		p.onError(ctx, reason)
	}
}

// peerTransport dispatches each request down the tunnel the route
// header names.
type peerTransport struct {
	proxy *Proxy
}

func (t peerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	peer := req.Header.Get(RouteHeader)
	if peer == "" {
		// Defense in depth: ServeHTTP already served the landing page.
		t.proxy.record(req.Context(), ReasonNoPeer)

		return nil, notAttachedError{peer: ""}
	}

	req.Header.Del(RouteHeader)

	if !t.proxy.peers.Attached(peer) {
		t.proxy.record(req.Context(), ReasonNotAttached)

		return nil, notAttachedError{peer: peer}
	}

	resp, err := t.proxy.peers.RoundTripper(peer).RoundTrip(req)
	if err != nil {
		t.proxy.record(req.Context(), ReasonTransport)
	}

	return resp, err
}
