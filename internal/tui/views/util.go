package views

// util.go holds the rendering helpers shared across the cockpit's views.
// Anything used by more than one view lives here so a fix lands once.

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/sourceplane/orun/internal/tui/theme"
)

// truncate clips s to at most w visible cells, appending "…" when clipped.
// ANSI- and rune-aware (a byte slice would split escapes or multi-byte runes).
func truncate(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= w {
		return s
	}
	if w <= 1 {
		return "…"
	}
	return ansi.Truncate(s, w, "…")
}

// pad clips s to w visible cells (rune-aware) and pads with spaces so the
// result is always exactly w cells — truncating a double-width rune can land
// one cell short, so padding happens after the clip.
func pad(s string, w int) string {
	if w <= 0 {
		return ""
	}
	if lipgloss.Width(s) > w {
		s = truncate(s, w)
	}
	return s + strings.Repeat(" ", w-lipgloss.Width(s))
}

// padStyled pads `styled` (which may contain ANSI) so its *visible* width
// equals w, using the unstyled `raw` to measure.
func padStyled(styled, raw string, w int) string {
	rawW := lipgloss.Width(raw)
	if rawW >= w {
		return pad(raw, w)
	}
	return styled + strings.Repeat(" ", w-rawW)
}

// viewportWindow returns the [start, end) slice bounds that keep cursor
// visible inside a viewport of at most max rows over total rows. Every
// scrolling list in the cockpit (browse, history, activity, catalog) windows
// its rows through this one helper.
func viewportWindow(cursor, total, max int) (int, int) {
	if max < 1 {
		max = 1
	}
	start := 0
	if cursor >= max {
		start = cursor - max + 1
	}
	end := start + max
	if end > total {
		end = total
	}
	if start > end {
		start = end
	}
	return start, end
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// zoa renders an em-dash for an absent value so empty table cells stay scannable.
func zoa(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// centerCard renders a centered message card with a hint.
func centerCard(width, height int, msg string) string {
	if width <= 0 {
		width = 40
	}
	// Clamp the message to fit comfortably inside the available width.
	maxMsg := width - 8
	if maxMsg < 12 {
		maxMsg = 12
	}
	if len(msg) > maxMsg {
		// soft-wrap by inserting line breaks at word boundaries
		words := strings.Fields(msg)
		var lines []string
		cur := ""
		for _, w := range words {
			if cur == "" {
				cur = w
				continue
			}
			if len(cur)+1+len(w) > maxMsg {
				lines = append(lines, cur)
				cur = w
			} else {
				cur += " " + w
			}
		}
		if cur != "" {
			lines = append(lines, cur)
		}
		msg = strings.Join(lines, "\n")
	}
	card := theme.StyleModalCard.Render(theme.StyleDim.Render(msg))
	if height <= 0 {
		return lipgloss.Place(width, 6, lipgloss.Center, lipgloss.Center, card)
	}
	return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, card)
}
