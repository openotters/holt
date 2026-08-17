// Package reqlog is the live view of requests crossing a tunnel.
//
// Both ends see the same request and can report it: the hub as it
// proxies (revproxy.WithRequestHook) and the peer as it serves
// (dial.Options.RequestHook). Neither stores anything. A hook is
// called once per request, after the response, and whatever it does
// with the event is the application's business: print it, count it,
// ship it somewhere.
//
// The holt CLI uses it for the line `holt expose` and `holt hub`
// print as traffic goes through, which is the whole point: a tunnel
// you cannot watch is hard to trust.
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
	// Query is the raw query string without "?", empty when there is
	// none. It is kept apart from Path so a list stays readable and a
	// reader can still see what was asked.
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

// Middleware wraps a handler so every request it serves is reported.
// It is what the peer side uses, and it works on any http.Handler.
func Middleware(hook Hook, next http.Handler) http.Handler {
	if hook == nil {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Read from the request before the handler runs: a handler is
		// free to consume the body and rewrite the URL it was given.
		ev := From(r)
		rec := NewRecorder(w)
		start := time.Now()

		next.ServeHTTP(rec, r)

		ev.At = time.Now()
		ev.Status = rec.Status()
		ev.ResponseBytes = rec.Written()
		ev.Duration = time.Since(start)

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
}

// NewRecorder wraps w. Until the handler writes, the status reads as
// 200, which is what net/http sends for a handler that writes a body
// without calling WriteHeader.
func NewRecorder(w http.ResponseWriter) *Recorder {
	return &Recorder{ResponseWriter: w, status: http.StatusOK}
}

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
