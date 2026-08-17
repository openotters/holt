package reqlog_test

import (
	"net/http"
	"net/http/httptest"
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
