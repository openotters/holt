package revproxy_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/openotters/holt/pkg/revproxy"
)

// request builds an inbound proxy request with the given Host and,
// when non-empty, the routing header.
func request(t *testing.T, host, header string) *http.Request {
	t.Helper()

	r, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://placeholder/", nil)
	if err != nil {
		t.Fatal(err)
	}

	r.Host = host
	if header != "" {
		r.Header.Set(revproxy.RouteHeader, header)
	}

	return r
}

func TestRoutingResolverPeer(t *testing.T) {
	t.Parallel()

	const domain = "peers.example.com"

	cases := []struct {
		name    string
		routing revproxy.Routing
		domain  string
		host    string
		header  string
		want    string
	}{
		// header strategy
		{"header routes", revproxy.RoutingHeader, "", "anything", "alice", "alice"},
		{"header absent", revproxy.RoutingHeader, "", "alice." + domain, "", ""},

		// subdomain strategy
		{"subdomain routes", revproxy.RoutingSubdomain, domain, "alice." + domain, "", "alice"},
		{"subdomain with port", revproxy.RoutingSubdomain, domain, "alice." + domain + ":7002", "", "alice"},
		{"subdomain uppercased host", revproxy.RoutingSubdomain, domain, "ALICE." + domain, "", "alice"},
		{"subdomain trailing dot", revproxy.RoutingSubdomain, domain, "alice." + domain + ".", "", "alice"},
		{"apex is not a peer", revproxy.RoutingSubdomain, domain, domain, "", ""},
		{"other domain ignored", revproxy.RoutingSubdomain, domain, "alice.evil.example.com", "", ""},
		{"suffix without dot boundary", revproxy.RoutingSubdomain, domain, "notpeers.example.com", "", ""},
		{"header ignored in subdomain mode", revproxy.RoutingSubdomain, domain, domain, "alice", ""},

		// both
		{"both prefers the header", revproxy.RoutingBoth, domain, "bob." + domain, "alice", "alice"},
		{"both falls back to subdomain", revproxy.RoutingBoth, domain, "bob." + domain, "", "bob"},
		{"both with neither", revproxy.RoutingBoth, domain, "someone.else.net", "", ""},

		// the configured domain is normalised too
		{
			"configured domain with dots trimmed",
			revproxy.RoutingSubdomain, "." + domain + ".", "alice." + domain, "", "alice",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			resolver, err := tc.routing.Resolver(tc.domain)
			if err != nil {
				t.Fatalf("Resolver() = %v", err)
			}

			if got := resolver.Peer(request(t, tc.host, tc.header)); got != tc.want {
				t.Fatalf("Peer() = %q, want %q", got, tc.want)
			}
		})
	}
}

// An unusable strategy/domain pair must be an error at construction —
// never a resolver that silently routes nothing.
func TestRoutingResolverErrors(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		routing revproxy.Routing
		domain  string
		want    error
	}{
		{"header with a domain is ambiguous", revproxy.RoutingHeader, "peers.example.com", revproxy.ErrUnusedDomain},
		{"subdomain without domain", revproxy.RoutingSubdomain, "", revproxy.ErrNoDomain},
		{"both without domain", revproxy.RoutingBoth, "", revproxy.ErrNoDomain},
		{"domain of only dots is empty", revproxy.RoutingSubdomain, "..", revproxy.ErrNoDomain},
		{"unknown strategy", revproxy.Routing("sideways"), "peers.example.com", revproxy.ErrUnknownRouting},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := tc.routing.Resolver(tc.domain); !errors.Is(err, tc.want) {
				t.Fatalf("Resolver() error = %v, want %v", err, tc.want)
			}
		})
	}
}

// ResolveFirst composes strategies in order; custom resolvers slot in
// next to the built-ins.
func TestResolveFirst(t *testing.T) {
	t.Parallel()

	sub, err := revproxy.ResolveBySubdomain("peers.example.com")
	if err != nil {
		t.Fatal(err)
	}

	resolver := revproxy.ResolveFirst(revproxy.ResolveByHeader(), sub)

	if got := resolver.Peer(request(t, "bob.peers.example.com", "alice")); got != "alice" {
		t.Fatalf("header should win, got %q", got)
	}

	if got := resolver.Peer(request(t, "bob.peers.example.com", "")); got != "bob" {
		t.Fatalf("subdomain fallback, got %q", got)
	}

	if got := revproxy.ResolveFirst().Peer(request(t, "x", "")); got != "" {
		t.Fatalf("empty chain resolves nothing, got %q", got)
	}
}
