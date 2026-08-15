//nolint:testpackage // jwtMiddleware and secretState are unexported; white-box unit test on purpose.
package commands

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/openotters/holt/cmd/holt/internal/jwtauth"
)

// A token minted before peer names were constrained (or by another
// issuer) can carry a name no hostname strategy could route. The
// attach path must refuse it even though the signature is valid,
// otherwise the peer attaches under a name the proxy cannot address.
func TestJWTMiddleware_RejectsUnroutablePeerName(t *testing.T) {
	t.Parallel()

	secret := []byte("test-secret-value-for-signing-only")

	secrets := &secretState{}
	secrets.set(secret)

	blocks := &blockList{blocked: map[string]int64{}}
	metrics := newHubMetrics("test", "test")

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := jwtMiddleware(secrets, blocks, metrics, next)

	cases := []struct {
		name       string
		peer       string
		wantStatus int
	}{
		{"routable name attaches", "api-gateway-1", http.StatusOK},
		{"uppercase refused", "Alice", http.StatusForbidden},
		{"underscore refused", "my_service", http.StatusForbidden},
		{"dotted name refused", "a.b", http.StatusForbidden},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			jwt, err := jwtauth.Issue(secret, tc.peer, time.Hour)
			if err != nil {
				t.Fatal(err)
			}

			req := httptest.NewRequest(http.MethodGet, "/", nil)
			req.Header.Set("Authorization", "Bearer "+jwt)

			rec := httptest.NewRecorder()
			handler.ServeHTTP(rec, req)

			if rec.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d (body %q)", rec.Code, tc.wantStatus, rec.Body.String())
			}
		})
	}
}
