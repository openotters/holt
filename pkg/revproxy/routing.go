package revproxy

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// RouteHeader is the HTTP header that names the target peer.
const RouteHeader = "x-tunnel-peer"

// Resolver maps an inbound proxy request to the peer it targets, or
// "" when the request names none (a bare visit to the proxy, or a
// host outside the base domain).
type Resolver interface {
	Peer(req *http.Request) string
}

// ResolveByHeader routes on the RouteHeader header. It works anywhere
// — no DNS needed — and is the proxy's default.
func ResolveByHeader() Resolver { return headerResolver{} }

// headerResolver implements ResolveByHeader.
type headerResolver struct{}

func (headerResolver) Peer(req *http.Request) string { return req.Header.Get(RouteHeader) }

// ResolveBySubdomain routes on the label(s) in front of domain, so
// alice.peers.example.com targets "alice" — every peer gets its own
// hostname, which is what ordinary clients (browsers, webhooks,
// anything that only takes a URL) can actually use. Without a domain
// to strip every host would look like a peer, so a blank one is
// ErrNoDomain instead of a resolver.
func ResolveBySubdomain(domain string) (Resolver, error) {
	base := strings.ToLower(strings.Trim(domain, "."))
	if base == "" {
		return nil, ErrNoDomain
	}

	return subdomainResolver{suffix: "." + base}, nil
}

// subdomainResolver implements ResolveBySubdomain.
type subdomainResolver struct {
	suffix string // ".peers.example.com", lowercase
}

func (s subdomainResolver) Peer(req *http.Request) string {
	// Host carries the authority as sent; strip the port and the DNS
	// root dot, and lowercase it (hostnames are case-insensitive,
	// peer ids are not, so subdomain routing only reaches lowercase
	// peer ids).
	host := req.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	host = strings.ToLower(strings.TrimSuffix(host, "."))
	if !strings.HasSuffix(host, s.suffix) {
		return ""
	}

	return strings.TrimSuffix(host, s.suffix)
}

// ResolveFirst tries each resolver in order and returns the first
// peer named. It is how strategies compose: put the explicit signal
// (the header) before the inferred one (the hostname).
func ResolveFirst(resolvers ...Resolver) Resolver { return firstResolver(resolvers) }

// firstResolver implements ResolveFirst.
type firstResolver []Resolver

func (f firstResolver) Peer(req *http.Request) string {
	for _, r := range f {
		if peer := r.Peer(req); peer != "" {
			return peer
		}
	}

	return ""
}

// Routing is the configuration vocabulary for the built-in
// strategies, as spelled in CLI flags and deployment values. It is
// turned into behavior in exactly one place — Resolver — so an
// invalid strategy or a mismatched domain is an error there, never a
// proxy that silently drops every request.
type Routing string

// The built-in strategies.
const (
	RoutingHeader    Routing = "header"
	RoutingSubdomain Routing = "subdomain"
	RoutingBoth      Routing = "both"
)

// ErrNoDomain is returned for a subdomain strategy with no base
// domain to match against.
var ErrNoDomain = errors.New("needs a base domain, e.g. peers.example.com")

// ErrUnusedDomain is returned when header routing is paired with a
// base domain. The pair is ambiguous — it reads as "serve subdomains
// too", which header routing does not do — so it is refused rather
// than quietly resolved one way.
var ErrUnusedDomain = errors.New("header routing does not use a base domain; use \"both\" to serve subdomains too")

// ErrUnknownRouting is returned for a strategy that is not one of
// header, subdomain, or both.
var ErrUnknownRouting = errors.New("unknown routing strategy")

// Resolver builds the resolver the configured strategy names, with
// domain as the base domain for the subdomain strategies. Callers can
// wrap the error with their own configuration hint; errors.Is against
// ErrNoDomain, ErrUnusedDomain, and ErrUnknownRouting keeps working
// through the wrap.
func (r Routing) Resolver(domain string) (Resolver, error) {
	switch r {
	case RoutingHeader:
		if strings.Trim(domain, ".") != "" {
			return nil, fmt.Errorf("proxy routing %q: %w", r, ErrUnusedDomain)
		}

		return ResolveByHeader(), nil
	case RoutingSubdomain:
		sub, err := ResolveBySubdomain(domain)
		if err != nil {
			return nil, fmt.Errorf("proxy routing %q: %w", r, err)
		}

		return sub, nil
	case RoutingBoth:
		sub, err := ResolveBySubdomain(domain)
		if err != nil {
			return nil, fmt.Errorf("proxy routing %q: %w", r, err)
		}

		return ResolveFirst(ResolveByHeader(), sub), nil
	default:
		return nil, fmt.Errorf("proxy routing %q: %w", r, ErrUnknownRouting)
	}
}
