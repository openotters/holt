//nolint:testpackage // proxyLanding / proxyError are unexported; white-box unit test on purpose.
package commands

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProxyLanding(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		accept   string
		wantType string
	}{
		{"curl gets text", "*/*", "text/plain"},
		{"browser gets html", "text/html,application/xhtml+xml", "text/html"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "http://placeholder/", nil)
			req.Header.Set("Accept", tc.accept)

			proxyLanding(rec, req)

			// A missing target is a client error, not a 502, so Cloudflare
			// and friends never show their scary bad-gateway page.
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got status %d, want %d", rec.Code, http.StatusBadRequest)
			}

			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, tc.wantType) {
				t.Fatalf("content-type %q, want prefix %q", ct, tc.wantType)
			}

			if !strings.Contains(rec.Body.String(), routeHeader) {
				t.Fatalf("body should mention the %q header, got: %s", routeHeader, rec.Body.String())
			}
		})
	}
}

func TestProxyErrorStatus(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want int
	}{
		{"absent peer is 404", notAttachedError{peer: "alice"}, http.StatusNotFound},
		{"transport failure is 502", errors.New("dial tcp: connection refused"), http.StatusBadGateway},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "http://placeholder/", nil)

			proxyError(rec, req, tc.err)

			if rec.Code != tc.want {
				t.Fatalf("got status %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

func TestProxyErrorGRPC(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://placeholder/svc/Method", nil)
	req.Header.Set("Content-Type", "application/grpc")

	proxyError(rec, req, notAttachedError{peer: "alice"})

	// gRPC callers get a trailer-less status, always 200 at the HTTP layer.
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	if got := rec.Header().Get("Grpc-Status"); got != "14" {
		t.Fatalf("grpc-status %q, want 14", got)
	}
}
