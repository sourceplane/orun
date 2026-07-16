package frame

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Fit renders content into exactly size: exactly size.Height lines, each
// exactly size.Width display cells wide. Overflow is truncated (ANSI-aware),
// underflow is padded with spaces. Leaf renderers call Fit as the last step
// of producing their box; everything above them can then treat dimensions as
// facts rather than hopes.
func Fit(content string, size Size) string {
	if size.Empty() {
		return ""
	}
	lines := strings.Split(content, "\n")
	if len(lines) > size.Height {
		lines = lines[:size.Height]
	}
	out := make([]string, size.Height)
	for i := range out {
		var line string
		if i < len(lines) {
			line = lines[i]
		}
		out[i] = FitLine(line, size.Width)
	}
	return strings.Join(out, "\n")
}

// FitLine renders one line at exactly width display cells.
func FitLine(line string, width int) string {
	if width <= 0 {
		return ""
	}
	w := ansi.StringWidth(line)
	switch {
	case w == width:
		return line
	case w > width:
		line = ansi.Truncate(line, width, "")
		// Truncating on a wide rune can land one cell short; make it exact.
		if got := ansi.StringWidth(line); got < width {
			line += strings.Repeat(" ", width-got)
		}
		return line
	default:
		return line + strings.Repeat(" ", width-w)
	}
}

// LineWidth measures a line's display width, ANSI-aware — the measurement
// every layout decision in the cockpit uses.
func LineWidth(line string) int { return ansi.StringWidth(line) }

// Check verifies that out satisfies the exact-size contract. It is the
// assertion behind the frame invariants (design §13.1) — used by tests and
// debug builds, never needed at runtime because Fit makes violations
// unrepresentable at the leaves.
func Check(out string, size Size) error {
	if size.Empty() {
		if out != "" {
			return fmt.Errorf("frame: non-empty render for empty size %+v", size)
		}
		return nil
	}
	lines := strings.Split(out, "\n")
	if len(lines) != size.Height {
		return fmt.Errorf("frame: %d lines, want %d", len(lines), size.Height)
	}
	for i, line := range lines {
		if w := ansi.StringWidth(line); w != size.Width {
			return fmt.Errorf("frame: line %d is %d cells, want %d", i, w, size.Width)
		}
	}
	return nil
}
