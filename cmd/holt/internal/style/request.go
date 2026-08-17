package style

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/openotters/holt/pkg/reqlog"
)

// The request line's own palette. Status carries the meaning, so it is
// the only part that changes colour, and it changes by class: a reader
// scanning a fast-moving list sees red or amber before reading digits.
var (
	reqFaint  = lipgloss.NewStyle().Faint(true)
	reqMethod = lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
	reqPeer   = lipgloss.NewStyle().Foreground(lipgloss.Color("140"))
	reqOK     = lipgloss.NewStyle().Foreground(lipgloss.Color("35"))
	reqWarn   = lipgloss.NewStyle().Foreground(lipgloss.Color("214"))
	reqErr    = lipgloss.NewStyle().Foreground(lipgloss.Color("203")).Bold(true)
)

// Request renders one request as a log message, in the shape a
// tunnel's operator reads all day:
//
//	GET  /about            200  12ms
//
// It carries no clock: this goes through the logger like every other
// line, so the timestamp, level and prefix are the logger's and a
// request sits in the same columns as the tunnel events around it.
//
// With a peer (a view where several tunnels share the output) the peer
// comes first, since that is what tells the lines apart.
func Request(ev reqlog.Event) string {
	status := fmt.Sprintf("%3d", ev.Status)
	if ev.Status == 0 {
		// No response: the connection died before one was written.
		status = "---"
	}

	var b strings.Builder

	if ev.Peer != "" {
		b.WriteString(reqPeer.Render(pad(ev.Peer, 20)))
		b.WriteString("  ")
	}

	b.WriteString(reqMethod.Render(pad(ev.Method, 6)))
	b.WriteString(" ")
	b.WriteString(pad(truncate(ev.Path, 40), 40))
	b.WriteString("  ")
	b.WriteString(statusStyle(ev.Status).Render(status))
	b.WriteString("  ")
	b.WriteString(reqFaint.Render(duration(ev.Duration)))

	return b.String()
}

// statusStyle colours by class: 2xx and 3xx are fine, 4xx is the
// caller's problem, 5xx (and no response at all) is ours.
func statusStyle(status int) lipgloss.Style {
	switch {
	case status == 0 || status >= 500:
		return reqErr
	case status >= 400:
		return reqWarn
	default:
		return reqOK
	}
}

// duration renders in units a human reads at a glance rather than
// nine digits of precision.
func duration(d time.Duration) string {
	switch {
	case d >= time.Second:
		return fmt.Sprintf("%.1fs", d.Seconds())
	case d >= time.Millisecond:
		return fmt.Sprintf("%dms", d.Milliseconds())
	default:
		return fmt.Sprintf("%dµs", d.Microseconds())
	}
}

// truncate keeps a long path from pushing the status off the line,
// cutting the middle so the start and the end both survive.
func truncate(s string, width int) string {
	if len(s) <= width {
		return s
	}

	keep := (width - 1) / 2

	return s[:keep] + "…" + s[len(s)-(width-keep-1):]
}
