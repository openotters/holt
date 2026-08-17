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

	req := httptest.NewRequest(http.MethodGet, "http://placeholder/status", nil)
	req.Header.Set(revproxy.RouteHeader, "alice")
	proxy.ServeHTTP(httptest.NewRecorder(), req)

	if got.Peer != "alice" {
		t.Errorf("peer = %q, want alice", got.Peer)
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
