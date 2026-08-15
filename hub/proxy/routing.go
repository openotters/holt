package proxy

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
)

// RouteHeader is the HTTP header that names the target peer.
const RouteHeader = "x-tunnel-peer"

// Routing is how the proxy picks the target peer for a request. The
// header works anywhere (no DNS needed); subdomain routing gives every
// peer its own hostname, which is what ordinary clients (browsers,
// webhooks, anything that only takes a URL) can actually use.
type Routing string

// The routing strategies. The zero value is not one of them: pick
// explicitly, or let New default to RoutingHeader.
const (
	RoutingHeader    Routing = "header"
	RoutingSubdomain Routing = "subdomain"
	RoutingBoth      Routing = "both"
)

// ErrNoDomain is returned by Validate when a subdomain strategy has no
// base domain to match against. Without one, every host would look like
// a peer and route to nonsense.
var ErrNoDomain = errors.New("needs a base domain, e.g. peers.example.com")

// ErrUnusedDomain is returned by Validate when header routing is paired
// with a base domain. The pair is ambiguous — it reads as "serve
// subdomains too", which header routing does not do — so it is refused
// rather than quietly resolved one way.
var ErrUnusedDomain = errors.New("header routing does not use a base domain; use \"both\" to serve subdomains too")

// ErrUnknownRouting is returned by Validate for a strategy that is not
// one of header, subdomain, or both.
var ErrUnknownRouting = errors.New("unknown routing strategy")

// Validate reports whether the strategy can be served with the given
// base domain, so a misconfiguration fails at boot rather than once per
// request. Callers can wrap the result with their own configuration
// hint; errors.Is against ErrNoDomain, ErrUnusedDomain, and
// ErrUnknownRouting keeps working through the wrap.
func (r Routing) Validate(domain string) error {
	hasDomain := strings.Trim(domain, ".") != ""

	switch r {
	case RoutingHeader:
		if hasDomain {
			return fmt.Errorf("proxy routing %q: %w", r, ErrUnusedDomain)
		}

		return nil
	case RoutingSubdomain, RoutingBoth:
		if !hasDomain {
			return fmt.Errorf("proxy routing %q: %w", r, ErrNoDomain)
		}

		return nil
	default:
		return fmt.Errorf("proxy routing %q: %w", r, ErrUnknownRouting)
	}
}

// Resolver maps an inbound proxy request to the peer it targets.
// The header strategy reads x-tunnel-peer; the subdomain strategy
// takes the label(s) in front of a base domain, so
// alice.peers.example.com targets "alice". With both, the header
// wins when present — it is the explicit signal.
type Resolver struct {
	header bool
	domain string // "" disables subdomain routing
}

// NewResolver builds the resolver for a routing strategy. Each strategy
// reads exactly what it names: RoutingHeader ignores the domain,
// RoutingSubdomain ignores the header. An unknown strategy resolves
// nothing, so every request lands on the "no peer named" path rather
// than routing somewhere unintended; call Routing.Validate at boot to
// reject it loudly instead.
func NewResolver(routing Routing, domain string) Resolver {
	base := strings.ToLower(strings.Trim(domain, "."))

	switch routing {
	case RoutingHeader:
		return Resolver{header: true, domain: ""}
	case RoutingSubdomain:
		return Resolver{header: false, domain: base}
	case RoutingBoth:
		return Resolver{header: true, domain: base}
	default:
		return Resolver{header: false, domain: ""}
	}
}

// Peer returns the target peer id, or "" when the request names none
// (a bare visit to the proxy, or a host outside the base domain).
func (r Resolver) Peer(req *http.Request) string {
	if r.header {
		if peer := req.Header.Get(RouteHeader); peer != "" {
			return peer
		}
	}

	if r.domain == "" {
		return ""
	}

	// Host carries the authority as sent; strip the port and the DNS
	// root dot, and lowercase it (hostnames are case-insensitive,
	// peer ids are not, so subdomain routing only reaches lowercase
	// peer ids).
	host := req.Host
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}

	host = strings.ToLower(strings.TrimSuffix(host, "."))

	suffix := "." + r.domain
	if !strings.HasSuffix(host, suffix) {
		return ""
	}

	return strings.TrimSuffix(host, suffix)
}
