package style_test

import (
	"strings"
	"testing"
	"time"

	"github.com/openotters/holt/cmd/holt/internal/style"
	"github.com/openotters/holt/pkg/reqlog"
)

// The line carries what an operator scans for, and only names a peer
// when there is one to name (the peer side knows only itself). It
// carries no clock: this goes out through the logger, which stamps
// every line the same way.
func TestRequestLine(t *testing.T) {
	t.Parallel()

	at := time.Date(2026, time.August, 17, 9, 41, 5, 0, time.UTC)

	cases := []struct {
		name    string
		event   reqlog.Event
		want    []string
		notWant string
	}{
		{
			name: "a peer leads the line when there is one",
			event: reqlog.Event{
				At: at, Peer: "alice", Method: "GET", Path: "/about",
				Status: 200, Duration: 12 * time.Millisecond,
			},
			want:    []string{"alice", "GET", "/about", "200", "12ms"},
			notWant: "09:41:05",
		},
		{
			name:    "peer line has no peer column",
			event:   reqlog.Event{At: at, Method: "POST", Path: "/hook", Status: 500, Duration: 900 * time.Microsecond},
			want:    []string{"POST", "/hook", "500", "900µs"},
			notWant: "alice",
		},
		{
			name:  "no response reads as no status",
			event: reqlog.Event{At: at, Method: "GET", Path: "/", Status: 0, Duration: 2 * time.Second},
			want:  []string{"---", "2.0s"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			line := style.Request(tc.event)
			for _, want := range tc.want {
				if !strings.Contains(line, want) {
					t.Errorf("line %q does not contain %q", line, want)
				}
			}

			if tc.notWant != "" && strings.Contains(line, tc.notWant) {
				t.Errorf("line %q unexpectedly contains %q", line, tc.notWant)
			}
		})
	}
}

// A long path is cut in the middle so the status and duration stay
// where the eye expects them, keeping both ends of the path readable.
func TestRequestLineTruncatesLongPath(t *testing.T) {
	t.Parallel()

	long := "/api/v1/tenants/" + strings.Repeat("x", 80) + "/end"
	line := style.Request(reqlog.Event{Method: "GET", Path: long, Status: 200})

	if !strings.Contains(line, "/api/v1/") || !strings.Contains(line, "/end") {
		t.Errorf("line %q lost an end of the path", line)
	}

	if strings.Contains(line, strings.Repeat("x", 40)) {
		t.Errorf("line %q was not truncated", line)
	}
}
