package proxy_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/openotters/holt/hub/proxy"
)

func TestResolverPeer(t *testing.T) {
	t.Parallel()

	const domain = "peers.example.com"

	cases := []struct {
		name    string
		routing proxy.Routing
		domain  string
		host    string
		header  string
		want    string
	}{
		// header strategy
		{"header routes", proxy.RoutingHeader, "", "anything", "alice", "alice"},
		{"header absent", proxy.RoutingHeader, "", "alice." + domain, "", ""},
		{"header mode ignores subdomains", proxy.RoutingHeader, domain, "alice." + domain, "", ""},

		// subdomain strategy
		{"subdomain routes", proxy.RoutingSubdomain, domain, "alice." + domain, "", "alice"},
		{"subdomain with port", proxy.RoutingSubdomain, domain, "alice." + domain + ":7002", "", "alice"},
		{"subdomain uppercased host", proxy.RoutingSubdomain, domain, "ALICE." + domain, "", "alice"},
		{"subdomain trailing dot", proxy.RoutingSubdomain, domain, "alice." + domain + ".", "", "alice"},
		{"apex is not a peer", proxy.RoutingSubdomain, domain, domain, "", ""},
		{"other domain ignored", proxy.RoutingSubdomain, domain, "alice.evil.example.com", "", ""},
		{"suffix without dot boundary", proxy.RoutingSubdomain, domain, "notpeers.example.com", "", ""},
		{"header ignored in subdomain mode", proxy.RoutingSubdomain, domain, domain, "alice", ""},

		// both
		{"both prefers the header", proxy.RoutingBoth, domain, "bob." + domain, "alice", "alice"},
		{"both falls back to subdomain", proxy.RoutingBoth, domain, "bob." + domain, "", "bob"},
		{"both with neither", proxy.RoutingBoth, domain, "someone.else.net", "", ""},

		// the configured domain is normalised too
		{"configured domain with dots trimmed", proxy.RoutingSubdomain, "." + domain + ".", "alice." + domain, "", "alice"},

		// an unvalidated strategy resolves nothing rather than guessing
		{"unknown strategy routes nowhere", proxy.Routing("nonsense"), domain, "alice." + domain, "alice", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			r, err := http.NewRequestWithContext(t.Context(), http.MethodGet, "http://placeholder/", nil)
			if err != nil {
				t.Fatal(err)
			}

			r.Host = tc.host
			if tc.header != "" {
				r.Header.Set(proxy.RouteHeader, tc.header)
			}

			if got := proxy.NewResolver(tc.routing, tc.domain).Peer(r); got != tc.want {
				t.Fatalf("Peer() = %q, want %q", got, tc.want)
			}
		})
	}
}

// Subdomain routing without a domain to strip would match every host
// and route to nonsense, so it has to fail at boot, not per request.
func TestRoutingValidate(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		routing proxy.Routing
		domain  string
		want    error
	}{
		{"header needs no domain", proxy.RoutingHeader, "", nil},
		{"header with a domain is ambiguous", proxy.RoutingHeader, "peers.example.com", proxy.ErrUnusedDomain},
		{"subdomain with domain", proxy.RoutingSubdomain, "peers.example.com", nil},
		{"both with domain", proxy.RoutingBoth, "peers.example.com", nil},
		{"subdomain without domain", proxy.RoutingSubdomain, "", proxy.ErrNoDomain},
		{"both without domain", proxy.RoutingBoth, "", proxy.ErrNoDomain},
		{"domain of only dots is empty", proxy.RoutingSubdomain, "..", proxy.ErrNoDomain},
		{"unknown strategy", proxy.Routing("sideways"), "peers.example.com", proxy.ErrUnknownRouting},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.routing.Validate(tc.domain)
			if !errors.Is(err, tc.want) {
				t.Fatalf("Validate() = %v, want %v", err, tc.want)
			}
		})
	}
}
