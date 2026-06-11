package theme

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// brandRamp is the violet gradient the animated brand mark flows through.
// The ramp loops (dark → light → dark) so advancing the phase produces a
// continuous shimmer sweeping across the wordmark. Colors stay inside the
// cockpit's brand family (style.DefaultPalette violets).
var brandRamp = []lipgloss.AdaptiveColor{
	{Light: "#6d28d9", Dark: "#7c3aed"},
	{Light: "#7c3aed", Dark: "#8b5cf6"},
	{Light: "#8b5cf6", Dark: "#a78bfa"},
	{Light: "#a78bfa", Dark: "#c4b5fd"},
	{Light: "#c4b5fd", Dark: "#ede9fe"},
	{Light: "#a78bfa", Dark: "#c4b5fd"},
	{Light: "#8b5cf6", Dark: "#a78bfa"},
	{Light: "#7c3aed", Dark: "#8b5cf6"},
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
