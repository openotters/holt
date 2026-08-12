package token_test

import (
	"testing"

	"github.com/openotters/holt/cmd/holt/internal/token"
)

func TestEncodeDecodeRoundTrip(t *testing.T) {
	t.Parallel()

	in := token.JoinToken{
		Peer:      "alice",
		TunnelURL: "https://holt.example.com",
		JWT:       "a.b.c",
	}

	out, err := token.Decode(in.Encode())
	if err != nil {
		t.Fatal(err)
	}

	if out != in {
		t.Fatalf("round trip: got %+v, want %+v", out, in)
	}
}

func TestDecodeRejectsIncomplete(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		tok  token.JoinToken
	}{
		{"no url", token.JoinToken{Peer: "a", JWT: "j"}},
		{"no jwt", token.JoinToken{Peer: "a", TunnelURL: "http://localhost:7000"}},
		{"bad scheme", token.JoinToken{Peer: "a", TunnelURL: "ftp://localhost:7000", JWT: "j"}},
		{"no host", token.JoinToken{Peer: "a", TunnelURL: "http://", JWT: "j"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			if _, err := token.Decode(tc.tok.Encode()); err == nil {
				t.Fatalf("expected Decode to reject %s", tc.name)
			}
		})
	}
}

func TestTarget(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name           string
		url            string
		wantAddr       string
		wantServerName string
		wantTLS        bool
	}{
		{"https default port", "https://holt.example.com", "holt.example.com:443", "holt.example.com", true},
		{"https explicit port", "https://holt.example.com:8443", "holt.example.com:8443", "holt.example.com", true},
		{"http local", "http://127.0.0.1:7000", "127.0.0.1:7000", "127.0.0.1", false},
		{"http default port", "http://holt.example.com", "holt.example.com:80", "holt.example.com", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			jt := token.JoinToken{Peer: "p", TunnelURL: tc.url, JWT: "j"}

			addr, serverName, useTLS, err := jt.Target()
			if err != nil {
				t.Fatal(err)
			}

			if addr != tc.wantAddr || serverName != tc.wantServerName || useTLS != tc.wantTLS {
				t.Fatalf("Target(%q) = (%q, %q, %v), want (%q, %q, %v)",
					tc.url, addr, serverName, useTLS, tc.wantAddr, tc.wantServerName, tc.wantTLS)
			}
		})
	}
}
