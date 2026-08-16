package token_test

import (
	"encoding/base64"
	"testing"
	"time"

	"github.com/openotters/holt/pkg/jwtauth"
	"github.com/openotters/holt/pkg/token"
)

//nolint:gochecknoglobals // shared fixture, never mutated
var secret = []byte("test-secret-value-for-signing-only")

// mint is what the hub hands out: the signed JWT, nothing around it.
func mint(t *testing.T, peer, tunnelURL string) string {
	t.Helper()

	tok, err := jwtauth.Issue(secret, peer, tunnelURL, time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	return tok
}

func TestDecodeCompactJWS(t *testing.T) {
	t.Parallel()

	tok := mint(t, "alice", "wss://holt.example.com")

	got, err := token.Decode(tok)
	if err != nil {
		t.Fatal(err)
	}

	if got.Peer != "alice" {
		t.Fatalf("peer = %q, want alice", got.Peer)
	}

	if got.TunnelURL != "wss://holt.example.com" {
		t.Fatalf("tunnel url = %q", got.TunnelURL)
	}

	if got.JWT != tok {
		t.Fatalf("jwt = %q, want the token itself", got.JWT)
	}
}

// A token is one format now, so what enroll prints must be exactly
// what the peer presents as its bearer.
func TestDecodedJWTIsTheTokenItself(t *testing.T) {
	t.Parallel()

	tok := mint(t, "web", "ws://127.0.0.1:7200")

	jt, err := token.Decode(tok)
	if err != nil {
		t.Fatal(err)
	}

	if jt.JWT != tok {
		t.Fatal("the presented credential differs from the token")
	}
}

// Tokens minted before v0.20 were a base64 JSON envelope; they must
// keep working so an upgrade does not strand tokens already handed
// out.
func TestDecodeLegacyEnvelope(t *testing.T) {
	t.Parallel()

	inner := mint(t, "legacy-peer", "wss://old.example.com")
	envelope := `{"peer":"legacy-peer","tunnel_url":"https://old.example.com","jwt":"` + inner + `"}`
	legacy := base64.StdEncoding.EncodeToString([]byte(envelope))

	got, err := token.Decode(legacy)
	if err != nil {
		t.Fatal(err)
	}

	if got.Peer != "legacy-peer" {
		t.Fatalf("peer = %q", got.Peer)
	}

	// The envelope's URL wins for a legacy token: it is what that
	// format carried.
	if got.TunnelURL != "https://old.example.com" {
		t.Fatalf("tunnel url = %q", got.TunnelURL)
	}

	if got.JWT != inner {
		t.Fatal("legacy token must present its inner jwt")
	}
}

func TestDecodeRejects(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"empty":             "",
		"garbage":           "not-a-token",
		"jwt without aud":   mintNoAudience(t),
		"bad scheme":        mint(t, "a", "ftp://holt.example.com"),
		"no host":           mint(t, "a", "wss://"),
		"legacy incomplete": base64.StdEncoding.EncodeToString([]byte(`{"peer":"a"}`)),
	}

	for name, tok := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if _, err := token.Decode(tok); err == nil {
				t.Fatalf("expected Decode to reject %s", name)
			}
		})
	}
}

func mintNoAudience(t *testing.T) string {
	t.Helper()

	return mint(t, "a", "")
}

func TestWSURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		url  string
		want string
	}{
		{"wss passes through", "wss://holt.example.com", "wss://holt.example.com"},
		{"ws passes through", "ws://127.0.0.1:7200", "ws://127.0.0.1:7200"},
		{"https aliases wss", "https://holt.example.com:8443", "wss://holt.example.com:8443"},
		{"http aliases ws", "http://127.0.0.1:7200", "ws://127.0.0.1:7200"},
		{"path survives", "wss://holt.example.com/tunnel", "wss://holt.example.com/tunnel"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			jt := token.JoinToken{Peer: "p", TunnelURL: tc.url, JWT: "j"}

			got, err := jt.WSURL()
			if err != nil {
				t.Fatal(err)
			}

			if got != tc.want {
				t.Fatalf("WSURL(%q) = %q, want %q", tc.url, got, tc.want)
			}
		})
	}
}
