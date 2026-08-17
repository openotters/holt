package revproxy_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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

// The proxy carries no payload unless asked: a hub that keeps none
// cannot leak one, and streaming costs nothing.
func TestRequestHookCaptureIsOptIn(t *testing.T) {
	t.Parallel()

	peers := fakePeers{tunnels: map[string]http.RoundTripper{
		"alice": roundTripFunc(func(r *http.Request) (*http.Response, error) {
			// A real transport reads the request body on its way out,
			// which is what feeds the capture.
			_, _ = io.Copy(io.Discard, r.Body)

			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"ok":true}`)),
				Header:     http.Header{"Content-Type": {"application/json"}},
				Request:    r,
			}, nil
		}),
	}}

	send := func(opts ...revproxy.Option) reqlog.Event {
		t.Helper()

		var got reqlog.Event

		req := httptest.NewRequest(http.MethodPost, "http://placeholder/orders",
			strings.NewReader(`{"sku":"otter-1"}`))
		req.Header.Set(revproxy.RouteHeader, "alice")
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer supersecret")

		opts = append(opts, revproxy.WithRequestHook(func(ev reqlog.Event) { got = ev }))
		revproxy.New(peers, opts...).ServeHTTP(httptest.NewRecorder(), req)

		return got
	}

	if quiet := send(); quiet.RequestHeaders != nil || len(quiet.RequestBody.Content) > 0 {
		t.Errorf("payload reported without asking: %v %q", quiet.RequestHeaders, quiet.RequestBody.Content)
	}

	loud := send(revproxy.WithRequestCapture(4096))
	if string(loud.RequestBody.Content) != `{"sku":"otter-1"}` {
		t.Errorf("request body = %q", loud.RequestBody.Content)
	}

	if string(loud.ResponseBody.Content) != `{"ok":true}` {
		t.Errorf("response body = %q", loud.ResponseBody.Content)
	}

	// The hub sees the credential and never repeats it.
	if loud.RequestHeaders["Authorization"] != reqlog.Redacted {
		t.Errorf("Authorization = %q, want it redacted", loud.RequestHeaders["Authorization"])
	}
}
