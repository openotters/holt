//nolint:testpackage // peerResolver is unexported; white-box unit test on purpose.
package commands

import (
	"net/http"
	"testing"
)

func TestPeerResolver(t *testing.T) {
	t.Parallel()

	const domain = "peers.example.com"

	cases := []struct {
		name    string
		routing string
		domain  string
		host    string
		header  string
		want    string
	}{
		// header strategy
		{"header routes", routingHeader, "", "anything", "alice", "alice"},
		{"header absent", routingHeader, "", "alice." + domain, "", ""},

		// subdomain strategy
		{"subdomain routes", routingSubdomain, domain, "alice." + domain, "", "alice"},
		{"subdomain with port", routingSubdomain, domain, "alice." + domain + ":7002", "", "alice"},
		{"subdomain uppercased host", routingSubdomain, domain, "ALICE." + domain, "", "alice"},
		{"subdomain trailing dot", routingSubdomain, domain, "alice." + domain + ".", "", "alice"},
		{"apex is not a peer", routingSubdomain, domain, domain, "", ""},
		{"other domain ignored", routingSubdomain, domain, "alice.evil.example.com", "", ""},
		{"suffix without dot boundary", routingSubdomain, domain, "notpeers.example.com", "", ""},
		{"header ignored in subdomain mode", routingSubdomain, domain, domain, "alice", ""},

		// both
		{"both prefers the header", routingBoth, domain, "bob." + domain, "alice", "alice"},
		{"both falls back to subdomain", routingBoth, domain, "bob." + domain, "", "bob"},
		{"both with neither", routingBoth, domain, "someone.else.net", "", ""},

		// the configured domain is normalised too
		{"configured domain with dots trimmed", routingSubdomain, "." + domain + ".", "alice." + domain, "", "alice"},
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
				r.Header.Set(routeHeader, tc.header)
			}

			if got := newPeerResolver(tc.routing, tc.domain).peer(r); got != tc.want {
				t.Fatalf("peer() = %q, want %q", got, tc.want)
			}
		})
	}
}
