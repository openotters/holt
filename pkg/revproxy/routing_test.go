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
		{"subdomain with port", revproxy.RoutingSubdomain, domain, "alice." + domain + ":7202", "", "alice"},
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

			resolvers, err := tc.routing.Resolvers(tc.domain)
			if err != nil {
				t.Fatalf("Resolvers() = %v", err)
			}

			got := ""

			for _, resolver := range resolvers {
				if got = resolver.Peer(request(t, tc.host, tc.header)); got != "" {
					break
				}
			}

			if got != tc.want {
				t.Fatalf("chain resolved %q, want %q", got, tc.want)
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

			if _, err := tc.routing.Resolvers(tc.domain); !errors.Is(err, tc.want) {
				t.Fatalf("Resolvers() error = %v, want %v", err, tc.want)
			}
		})
	}
}

// queryResolver is a custom Resolver — anything implementing the
// interface slots into the chain next to the built-ins.
type queryResolver struct{}

func (queryResolver) Peer(req *http.Request) string { return req.URL.Query().Get("peer") }

// The proxy tries its resolvers in order and the first peer named
// wins; a custom implementation composes with the built-ins.
func TestResolverChainFirstWins(t *testing.T) {
	t.Parallel()

	sub, err := revproxy.ResolveBySubdomain("peers.example.com")
	if err != nil {
		t.Fatal(err)
	}

	chain := []revproxy.Resolver{revproxy.ResolveByHeader(), sub, queryResolver{}}

	peer := func(host, header, target string) string {
		r := request(t, host, header)
		if target != "" {
			r.URL.RawQuery = "peer=" + target
		}

		for _, resolver := range chain {
			if got := resolver.Peer(r); got != "" {
				return got
			}
		}

		return ""
	}

	if got := peer("bob.peers.example.com", "alice", "carol"); got != "alice" {
		t.Fatalf("header should win, got %q", got)
	}

	if got := peer("bob.peers.example.com", "", "carol"); got != "bob" {
		t.Fatalf("subdomain before the custom resolver, got %q", got)
	}

	if got := peer("elsewhere.example.net", "", "carol"); got != "carol" {
		t.Fatalf("custom resolver as the last resort, got %q", got)
	}

	if got := peer("elsewhere.example.net", "", ""); got != "" {
		t.Fatalf("nothing named resolves nothing, got %q", got)
	}
}
