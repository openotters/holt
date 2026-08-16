//nolint:testpackage // advertisedURL is unexported; white-box unit test on purpose.
package commands

import (
	"testing"

	"github.com/openotters/holt/cmd/holt/internal/config"
)

// The profile's tunnel_url describes the profile's own hub. Pointing
// the command at a different hub has to drop it, or a token minted at
// hub B tells the peer to dial hub A — which is exactly how `holt
// expose --admin-url localhost` ends up knocking on production.
func TestEnrollAdvertisedURL(t *testing.T) {
	t.Parallel()

	const (
		profileAdmin  = "https://holt.example.com"
		profileTunnel = "wss://tunnel.example.com"
		otherHub      = "http://127.0.0.1:17201"
	)

	profiled := config.Profile{AdminURL: profileAdmin, TunnelURL: profileTunnel}

	cases := []struct {
		name    string
		enroll  Enroll
		profile config.Profile
		want    string
	}{
		{
			name:    "profile hub keeps its tunnel url",
			enroll:  Enroll{},
			profile: profiled,
			want:    profileTunnel,
		},
		{
			name:    "same hub named explicitly keeps it",
			enroll:  Enroll{adminConn: adminConn{AdminURL: profileAdmin}},
			profile: profiled,
			want:    profileTunnel,
		},
		{
			name:    "trailing slash is the same hub",
			enroll:  Enroll{adminConn: adminConn{AdminURL: profileAdmin + "/"}},
			profile: profiled,
			want:    profileTunnel,
		},
		{
			name:    "another hub by url drops it",
			enroll:  Enroll{adminConn: adminConn{AdminURL: otherHub}},
			profile: profiled,
			want:    "",
		},
		{
			name:    "another hub by address drops it",
			enroll:  Enroll{adminConn: adminConn{AdminAddr: "127.0.0.1:17201"}},
			profile: profiled,
			want:    "",
		},
		{
			name:    "explicit flag always wins",
			enroll:  Enroll{TunnelURL: "wss://mine.example.com", adminConn: adminConn{AdminURL: otherHub}},
			profile: profiled,
			want:    "wss://mine.example.com",
		},
		{
			// A profile that names no hub describes no particular
			// endpoint: minting locally on the hub machine while
			// advertising the public URL stays supported.
			name:    "profile without an admin url still applies",
			enroll:  Enroll{adminConn: adminConn{AdminAddr: "127.0.0.1:7201"}},
			profile: config.Profile{TunnelURL: profileTunnel},
			want:    profileTunnel,
		},
		{
			name:    "no profile, no flag",
			enroll:  Enroll{},
			profile: config.Profile{},
			want:    "",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if got := tc.enroll.advertisedURL(tc.profile); got != tc.want {
				t.Fatalf("advertisedURL() = %q, want %q", got, tc.want)
			}
		})
	}
}
