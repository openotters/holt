package reqlog

import (
	"io"
	"net/http"
	"strings"
)

// Body is what was captured of one side's payload. Capture is bounded
// and best-effort: a body is a prefix, a reason it was not taken, or
// nothing at all when there was no body to take.
type Body struct {
	// Content is the captured prefix, empty when Skipped says why not.
	Content []byte
	// Size is how many bytes crossed in total, which is more than
	// len(Content) when Truncated.
	Size int64
	// Truncated reports that the body was longer than the limit.
	Truncated bool
	// Skipped names why nothing was captured: "disabled" (capture is
	// off), "binary" (a content type not worth showing as text), or
	// "" when Content is the answer.
	Skipped string
}

// The reasons a body was not captured.
const (
	SkippedDisabled = "disabled"
	SkippedBinary   = "binary"
)

// Redacted replaces the value of a header that carries a credential.
// The header is still reported — knowing a request was authenticated
// is worth seeing — but the secret itself never reaches a viewer.
const Redacted = "<redacted>"

// redacted reports whether a header's value is a secret by
// construction; compared lowercase.
func redacted(name string) bool {
	switch strings.ToLower(name) {
	case "authorization", "proxy-authorization", "cookie", "set-cookie", "x-api-key", "x-auth-token":
		return true
	default:
		return false
	}
}

// Headers flattens headers for reporting, joining repeats the way they
// travel on the wire and redacting the ones that carry credentials.
func Headers(h http.Header) map[string]string {
	if len(h) == 0 {
		return nil
	}

	out := make(map[string]string, len(h))

	for name, values := range h {
		if redacted(name) {
			out[name] = Redacted

			continue
		}

		out[name] = strings.Join(values, ", ")
	}

	return out
}

// captureText reports whether a content type is worth keeping as text.
// Images, archives and video would be noise at best in a viewer, so
// they are named rather than shown.
func captureText(contentType string) bool {
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	if mediaType == "" {
		// No type declared: a small form or a JSON body more often than
		// not, and the limit bounds the damage if not.
		return true
	}

	if strings.HasPrefix(mediaType, "text/") {
		return true
	}

	switch mediaType {
	case "application/json", "application/xml", "application/javascript",
		"application/x-www-form-urlencoded", "application/graphql", "application/ld+json",
		"application/problem+json", "application/x-ndjson", "application/yaml":
		return true
	}

	// The long tail of structured types: anything +json or +xml.
	return strings.HasSuffix(mediaType, "+json") || strings.HasSuffix(mediaType, "+xml")
}

// BodyCapture accumulates up to limit bytes while counting everything
// that passes. The zero value captures nothing.
type BodyCapture struct {
	limit int
	buf   []byte
	size  int64
}

// write feeds bytes through the capture, keeping the first limit of
// them.
func (c *BodyCapture) write(b []byte) {
	c.size += int64(len(b))

	if room := c.limit - len(c.buf); room > 0 {
		if len(b) > room {
			b = b[:room]
		}

		c.buf = append(c.buf, b...)
	}
}

// Body renders what was captured, given the content type it was
// declared with. A nil capture is a body nobody looked at.
func (c *BodyCapture) Body(contentType string) Body {
	if c == nil {
		return Body{}
	}

	if c.limit <= 0 {
		if c.size == 0 {
			return Body{}
		}

		return Body{Size: c.size, Skipped: SkippedDisabled}
	}

	if c.size == 0 {
		return Body{}
	}

	if !captureText(contentType) {
		return Body{Size: c.size, Skipped: SkippedBinary}
	}

	return Body{
		Content:   c.buf,
		Size:      c.size,
		Truncated: c.size > int64(len(c.buf)),
	}
}

// capturingBody wraps a request body so reading it also feeds the
// capture. The handler downstream reads exactly what it would have.
type capturingBody struct {
	io.ReadCloser
	capture *BodyCapture
}

func (b capturingBody) Read(p []byte) (int, error) {
	n, err := b.ReadCloser.Read(p)
	if n > 0 {
		b.capture.write(p[:n])
	}

	return n, err
}

// CaptureRequestBody replaces r.Body with one that records up to limit
// bytes as the handler reads it, and returns the capture to read
// afterwards. A limit of 0 records nothing but still counts the size,
// so a report can say a body was there.
func CaptureRequestBody(r *http.Request, limit int) *BodyCapture {
	capture := &BodyCapture{limit: limit}
	if r.Body == nil || r.Body == http.NoBody {
		return capture
	}

	r.Body = capturingBody{ReadCloser: r.Body, capture: capture}

	return capture
}
