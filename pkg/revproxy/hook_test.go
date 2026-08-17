package revproxy_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/openotters/holt/pkg/reqlog"
	"github.com/openotters/holt/pkg/revproxy"
)

// Every carried request is reported once the response is done, with the
// peer it went to: on the hub several tunnels share one output, so the
// peer is what tells the lines apart.
func TestRequestHookReportsCarriedRequest(t *testing.T) {
	t.Parallel()

	peers := fakePeers{tunnels: map[string]http.RoundTripper{
		"alice": roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusAccepted,
				Body:       http.NoBody,
				Header:     http.Header{},
				Request:    r,
			}, nil
		}),
	}}

	var got reqlog.Event

	proxy := revproxy.New(peers, revproxy.WithRequestHook(func(ev reqlog.Event) { got = ev }))

	req := httptest.NewRequest(http.MethodGet, "http://shop.example.com/status?deep=1", nil)
	req.Header.Set(revproxy.RouteHeader, "alice")
	req.Header.Set("User-Agent", "curl/8.7.1")
	proxy.ServeHTTP(httptest.NewRecorder(), req)

	if got.Peer != "alice" {
		t.Errorf("peer = %q, want alice", got.Peer)
	}

	// The details a console row opens to, read before routing rewrote
	// anything.
	if got.Query != "deep=1" || got.Host != "shop.example.com" || got.UserAgent != "curl/8.7.1" {
		t.Errorf("details = query %q host %q agent %q", got.Query, got.Host, got.UserAgent)
	}

	if got.Method != http.MethodGet || got.Path != "/status" {
		t.Errorf("got %s %s, want GET /status", got.Method, got.Path)
	}

	if got.Status != http.StatusAccepted {
		t.Errorf("status = %d, want 202", got.Status)
	}
}

// A request that named no peer never reached a tunnel, so it is
// reported with an empty peer and the code the landing page answered.
func TestRequestHookReportsUnroutedRequest(t *testing.T) {
	t.Parallel()

	var got reqlog.Event

	proxy := revproxy.New(fakePeers{}, revproxy.WithRequestHook(func(ev reqlog.Event) { got = ev }))

	proxy.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://placeholder/", nil))

	if got.Peer != "" {
		t.Errorf("peer = %q, want empty", got.Peer)
	}

	if got.Status != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", got.Status)
	}
}
