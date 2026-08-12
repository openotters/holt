// Package style is the CLI's small output vocabulary: a success mark,
// a dim note, and a plain column list. Colors degrade to plain text
// automatically when stdout is not a terminal.
package style

import (
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/charmbracelet/lipgloss"
)

var (
	okMark   = lipgloss.NewStyle().Foreground(lipgloss.Color("35"))
	warnMark = lipgloss.NewStyle().Foreground(lipgloss.Color("214")).Bold(true)
	dim      = lipgloss.NewStyle().Faint(true)
)

// Success renders a green check followed by the message.
func Success(format string, a ...any) string {
	return okMark.Render("✓") + " " + fmt.Sprintf(format, a...)
}

// Warn renders an amber warning line.
func Warn(format string, a ...any) string {
	return warnMark.Render("⚠ " + fmt.Sprintf(format, a...))
}

// Note renders a dim informational line.
func Note(format string, a ...any) string {
	return dim.Render(fmt.Sprintf(format, a...))
}

// Output tells commands which rendering mode the CLI runs in. It is
// built once in main from --log-format: pretty gets banners and
// styled text, json stays machine-only.
type Output struct {
	Pretty bool
}

// BannerRow is one aligned "key  value  hint" line of a Banner.
type BannerRow struct {
	Key   string
	Value string
	Hint  string
}

var (
	title    = lipgloss.NewStyle().Bold(true)
	rowKey   = lipgloss.NewStyle().Faint(true)
	rowValue = lipgloss.NewStyle().Bold(true)
)

// Banner renders a welcome block: a bold title, aligned key/value
// rows with dim hints, and an optional closing hint line. Values are
// aligned up to a cap so one long path cannot push every hint away.
func Banner(heading string, rows []BannerRow, hint string) string {
	const valueCap = 36

	keyWidth, valueWidth := 0, 0
	for _, r := range rows {
		keyWidth = max(keyWidth, len(r.Key))

		if len(r.Value) <= valueCap {
			valueWidth = max(valueWidth, len(r.Value))
		}
	}

	out := "\n" + title.Render("🌀 "+heading) + "\n\n"
	for _, r := range rows {
		out += fmt.Sprintf("  %s  %s", rowKey.Render(pad(r.Key, keyWidth)), rowValue.Render(pad(r.Value, valueWidth)))
		if r.Hint != "" {
			out += "  " + dim.Render(r.Hint)
		}

		out += "\n"
	}

	if hint != "" {
		// Trailing blank line separates the banner from the logs that
		// follow it.
		out += "\n  " + dim.Render(hint) + "\n\n"
	}

	return out
}

func pad(s string, w int) string {
	for len(s) < w {
		s += " "
	}

	return s
}

// List renders plain, space-aligned columns under uppercase headers,
// the way `kubectl get` prints: no borders, two spaces between columns.
func List(headers []string, rows [][]string) string {
	var b strings.Builder

	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, strings.Join(headers, "\t"))

	for _, r := range rows {
		fmt.Fprintln(w, strings.Join(r, "\t"))
	}

	_ = w.Flush()

	return strings.TrimRight(b.String(), "\n")
}
