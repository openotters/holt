package proxy

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

// notAttachedError marks "no such live peer" so the error handler can
// answer 404 rather than 502 (the peer is not a failing upstream, it is
// simply absent). It never reaches the response body.
type notAttachedError struct{ peer string }

func (e notAttachedError) Error() string {
	if e.peer == "" {
		return "no target peer named (set the " + RouteHeader + " header)"
	}

	return "peer " + strconv.Quote(e.peer) + " is not attached"
}

// serveError renders a tunnel failure. An absent peer is a 404, not a
// 502 (it is not a failing upstream, it just is not there); a real
// transport error stays a 502. The body is only the holt swirl, never
// the peer name or any hub detail, so a proxy in front of the hub
// cannot leak anything from an error.
func (p *Proxy) serveError(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusBadGateway

	var na notAttachedError
	if errors.As(err, &na) {
		status = http.StatusNotFound
	}

	// gRPC callers read the status from the trailers, not the HTTP code:
	// an HTTP error page would surface as a parse failure instead.
	if strings.HasPrefix(r.Header.Get("Content-Type"), grpcContentType) {
		w.Header().Set("Content-Type", grpcContentType)
		w.Header().Set("Grpc-Status", grpcUnavailable)
		w.Header().Set("Grpc-Message", "unavailable")
		w.WriteHeader(http.StatusOK)

		return
	}

	writePage(w, r, status)
}

const (
	grpcContentType = "application/grpc"
	grpcUnavailable = "14" // UNAVAILABLE
)

// writePage writes a bare holt swirl, centered, and nothing else, so no
// peer name, address, or other hub state leaks through the proxy,
// whoever the caller is.
func writePage(w http.ResponseWriter, r *http.Request, status int) {
	if strings.Contains(r.Header.Get("Accept"), "text/html") {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, pageHTML)

		return
	}

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, "🌀\n")
}

// pageHTML is the swirl, centered, self-contained, no other text.
const pageHTML = `<!doctype html><html lang="en"><head><meta charset="utf-8">` +
	`<meta name="viewport" content="width=device-width,initial-scale=1">` +
	`<title>🌀</title><style>` +
	`html,body{height:100%;margin:0}` +
	`body{display:flex;align-items:center;justify-content:center;` +
	`background:#0b0f14;font-size:4rem;line-height:1}` +
	`</style></head><body>🌀</body></html>`
