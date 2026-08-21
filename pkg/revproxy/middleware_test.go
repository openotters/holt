package revproxy_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openotters/holt/pkg/reqlog"
	"github.com/openotters/holt/pkg/revproxy"
)

// Middleware runs before routing, so what a stage writes into the
// request is what the resolvers read: a stage that names the peer has
// routed the request. Stages run in the order they were given.
func TestMiddlewareMutatesBeforeRouting(t *testing.T) {
	t.Parallel()

	var gotTrace string

	alice := roundTripFunc(func(r *http.Request) (*http.Response, error) {
		gotTrace = r.Header.Get("X-Trace")

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       http.NoBody,
			Header:     http.Header{},
			Request:    r,
		}, nil
	})

	appendTrace := func(mark string) revproxy.Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r.Header.Set("X-Trace", r.Header.Get("X-Trace")+mark)
				next.ServeHTTP(w, r)
			})
		}
	}

	route := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			r.Header.Set(revproxy.RouteHeader, "alice")
			next.ServeHTTP(w, r)
		})
	}

	var got reqlog.Event

	proxy := revproxy.New(
		fakePeers{tunnels: map[string]http.RoundTripper{"alice": alice}},
		revproxy.WithMiddleware(appendTrace("a"), appendTrace("b"), route),
	)

	rec := httptest.NewRecorder()
	handler := watch(proxy, func(ev reqlog.Event) { got = ev })
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://placeholder/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusOK)
	}

	if gotTrace != "ab" {
		t.Errorf("peer saw trace %q, want ab (stages in the order given)", gotTrace)
	}

	// The request log learns the peer even though only a stage named it.
	if got.Peer != "alice" {
		t.Errorf("hook saw peer %q, want alice", got.Peer)
	}
}

// A stage that answers the request itself never reaches routing, but
// the request log wrapping the proxy still reports it.
func TestMiddlewareShortCircuitIsObserved(t *testing.T) {
	t.Parallel()

	deny := func(http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusForbidden)
		})
	}

	var got reqlog.Event

	proxy := revproxy.New(fakePeers{}, revproxy.WithMiddleware(deny))

	rec := httptest.NewRecorder()
	handler := watch(proxy, func(ev reqlog.Event) { got = ev })
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "http://placeholder/admin", nil))

	if rec.Code != http.StatusForbidden {
		t.Fatalf("got status %d, want %d", rec.Code, http.StatusForbidden)
	}

	if got.Status != http.StatusForbidden || got.Path != "/admin" {
		t.Errorf("hook saw status %d path %q, want 403 /admin", got.Status, got.Path)
	}

	if got.Peer != "" {
		t.Errorf("hook saw peer %q, want empty (routing never ran)", got.Peer)
	}
}
