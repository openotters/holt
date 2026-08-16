package revproxy_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/openotters/holt/pkg/revproxy"
)

// fakePeers stands in for a *registry.Registry: whoever is in the map has a
// live tunnel.
type fakePeers struct {
	tunnels map[string]http.RoundTripper
}

func (f fakePeers) Attached(peer string) bool {
	_, ok := f.tunnels[peer]

	return ok
}

func (f fakePeers) RoundTripper(peer string) http.RoundTripper { return f.tunnels[peer] }

// roundTripFunc is a peer's tunnel, as a function.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A request that names no peer gets the landing page, not a proxied
// request, so hitting the proxy root reveals nothing and never shows a
// 502.
func TestLandingPage(t *testing.T) {
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

			revproxy.New(fakePeers{}).ServeHTTP(rec, req)

			// A missing target is a client error, not a 502, so Cloudflare
			// and friends never show their scary bad-gateway page.
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("got status %d, want %d", rec.Code, http.StatusBadRequest)
			}

			if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, tc.wantType) {
				t.Fatalf("content-type %q, want prefix %q", ct, tc.wantType)
			}

			// The page is only the swirl: no header names, addresses, or
			// other hub state may leak through the proxy.
			if !strings.Contains(rec.Body.String(), "🌀") {
				t.Fatalf("body should be the swirl, got: %s", rec.Body.String())
			}

			if strings.Contains(rec.Body.String(), revproxy.RouteHeader) {
				t.Fatalf("body must not leak the %q header, got: %s", revproxy.RouteHeader, rec.Body.String())
			}
		})
	}
}

func TestErrorStatus(t *testing.T) {
	t.Parallel()

	failing := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("dial tcp: connection refused")
	})

	cases := []struct {
		name string
		peer string
		want int
	}{
		{"absent peer is 404", "nobody", http.StatusNotFound},
		{"failing tunnel is 502", "alice", http.StatusBadGateway},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodGet, "http://placeholder/", nil)
			req.Header.Set(revproxy.RouteHeader, tc.peer)

			peers := fakePeers{tunnels: map[string]http.RoundTripper{"alice": failing}}
			revproxy.New(peers).ServeHTTP(rec, req)

			if rec.Code != tc.want {
				t.Fatalf("got status %d, want %d", rec.Code, tc.want)
			}

			// The error body must not echo the peer name or the raw error.
			if strings.Contains(rec.Body.String(), tc.peer) {
				t.Fatalf("body must not leak the peer name, got: %s", rec.Body.String())
			}
		})
	}
}

// gRPC callers read the status from the trailers: an HTML error page
// would surface as a parse failure instead of "unavailable".
func TestErrorGRPC(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "http://placeholder/svc/Method", nil)
	req.Header.Set("Content-Type", "application/grpc")
	req.Header.Set(revproxy.RouteHeader, "alice")

	revproxy.New(fakePeers{}).ServeHTTP(rec, req)

	// gRPC callers get a trailer-less status, always 200 at the HTTP layer.
	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	if got := rec.Header().Get("Grpc-Status"); got != "14" {
		t.Fatalf("grpc-status %q, want 14", got)
	}
}

// The happy path: the request reaches the named peer's tunnel with the
// routing header consumed, and the peer's response comes back whole.
func TestProxiesToAttachedPeer(t *testing.T) {
	t.Parallel()

	var (
		gotPath   string
		gotHeader string
	)

	alice := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotPath = r.URL.Path
		gotHeader = r.Header.Get(revproxy.RouteHeader)

		rec := httptest.NewRecorder()
		rec.WriteHeader(http.StatusTeapot)
		_, _ = rec.WriteString("served by alice")

		return rec.Result(), nil
	})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://placeholder/hello", nil)
	req.Host = "alice.peers.example.com"

	subdomains, err := revproxy.ResolveBySubdomain("peers.example.com")
	if err != nil {
		t.Fatal(err)
	}

	revproxy.New(
		fakePeers{tunnels: map[string]http.RoundTripper{"alice": alice}},
		revproxy.WithResolvers(subdomains),
	).ServeHTTP(rec, req)

	if rec.Code != http.StatusTeapot {
		t.Fatalf("got status %d, want %d (body %q)", rec.Code, http.StatusTeapot, rec.Body.String())
	}

	if rec.Body.String() != "served by alice" {
		t.Fatalf("body = %q, want the peer's response", rec.Body.String())
	}

	if gotPath != "/hello" {
		t.Fatalf("peer saw path %q, want /hello", gotPath)
	}

	// The routing header is the hub's business, not the peer's.
	if gotHeader != "" {
		t.Fatalf("peer saw the %s header (%q); it must be stripped", revproxy.RouteHeader, gotHeader)
	}
}

// The error hook is what the CLI counts proxy failures with, so every
// path that refuses a request has to report a reason.
func TestErrorHookReasons(t *testing.T) {
	t.Parallel()

	failing := roundTripFunc(func(*http.Request) (*http.Response, error) {
		return nil, errors.New("tunnel closed")
	})

	cases := []struct {
		name   string
		peer   string
		want   string
		routed bool
	}{
		{"no peer named", "", revproxy.ReasonNoPeer, false},
		{"peer not attached", "nobody", revproxy.ReasonNotAttached, true},
		{"tunnel fails", "alice", revproxy.ReasonTransport, true},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var (
				mu      sync.Mutex
				reasons []string
			)

			hook := func(_ context.Context, reason string) {
				mu.Lock()
				defer mu.Unlock()

				reasons = append(reasons, reason)
			}

			req := httptest.NewRequest(http.MethodGet, "http://placeholder/", nil)
			if tc.routed {
				req.Header.Set(revproxy.RouteHeader, tc.peer)
			}

			peers := fakePeers{tunnels: map[string]http.RoundTripper{"alice": failing}}
			revproxy.New(peers, revproxy.WithErrorHook(hook)).ServeHTTP(httptest.NewRecorder(), req)

			mu.Lock()
			defer mu.Unlock()

			if len(reasons) != 1 || reasons[0] != tc.want {
				t.Fatalf("hook saw %v, want exactly [%s]", reasons, tc.want)
			}
		})
	}
}
