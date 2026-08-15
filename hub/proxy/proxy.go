// Package proxy is the hub's data plane: an http.Handler that picks
// the peer a request targets and dials it through that peer's tunnel.
//
// A request names its peer either with the x-tunnel-peer header (works
// anywhere, no DNS needed) or with a subdomain of a base domain
// (alice.peers.example.com targets "alice"). A request that names no
// peer, or names one that is not attached, never reaches a backend: it
// gets a bare page that says nothing about the hub.
//
// Header routing needs no configuration:
//
//	mux.Handle("/", proxy.New(registry))
//
// where registry is a *hub.Registry (or anything else satisfying
// Peers). Subdomain routing and an observability hook are options:
//
//	p := proxy.New(registry,
//		proxy.WithRouting(proxy.RoutingBoth, "peers.example.com"),
//		proxy.WithErrorHook(func(ctx context.Context, reason string) {
//			errors.Add(ctx, 1, metric.WithAttributes(attribute.String("reason", reason)))
//		}),
//	)
//
// Transport encryption is the deployment's job: put a TLS edge,
// ingress, or mesh in front of the hub, same as its other listeners.
package proxy

import (
	"context"
	"net/http"
	"net/http/httputil"
)

// Peers is the live-tunnel half of the hub the proxy dials through.
// *hub.Registry satisfies it; a fake satisfies it in tests.
type Peers interface {
	// Attached reports whether the peer has a live tunnel right now.
	Attached(peer string) bool
	// RoundTripper dials the peer through its attached tunnel.
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
type Proxy struct {
	peers    Peers
	resolver Resolver
	onError  ErrorHook
	reverse  *httputil.ReverseProxy
}

// Option configures a Proxy.
type Option func(*Proxy)

// WithRouting sets how the target peer is picked. domain is the base
// domain for the subdomain strategies (ignored by RoutingHeader).
// Validate the pair at boot with Routing.Validate: an unusable
// combination resolves no peer here rather than erroring.
func WithRouting(routing Routing, domain string) Option {
	return func(p *Proxy) { p.resolver = NewResolver(routing, domain) }
}

// WithErrorHook registers an observer for requests that could not be
// proxied. See ErrorHook.
func WithErrorHook(hook ErrorHook) Option {
	return func(p *Proxy) { p.onError = hook }
}

// New builds a proxy over the given peers. Without options it routes on
// the x-tunnel-peer header alone.
func New(peers Peers, opts ...Option) *Proxy {
	p := &Proxy{
		peers:    peers,
		resolver: NewResolver(RoutingHeader, ""),
		onError:  nil,
		reverse:  nil,
	}

	for _, opt := range opts {
		opt(p)
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

	return p
}

// peerHost is the placeholder authority on proxied requests: the tunnel
// decides the destination, not the URL.
const peerHost = "peer.invalid"

// ServeHTTP routes the request to the peer it names. A request that
// names none gets the landing page rather than a proxied request, so
// hitting the proxy root never turns into a 502.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	peer := p.resolver.Peer(r)
	if peer == "" {
		p.record(r.Context(), ReasonNoPeer)
		writePage(w, r, http.StatusBadRequest)

		return
	}

	// A subdomain hit is normalised onto the header here, so everything
	// downstream routes the one way.
	r.Header.Set(RouteHeader, peer)
	p.reverse.ServeHTTP(w, r)
}

// record notifies the error hook, if one is registered.
func (p *Proxy) record(ctx context.Context, reason string) {
	if p.onError != nil {
		p.onError(ctx, reason)
	}
}

// peerTransport dispatches each request down the tunnel named in the
// route header.
type peerTransport struct {
	proxy *Proxy
}

func (t peerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	peer := req.Header.Get(RouteHeader)
	if peer == "" {
		// ServeHTTP serves the landing page before we get here; this is
		// just defense in depth.
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
