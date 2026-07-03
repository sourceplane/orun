package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// brandRamp is the amber gradient the animated brand mark flows through.
// The ramp loops (deep → bright → deep) so advancing the phase produces a
// continuous shimmer sweeping across the wordmark. Colors stay inside the
// cockpit's brand family (style.DefaultPalette amber — the Orun Cloud brand).
var brandRamp = []lipgloss.AdaptiveColor{
	{Light: "#b45309", Dark: "#d97706"},
	{Light: "#d97706", Dark: "#f59e0b"},
	{Light: "#f59e0b", Dark: "#fbbf24"},
	{Light: "#fbbf24", Dark: "#fcd34d"},
	{Light: "#fcd34d", Dark: "#fde68a"},
	{Light: "#fbbf24", Dark: "#fcd34d"},
	{Light: "#f59e0b", Dark: "#fbbf24"},
	{Light: "#d97706", Dark: "#f59e0b"},
}

// brandText is the cockpit wordmark. Single-width glyph + ASCII only, so the
// animated mark can never widen a header line past its measured width.
const brandText = "◆ orun"

// BrandMark renders the animated wordmark for the given animation phase:
// each rune takes its color from the ramp offset by the phase, so successive
// phases sweep a highlight across the mark. The visible text and width are
// identical for every phase — only colors change — keeping the header line
// stable for the renderer.
func BrandMark(phase int) string {
	runes := []rune(brandText)
	var b strings.Builder
	for i, r := range runes {
		if r == ' ' {
			b.WriteRune(' ')
			continue
		}
		idx := (i + phase) % len(brandRamp)
		if idx < 0 {
			idx += len(brandRamp)
		}
		b.WriteString(lipgloss.NewStyle().Foreground(brandRamp[idx]).Bold(true).Render(string(r)))
	}
	return b.String()
}
