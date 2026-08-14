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

func TestWSURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		url  string
		want string
	}{
		{"wss passes through", "wss://holt.example.com", "wss://holt.example.com"},
		{"ws passes through", "ws://127.0.0.1:7000", "ws://127.0.0.1:7000"},
		{"https aliases wss", "https://holt.example.com:8443", "wss://holt.example.com:8443"},
		{"http aliases ws", "http://127.0.0.1:7000", "ws://127.0.0.1:7000"},
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
