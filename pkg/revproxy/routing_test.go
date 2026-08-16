package revproxy_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/openotters/holt/pkg/revproxy"
)

func TestResolverPeer(t *testing.T) {
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
		{"header mode ignores subdomains", revproxy.RoutingHeader, domain, "alice." + domain, "", ""},

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

		// an unvalidated strategy resolves nothing rather than guessing
		{"unknown strategy routes nowhere", revproxy.Routing("nonsense"), domain, "alice." + domain, "alice", ""},
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
				r.Header.Set(revproxy.RouteHeader, tc.header)
			}

			if got := revproxy.NewResolver(tc.routing, tc.domain).Peer(r); got != tc.want {
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
		routing revproxy.Routing
		domain  string
		want    error
	}{
		{"header needs no domain", revproxy.RoutingHeader, "", nil},
		{"header with a domain is ambiguous", revproxy.RoutingHeader, "peers.example.com", revproxy.ErrUnusedDomain},
		{"subdomain with domain", revproxy.RoutingSubdomain, "peers.example.com", nil},
		{"both with domain", revproxy.RoutingBoth, "peers.example.com", nil},
		{"subdomain without domain", revproxy.RoutingSubdomain, "", revproxy.ErrNoDomain},
		{"both without domain", revproxy.RoutingBoth, "", revproxy.ErrNoDomain},
		{"domain of only dots is empty", revproxy.RoutingSubdomain, "..", revproxy.ErrNoDomain},
		{"unknown strategy", revproxy.Routing("sideways"), "peers.example.com", revproxy.ErrUnknownRouting},
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
