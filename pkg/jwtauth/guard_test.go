package jwtauth_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openotters/holt/pkg/jwtauth"
)

const testSecret = "test-secret-value-for-signing-only"

// blockedPeers is a denylist holding whoever it was built with.
type blockedPeers map[string]bool

func (b blockedPeers) IsBlocked(peer string) bool { return b[peer] }

// attach runs one attach through the guard and reports the status and
// the reason the guard recorded (empty when it let the request through).
func attach(t *testing.T, guard jwtauth.Guard, token string) (int, string, string) {
	t.Helper()

	var reason string

	guard.OnReject = func(_ context.Context, r string) { reason = r }

	var seen string

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		peer, err := jwtauth.PeerFrom(r.Context())
		if err != nil {
			t.Errorf("PeerFrom: %v", err)
		}

		seen = peer
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	rec := httptest.NewRecorder()
	guard.Middleware(next).ServeHTTP(rec, req)

	return rec.Code, reason, seen
}

// A token minted before peer names were constrained (or by another
// issuer) can carry a name no hostname strategy could route. The
// attach path must refuse it even though the signature is valid,
// otherwise the peer attaches under a name the proxy cannot address.
func TestGuardRejectsUnroutablePeerName(t *testing.T) {
	t.Parallel()

	secret := []byte(testSecret)
	guard := jwtauth.Guard{Secret: jwtauth.NewSecret(secret)}

	cases := []struct {
		name       string
		peer       string
		wantStatus int
		wantReason string
	}{
		{"routable name attaches", "api-gateway-1", http.StatusOK, ""},
		{"uppercase refused", "Alice", http.StatusForbidden, jwtauth.ReasonInvalidName},
		{"underscore refused", "my_service", http.StatusForbidden, jwtauth.ReasonInvalidName},
		{"dotted name refused", "a.b", http.StatusForbidden, jwtauth.ReasonInvalidName},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			token, err := jwtauth.Issue(secret, tc.peer, "ws://127.0.0.1:7000", time.Hour)
			if err != nil {
				t.Fatal(err)
			}

			status, reason, peer := attach(t, guard, token)
			if status != tc.wantStatus {
				t.Fatalf("status = %d, want %d", status, tc.wantStatus)
			}

			if reason != tc.wantReason {
				t.Fatalf("reason = %q, want %q", reason, tc.wantReason)
			}

			if tc.wantStatus == http.StatusOK && peer != tc.peer {
				t.Fatalf("handler saw peer %q, want %q", peer, tc.peer)
			}
		})
	}
}

// The ban is on the identity, not the token: a blocked peer stays out
// holding a perfectly valid one.
func TestGuardRejectsBlockedPeer(t *testing.T) {
	t.Parallel()

	secret := []byte(testSecret)

	guard := jwtauth.Guard{
		Secret:  jwtauth.NewSecret(secret),
		Blocked: blockedPeers{"alice": true},
	}

	token, err := jwtauth.Issue(secret, "alice", "ws://127.0.0.1:7000", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	status, reason, _ := attach(t, guard, token)
	if status != http.StatusForbidden || reason != jwtauth.ReasonBlocked {
		t.Fatalf("blocked peer: status %d reason %q, want %d %q",
			status, reason, http.StatusForbidden, jwtauth.ReasonBlocked)
	}
}

func TestGuardRejectsBadTokens(t *testing.T) {
	t.Parallel()

	secret := []byte(testSecret)

	expired, err := jwtauth.Issue(secret, "alice", "ws://127.0.0.1:7000", -time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	otherIssuer, err := jwtauth.Issue([]byte("a-completely-different-signing-secret"),
		"alice", "ws://127.0.0.1:7000", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		token string
	}{
		{"no token", ""},
		{"garbage", "not-a-jwt"},
		{"expired", expired},
		{"signed with another secret", otherIssuer},
	}

	guard := jwtauth.Guard{Secret: jwtauth.NewSecret(secret)}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			status, reason, _ := attach(t, guard, tc.token)
			if status != http.StatusUnauthorized || reason != jwtauth.ReasonUnauthorized {
				t.Fatalf("status %d reason %q, want %d %q",
					status, reason, http.StatusUnauthorized, jwtauth.ReasonUnauthorized)
			}
		})
	}
}

// Rotating the secret invalidates tokens already issued, on the very
// next attach and with no restart.
func TestSecretRotationInvalidatesTokens(t *testing.T) {
	t.Parallel()

	secret := jwtauth.NewSecret([]byte(testSecret))
	guard := jwtauth.Guard{Secret: secret}

	token, err := jwtauth.Issue(secret.Get(), "alice", "ws://127.0.0.1:7000", time.Hour)
	if err != nil {
		t.Fatal(err)
	}

	if status, _, _ := attach(t, guard, token); status != http.StatusOK {
		t.Fatalf("before rotation: status %d, want %d", status, http.StatusOK)
	}

	secret.Set([]byte("a-freshly-rotated-signing-secret-value"))

	if status, _, _ := attach(t, guard, token); status != http.StatusUnauthorized {
		t.Fatalf("after rotation: status %d, want %d", status, http.StatusUnauthorized)
	}
}
