//nolint:testpackage // tunnelCertHosts is unexported; white-box unit test on purpose.
package commands

import (
	"slices"
	"testing"
)

func TestTunnelCertHosts(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		advertise string
		want      []string
	}{
		{"loopback stays loopback", "127.0.0.1:7000", []string{"127.0.0.1", "localhost"}},
		{"public host is added", "holt.example.com:7000", []string{"127.0.0.1", "localhost", "holt.example.com"}},
		{"lb ip is added", "192.168.8.193:7000", []string{"127.0.0.1", "localhost", "192.168.8.193"}},
		{"bare host without port", "holt.example.com", []string{"127.0.0.1", "localhost", "holt.example.com"}},
		{"bind wildcard is not a san", "0.0.0.0:7000", []string{"127.0.0.1", "localhost"}},
		{"empty advertise", "", []string{"127.0.0.1", "localhost"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := tunnelCertHosts(tc.advertise)
			if !slices.Equal(got, tc.want) {
				t.Fatalf("tunnelCertHosts(%q) = %v, want %v", tc.advertise, got, tc.want)
			}
		})
	}
}
