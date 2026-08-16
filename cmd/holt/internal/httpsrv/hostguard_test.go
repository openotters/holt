package httpsrv_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openotters/holt/cmd/holt/internal/httpsrv"
)

func TestHostGuard(t *testing.T) {
	t.Parallel()

	ok := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	cases := []struct {
		name    string
		allowed []string
		host    string
		path    string
		want    int
	}{
		{"loopback ip always allowed", nil, "127.0.0.1:7201", "/", http.StatusOK},
		{"localhost always allowed", nil, "localhost", "/", http.StatusOK},
		{"ipv6 loopback allowed", nil, "[::1]:7201", "/", http.StatusOK},
		{"unknown host rejected", nil, "evil.example.com", "/", http.StatusForbidden},
		{"rebinding to loopback via foreign host rejected", nil, "attacker.test", "/", http.StatusForbidden},
		{"configured host allowed", []string{"holt.example.com"}, "holt.example.com", "/", http.StatusOK},
		{"configured host with port allowed", []string{"holt.example.com"}, "holt.example.com:443", "/", http.StatusOK},
		{"wildcard disables the check", []string{"*"}, "anything.at.all", "/", http.StatusOK},
		{"case insensitive", []string{"Holt.Example.com"}, "holt.EXAMPLE.com", "/", http.StatusOK},
		// Probes reach the pod by IP, which no allow-list can predict.
		{"health check exempt", nil, "10.42.0.7:7201", "/healthz", http.StatusOK},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "http://placeholder"+tc.path, nil)
			req.Host = tc.host

			httpsrv.HostGuard(tc.allowed, ok).ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("host %q with allowed %v: got %d, want %d", tc.host, tc.allowed, rec.Code, tc.want)
			}
		})
	}
}
