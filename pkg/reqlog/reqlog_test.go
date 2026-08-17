package reqlog_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openotters/holt/pkg/reqlog"
)

// The middleware reports what the handler actually did: the request as
// received, the code it answered with, and a duration that was measured
// rather than left zero.
func TestMiddlewareReportsRequest(t *testing.T) {
	t.Parallel()

	var got reqlog.Event

	handler := reqlog.Middleware(
		func(ev reqlog.Event) { got = ev },
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		}),
	)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/things/7", nil))

	if got.Method != http.MethodPost || got.Path != "/things/7" {
		t.Errorf("got %s %s, want POST /things/7", got.Method, got.Path)
	}

	if got.Status != http.StatusNotFound {
		t.Errorf("status = %d, want 404", got.Status)
	}

	if got.At.IsZero() {
		t.Error("event has no timestamp")
	}

	if got.Duration < 0 {
		t.Errorf("duration = %s, want a measured duration", got.Duration)
	}
}

// A handler that writes a body without a code sent 200, which is what
// net/http put on the wire, so that is what the event says.
func TestMiddlewareDefaultsToOK(t *testing.T) {
	t.Parallel()

	var got reqlog.Event

	handler := reqlog.Middleware(
		func(ev reqlog.Event) { got = ev },
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("hello"))
		}),
	)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if got.Status != http.StatusOK {
		t.Errorf("status = %d, want 200", got.Status)
	}
}

// Without a hook there is nothing to report, so the handler is used as
// it is rather than wrapped for nobody.
func TestMiddlewareWithoutHook(t *testing.T) {
	t.Parallel()

	rec := httptest.NewRecorder()
	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})

	reqlog.Middleware(nil, next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusTeapot {
		t.Errorf("code = %d, want 418", rec.Code)
	}
}

// The first code wins: a handler that writes twice (an error handler
// running after headers went out) does not rewrite history.
func TestRecorderKeepsFirstStatus(t *testing.T) {
	t.Parallel()

	rec := reqlog.NewRecorder(httptest.NewRecorder())
	rec.WriteHeader(http.StatusBadGateway)
	rec.WriteHeader(http.StatusOK)

	if rec.Status() != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Status())
	}
}

// Streaming keeps working through the recorder: a flush reaches the
// writer underneath instead of stopping at the wrapper.
func TestRecorderFlushes(t *testing.T) {
	t.Parallel()

	under := httptest.NewRecorder()
	rec := reqlog.NewRecorder(under)

	_, _ = rec.Write([]byte("chunk"))
	rec.Flush()

	if !under.Flushed {
		t.Error("flush did not reach the underlying writer")
	}

	if rec.Unwrap() != http.ResponseWriter(under) {
		t.Error("unwrap did not return the underlying writer")
	}
}

// The event carries the request as received, so a details view can be
// built from it: what was asked, over what, by whom, and how big both
// halves were.
func TestMiddlewareReportsDetails(t *testing.T) {
	t.Parallel()

	var got reqlog.Event

	handler := reqlog.Middleware(
		func(ev reqlog.Event) { got = ev },
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// A handler is free to drain the body and rewrite the URL
			// it was given; the event must not depend on either.
			_, _ = io.Copy(io.Discard, r.Body)
			r.URL.Path = "/rewritten"

			_, _ = w.Write([]byte("response body"))
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/orders?ref=twitter&utm=x", strings.NewReader(`{"a":1}`))
	req.Host = "shop.example.com"
	req.Header.Set("User-Agent", "curl/8.7.1")
	req.RemoteAddr = "10.0.0.9:54321"

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if got.Path != "/orders" || got.Query != "ref=twitter&utm=x" {
		t.Errorf("path/query = %q %q, want /orders and the raw query", got.Path, got.Query)
	}

	if got.Host != "shop.example.com" || got.UserAgent != "curl/8.7.1" || got.RemoteAddr != "10.0.0.9:54321" {
		t.Errorf("host/agent/client = %q %q %q", got.Host, got.UserAgent, got.RemoteAddr)
	}

	if got.Proto == "" {
		t.Error("event carries no protocol")
	}

	if got.RequestBytes != 7 {
		t.Errorf("request bytes = %d, want 7", got.RequestBytes)
	}

	if got.ResponseBytes != int64(len("response body")) {
		t.Errorf("response bytes = %d, want %d", got.ResponseBytes, len("response body"))
	}
}
