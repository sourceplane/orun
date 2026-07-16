package design

import (
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/sourceplane/orun/internal/cockpit/style"
	"github.com/sourceplane/orun/internal/tui2/frame"
)

// Pill renders a compact toned chip — the terminal cell of the console's
// status pill.
func Pill(tone Tone, text string) string {
	return tone.Style().Render(text)
}

// StatusText renders a lifecycle status with its canonical glyph, toned.
// The glyph and label vocabularies come from internal/cockpit/style so the
// CLI, the v1 cockpit, and v2 never drift.
func StatusText(status string) string {
	return Pill(StatusTone(status), style.StatusGlyph(status)+" "+style.StatusLabel(status))
}

// LiveDot renders the running-work marker at an animation phase. It is the
// only component allowed to move, and only while work is live (design §4b).
func LiveDot(phase int) string {
	frames := []rune("⣾⣽⣻⢿⡿⣟⣯⣷")
	return ToneLive.Style().Render(string(frames[phase%len(frames)]))
}

// DataRow renders the console's data-row pattern at exactly width cells:
// selection marker, primary text, dim secondary, right-aligned status.
//
//	▸ checkout-service   payments · gold        ✓ succeeded
func DataRow(width int, selected bool, primary, secondary, right string) string {
	marker := "  "
	p := Text.Render(primary)
	if selected {
		marker = Selected.Render("▸ ")
		p = Selected.Render(primary)
	}
	left := " " + marker + p
	if secondary != "" {
		left += "  " + Dim.Render(secondary)
	}
	rw := ansi.StringWidth(right)
	lw := ansi.StringWidth(left)
	gap := width - lw - rw - 1
	if gap < 1 {
		return frame.FitLine(left, width)
	}
	return left + strings.Repeat(" ", gap) + right + " "
}

// StatTile renders a Home stat tile: dim label over an emphasized value,
// with an optional dim hint line. Three lines, exactly width cells each.
func StatTile(width int, label, value, hint string) string {
	lines := []string{
		Dim.Render(strings.ToLower(label)),
		Title.Render(value),
		Dim.Render(hint),
	}
	for i, l := range lines {
		lines[i] = frame.FitLine(" "+l, width)
	}
	return strings.Join(lines, "\n")
}

// Rule renders a muted horizontal rule.
func Rule(width int) string {
	return Muted.Render(strings.Repeat("─", width))
}

// KeyHint renders a keybinding hint pair ("r run").
func KeyHint(key, action string) string {
	return Text.Render(key) + " " + Dim.Render(action)
}

// Chips renders a filter-chip row; the active chip is accented.
//
//	all · running · failed
func Chips(active int, labels ...string) string {
	parts := make([]string, len(labels))
	for i, l := range labels {
		if i == active {
			parts[i] = Selected.Render(l)
		} else {
			parts[i] = Dim.Render(l)
		}
	}
	return strings.Join(parts, Muted.Render(" · "))
}

// Kind renders a catalog entity kind with its canonical glyph.
func Kind(kind string) string {
	return Dim.Render(style.EntityKindGlyph(kind)) + " " + Text.Render(kind)
}
