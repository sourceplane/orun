// Package tui implements the Orun Cockpit terminal UI as a component-native
// control plane over Orun internal packages. The TUI never shells out to
// `orun`; all data and actions flow through services.OrunService.
package tui

import "github.com/charmbracelet/lipgloss"

// Color tokens. Kept small and tasteful for the first cut; theme work
// expands in Phase 2.
var (
	colorFG          = lipgloss.AdaptiveColor{Light: "#1f2933", Dark: "#e0e6f0"}
	colorFGDim       = lipgloss.AdaptiveColor{Light: "#6b7280", Dark: "#7a869a"}
	colorAccent      = lipgloss.AdaptiveColor{Light: "#2563eb", Dark: "#7dd3fc"}
	colorBorder      = lipgloss.AdaptiveColor{Light: "#cbd5e1", Dark: "#3b4252"}
	colorBorderFocus = lipgloss.AdaptiveColor{Light: "#2563eb", Dark: "#7dd3fc"}
	colorError       = lipgloss.AdaptiveColor{Light: "#b91c1c", Dark: "#fca5a5"}
	colorSuccess     = lipgloss.AdaptiveColor{Light: "#15803d", Dark: "#86efac"}
	colorWarning     = lipgloss.AdaptiveColor{Light: "#a16207", Dark: "#fde68a"}
)

// Style constants. View files must use these and never inline lipgloss
// literals.
var (
	StylePanel = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	StylePanelFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorderFocus).
				Padding(0, 1)

	StyleStatusBar = lipgloss.NewStyle().
			Foreground(colorFG).
			Padding(0, 1)

	StyleKeyHint = lipgloss.NewStyle().
			Foreground(colorFGDim).
			Padding(0, 1)

	StyleErrorBanner = lipgloss.NewStyle().
				Foreground(colorError).
				Padding(0, 1)

	StyleLoading = lipgloss.NewStyle().
			Foreground(colorAccent).
			Padding(1, 2)

	StyleDimmed = lipgloss.NewStyle().Foreground(colorFGDim)
	StyleTitle  = lipgloss.NewStyle().Bold(true).Foreground(colorFG)
	StyleAccent = lipgloss.NewStyle().Foreground(colorAccent)
)
