// Package reqlog is the live view of requests crossing a tunnel. Both
// ends see the same request and can report it: the hub as it proxies
// (revproxy.WithRequestHook) and the peer as it serves
// (dial.Options.RequestHook). Neither stores anything; a hook is
// called once per request, after the response.
package reqlog

import (
	"net/http"
	"time"
)

// Event is one request, seen from whichever end reports it. It is
// metadata only: no header values beyond the few named here, and never
// a body, so reporting a request costs what reading these fields costs
// and nothing that would have to be redacted later.
type Event struct {
	// At is when the response completed.
	At time.Time
	// Peer is the tunnel the request went through. The hub fills it;
	// a peer reporting its own requests leaves it empty, since it
	// knows only one.
	Peer string
	// Method and Path are the request's, as received.
	Method string
	Path   string
	// Query is the raw query string without "?", kept apart from Path
	// so a list stays readable.
	Query string
	// Host is the authority the request carried, which is what
	// subdomain routing reads.
	Host string
	// Proto is the client's protocol ("HTTP/1.1", "HTTP/2.0").
	Proto string
	// RemoteAddr is who the reporting end saw connecting: the client
	// for a hub behind nothing, otherwise whatever proxy fronts it.
	RemoteAddr string
	// UserAgent is the request's, empty when it sent none.
	UserAgent string
	// RequestBytes is the request's declared Content-Length, -1 when
	// it did not say (a streamed or chunked body).
	RequestBytes int64
	// ResponseBytes is what the handler actually wrote.
	ResponseBytes int64
	// RequestHeaders and ResponseHeaders are the headers as they
	// crossed, with credential-carrying values redacted (see Headers).
	// Nil when the reporting end does not collect them.
	RequestHeaders  map[string]string
	ResponseHeaders map[string]string
	// RequestBody and ResponseBody are bounded captures of the
	// payloads, empty unless the reporting end was built with a body
	// limit. See Body.
	RequestBody  Body
	ResponseBody Body
	// Status is the response code, 0 when the request never got one
	// (the connection died first).
	Status int
	// Duration is how long the response took at the reporting end,
	// so the hub's includes the tunnel hop and the peer's does not.
	Duration time.Duration
}

// From fills the request half of an Event from r. The reporting end
// adds what only it knows (the peer, the outcome, the timing).
func From(r *http.Request) Event {
	return Event{
		Method:       r.Method,
		Path:         r.URL.Path,
		Query:        r.URL.RawQuery,
		Host:         r.Host,
		Proto:        r.Proto,
		RemoteAddr:   r.RemoteAddr,
		UserAgent:    r.UserAgent(),
		RequestBytes: r.ContentLength,
	}
}

// Hook receives one event per request. It runs on the request's
// goroutine after the response, so it should not block: print, count,
// or hand off to a channel.
type Hook func(Event)

// Option configures what a Middleware or Recorder collects.
type Option func(*config)

// config is what the options build up.
type config struct {
	headers   bool
	bodyLimit int
}

// WithHeaders reports the request and response headers, with
// credential-carrying values redacted (see Headers).
func WithHeaders() Option {
	return func(c *config) { c.headers = true }
}

// WithBodyLimit captures up to limit bytes of each body, request and
// response. 0 (the default) captures none, which is the only setting
// that costs nothing and the only one that cannot leak a payload into
// whatever reads the events.
func WithBodyLimit(limit int) Option {
	return func(c *config) { c.bodyLimit = limit }
}

// Middleware wraps a handler so every request it serves is reported —
// metadata only by default; WithHeaders and WithBodyLimit add the
// payload, bounded.
func Middleware(hook Hook, next http.Handler, opts ...Option) http.Handler {
	if hook == nil {
		return next
	}

	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Before the handler runs: it may consume the body and rewrite
		// the URL.
		ev := From(r)

		if cfg.headers {
			ev.RequestHeaders = Headers(r.Header)
		}

		reqBody := CaptureRequestBody(r, cfg.bodyLimit)
		contentType := r.Header.Get("Content-Type")

		rec := NewRecorder(w, opts...)
		start := time.Now()

		next.ServeHTTP(rec, r)

		ev.At = time.Now()
		ev.Status = rec.Status()
		ev.ResponseBytes = rec.Written()
		ev.Duration = time.Since(start)
		ev.RequestBody = reqBody.Body(contentType)
		ev.ResponseBody = rec.Body()

		if cfg.headers {
			ev.ResponseHeaders = Headers(rec.Header())
		}

		hook(ev)
	})
}

// Recorder captures the status code of a response while forwarding
// everything else, so a handler behind it keeps streaming, flushing
// and hijacking as it would without one.
type Recorder struct {
	http.ResponseWriter
	status  int
	written bool
	bytes   int64
	capture BodyCapture
}

// NewRecorder wraps w. Until the handler writes, the status reads as
// 200, which is what net/http sends for a handler that writes a body
// without calling WriteHeader. With WithBodyLimit it also keeps a
// bounded prefix of the response, readable afterwards with Body.
func NewRecorder(w http.ResponseWriter, opts ...Option) *Recorder {
	var cfg config
	for _, opt := range opts {
		opt(&cfg)
	}

	return &Recorder{ResponseWriter: w, status: http.StatusOK, capture: BodyCapture{limit: cfg.bodyLimit}}
}

// Body is what was captured of the response, judged against the
// content type the handler declared.
func (r *Recorder) Body() Body { return r.capture.Body(r.Header().Get("Content-Type")) }

// Status is the code the handler sent.
func (r *Recorder) Status() int { return r.status }

func (r *Recorder) WriteHeader(code int) {
	if !r.written {
		r.status = code
		r.written = true
	}

	r.ResponseWriter.WriteHeader(code)
}

func (r *Recorder) Write(b []byte) (int, error) {
	r.written = true

	n, err := r.ResponseWriter.Write(b)
	r.bytes += int64(n)
	r.capture.write(b[:n])

	return n, err
}

// Written is how many bytes of body the handler wrote.
func (r *Recorder) Written() int64 { return r.bytes }

// Flush keeps streaming responses streaming: without it the wrapper
// would hide the underlying Flusher and the proxy's immediate flush
// would buffer.
func (r *Recorder) Flush() {
	if f, ok := r.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

// Unwrap lets http.ResponseController reach the real writer, which
// the standard library looks for (hijack, deadlines).
func (r *Recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }
