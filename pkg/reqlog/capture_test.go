package reqlog_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/openotters/holt/pkg/reqlog"
)

// With capture on, both payloads are reported and the handler still
// reads exactly what the client sent.
func TestCaptureBodies(t *testing.T) {
	t.Parallel()

	var got reqlog.Event

	var read string

	handler := reqlog.Middleware(
		func(ev reqlog.Event) { got = ev },
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			body, _ := io.ReadAll(r.Body)
			read = string(body)

			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"id":"ord_123"}`))
		}),
		reqlog.WithHeaders(), reqlog.WithBodyLimit(4096),
	)

	req := httptest.NewRequest(http.MethodPost, "/v1/orders", strings.NewReader(`{"sku":"otter-1"}`))
	req.Header.Set("Content-Type", "application/json")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if read != `{"sku":"otter-1"}` {
		t.Fatalf("handler read %q: capture changed what the handler sees", read)
	}

	if string(got.RequestBody.Content) != `{"sku":"otter-1"}` || got.RequestBody.Truncated {
		t.Errorf("request body = %+v", got.RequestBody)
	}

	if string(got.ResponseBody.Content) != `{"id":"ord_123"}` {
		t.Errorf("response body = %+v", got.ResponseBody)
	}

	if got.RequestHeaders["Content-Type"] != "application/json" {
		t.Errorf("request headers = %v", got.RequestHeaders)
	}

	if got.ResponseHeaders["Content-Type"] != "application/json" {
		t.Errorf("response headers = %v", got.ResponseHeaders)
	}
}

// A body past the limit is reported as a prefix that says so, with the
// full size next to it, so nobody reads part of a payload as all of it.
func TestCaptureTruncates(t *testing.T) {
	t.Parallel()

	var got reqlog.Event

	long := strings.Repeat("x", 9000)

	handler := reqlog.Middleware(
		func(ev reqlog.Event) { got = ev },
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			w.Header().Set("Content-Type", "text/plain")
			_, _ = w.Write([]byte(long))
		}),
		reqlog.WithBodyLimit(4096),
	)

	req := httptest.NewRequest(http.MethodPost, "/bulk", strings.NewReader(long))
	req.Header.Set("Content-Type", "text/plain")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	for name, body := range map[string]reqlog.Body{"request": got.RequestBody, "response": got.ResponseBody} {
		if len(body.Content) != 4096 || !body.Truncated || body.Size != 9000 {
			t.Errorf("%s body: %d bytes captured, truncated=%v, size=%d; want 4096 / true / 9000",
				name, len(body.Content), body.Truncated, body.Size)
		}
	}
}

// A payload that is not text is named rather than shown: an image in a
// viewer is noise, and the bytes were never worth carrying.
func TestCaptureSkipsBinary(t *testing.T) {
	t.Parallel()

	var got reqlog.Event

	handler := reqlog.Middleware(
		func(ev reqlog.Event) { got = ev },
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "image/png")
			_, _ = w.Write(make([]byte, 500))
		}),
		reqlog.WithBodyLimit(4096),
	)

	handler.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/logo.png", nil))

	if got.ResponseBody.Skipped != reqlog.SkippedBinary || len(got.ResponseBody.Content) != 0 {
		t.Errorf("response body = %+v, want skipped as binary with no content", got.ResponseBody)
	}

	if got.ResponseBody.Size != 500 {
		t.Errorf("size = %d, want 500: a skipped body is still counted", got.ResponseBody.Size)
	}
}

// Without the options nothing of the payload is reported, and a body
// that crossed is reported as such rather than as absent.
func TestCaptureOffByDefault(t *testing.T) {
	t.Parallel()

	var got reqlog.Event

	handler := reqlog.Middleware(
		func(ev reqlog.Event) { got = ev },
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_, _ = io.Copy(io.Discard, r.Body)
			_, _ = w.Write([]byte("hello"))
		}),
	)

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader("secret payload"))
	req.Header.Set("Authorization", "Bearer nope")

	handler.ServeHTTP(httptest.NewRecorder(), req)

	if got.RequestHeaders != nil || got.ResponseHeaders != nil {
		t.Errorf("headers reported without asking: %v / %v", got.RequestHeaders, got.ResponseHeaders)
	}

	if len(got.RequestBody.Content) != 0 || len(got.ResponseBody.Content) != 0 {
		t.Error("a body was captured with capture off")
	}

	if got.RequestBody.Skipped != reqlog.SkippedDisabled {
		t.Errorf("request body = %+v, want it named as disabled", got.RequestBody)
	}
}

// Credentials are reported as present, never as themselves.
func TestHeadersRedactCredentials(t *testing.T) {
	t.Parallel()

	h := http.Header{}
	h.Set("Authorization", "Bearer supersecret")
	h.Set("Cookie", "session=abc")
	h.Set("X-Api-Key", "k-123")
	h.Set("Accept", "application/json")
	h.Add("X-Forwarded-For", "10.0.0.1")
	h.Add("X-Forwarded-For", "10.0.0.2")

	got := reqlog.Headers(h)

	for _, name := range []string{"Authorization", "Cookie", "X-Api-Key"} {
		if got[name] != reqlog.Redacted {
			t.Errorf("%s = %q, want it redacted", name, got[name])
		}
	}

	if got["Accept"] != "application/json" {
		t.Errorf("Accept = %q", got["Accept"])
	}

	// Repeats travel joined, the way they read on the wire.
	if got["X-Forwarded-For"] != "10.0.0.1, 10.0.0.2" {
		t.Errorf("X-Forwarded-For = %q", got["X-Forwarded-For"])
	}
}
