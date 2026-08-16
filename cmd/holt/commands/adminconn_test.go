//nolint:testpackage // endpoint is unexported; white-box unit test on purpose.
package commands

import (
	"os"
	"path/filepath"
	"testing"
)

// writeConfig drops a config file naming one hub, with a credential
// for it, and returns its path.
func writeConfig(t *testing.T, adminURL string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "config.yaml")

	body := "default_profile: prod\nprofiles:\n  prod:\n    headers:\n      CF-Access-Client-Secret: shhh\n"
	if adminURL != "" {
		body = "default_profile: prod\nprofiles:\n  prod:\n    admin_url: " + adminURL +
			"\n    headers:\n      CF-Access-Client-Secret: shhh\n"
	}

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

// The profile's headers are a credential for the profile's hub. Aiming
// the command at another hub must not hand them over: that is a secret
// leaking to whatever host was named on the command line.
func TestEndpointHeadersFollowTheirHub(t *testing.T) {
	t.Parallel()

	const (
		profileHub = "https://holt.example.com"
		otherHub   = "http://127.0.0.1:17201"
	)

	cases := []struct {
		name           string
		profileAdmin   string
		conn           adminConn
		wantURL        string
		wantCredential bool
	}{
		{
			name:           "profile hub gets its credential",
			profileAdmin:   profileHub,
			conn:           adminConn{},
			wantURL:        profileHub,
			wantCredential: true,
		},
		{
			name:           "same hub named explicitly still gets it",
			profileAdmin:   profileHub,
			conn:           adminConn{AdminURL: profileHub + "/"},
			wantURL:        profileHub + "/",
			wantCredential: true,
		},
		{
			name:           "another hub by url does not",
			profileAdmin:   profileHub,
			conn:           adminConn{AdminURL: otherHub},
			wantURL:        otherHub,
			wantCredential: false,
		},
		{
			name:           "another hub by address does not",
			profileAdmin:   profileHub,
			conn:           adminConn{AdminAddr: "127.0.0.1:17201"},
			wantURL:        otherHub,
			wantCredential: false,
		},
		{
			// A profile naming no hub describes no particular endpoint,
			// so its headers are not tied to one either.
			name:           "profile without an admin url keeps applying",
			profileAdmin:   "",
			conn:           adminConn{AdminURL: otherHub},
			wantURL:        otherHub,
			wantCredential: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			conn := tc.conn
			conn.Config = writeConfig(t, tc.profileAdmin)

			ep, err := conn.endpoint()
			if err != nil {
				t.Fatalf("endpoint: %v", err)
			}

			if ep.url != tc.wantURL {
				t.Fatalf("url = %q, want %q", ep.url, tc.wantURL)
			}

			_, sent := ep.headers["CF-Access-Client-Secret"]
			if sent != tc.wantCredential {
				t.Fatalf("credential sent = %v, want %v (headers %v)", sent, tc.wantCredential, ep.headers)
			}
		})
	}
}

// --header is explicit, so it applies wherever the command is aimed,
// including at a hub the profile does not describe.
func TestEndpointExplicitHeaderAlwaysApplies(t *testing.T) {
	t.Parallel()

	conn := adminConn{
		AdminURL: "http://127.0.0.1:17201",
		Header:   []string{"Authorization: Bearer mine"},
		Config:   writeConfig(t, "https://holt.example.com"),
	}

	ep, err := conn.endpoint()
	if err != nil {
		t.Fatalf("endpoint: %v", err)
	}

	if got := ep.headers["Authorization"]; got != "Bearer mine" {
		t.Fatalf("Authorization = %q, want the explicit flag's value", got)
	}

	if _, sent := ep.headers["CF-Access-Client-Secret"]; sent {
		t.Fatal("the profile's credential went to a hub the profile does not describe")
	}
}
