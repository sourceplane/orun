// Package theme holds the cockpit's shared lipgloss palette and reusable
// styles. Both the root tui package and the per-view models in
// internal/tui/views import from here so the look stays consistent.
package theme

import "github.com/charmbracelet/lipgloss"

// Palette — a soft modern accent system inspired by Claude Code / Linear.
// All colors are AdaptiveColor so the cockpit reads well on both light
// and dark terminals.
var (
	colorFG      = lipgloss.AdaptiveColor{Light: "#1f2933", Dark: "#e2e8f0"}
	colorFGDim   = lipgloss.AdaptiveColor{Light: "#64748b", Dark: "#64748b"}
	colorFGMuted = lipgloss.AdaptiveColor{Light: "#94a3b8", Dark: "#475569"}

	colorAccent     = lipgloss.AdaptiveColor{Light: "#7c3aed", Dark: "#a78bfa"}
	colorAccentSoft = lipgloss.AdaptiveColor{Light: "#c4b5fd", Dark: "#6d28d9"}
	colorSecondary  = lipgloss.AdaptiveColor{Light: "#0891b2", Dark: "#22d3ee"}

	colorSuccess = lipgloss.AdaptiveColor{Light: "#059669", Dark: "#34d399"}
	colorWarning = lipgloss.AdaptiveColor{Light: "#b45309", Dark: "#fbbf24"}
	colorError   = lipgloss.AdaptiveColor{Light: "#dc2626", Dark: "#f87171"}

	colorBorder      = lipgloss.AdaptiveColor{Light: "#cbd5e1", Dark: "#334155"}
	colorBorderFocus = lipgloss.AdaptiveColor{Light: "#7c3aed", Dark: "#a78bfa"}

	colorRowAlt = lipgloss.AdaptiveColor{Light: "#f1f5f9", Dark: "#1e293b"}
	colorRowSel = lipgloss.AdaptiveColor{Light: "#ede9fe", Dark: "#312e81"}
)

// Style constants. Views should compose these instead of inlining lipgloss
// literals so theming stays consistent.
var (
	// Base text / chrome --------------------------------------------------
	StyleFG     = lipgloss.NewStyle().Foreground(colorFG)
	StyleDim    = lipgloss.NewStyle().Foreground(colorFGDim)
	StyleMuted  = lipgloss.NewStyle().Foreground(colorFGMuted)
	StyleTitle  = lipgloss.NewStyle().Bold(true).Foreground(colorFG)
	StyleAccent = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	// Panels --------------------------------------------------------------
	StyleStage = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorBorder).
			Padding(0, 1)

	StyleStageFocused = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(colorBorderFocus).
				Padding(0, 1)

	StyleInspector = lipgloss.NewStyle().
			Border(lipgloss.DoubleBorder()).
			BorderForeground(colorAccent).
			Padding(0, 1)

	StyleSidebar = lipgloss.NewStyle().
			Padding(1, 1).
			Foreground(colorFG)

	// Header / footer -----------------------------------------------------
	StyleHeader = lipgloss.NewStyle().
			Foreground(colorFG).
			Padding(0, 1)

	StyleHeaderAccent = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true)

	StyleRule = lipgloss.NewStyle().Foreground(colorAccent)

	StyleStatusLine = lipgloss.NewStyle().
			Foreground(colorFGDim).
			Padding(0, 1)

	StyleHints = lipgloss.NewStyle().
			Foreground(colorFG).
			Padding(0, 1)

	StyleBottomPanel = lipgloss.NewStyle().
				Foreground(colorFG).
				BorderStyle(lipgloss.NormalBorder()).
				BorderTop(true).
				BorderBottom(false).
				BorderLeft(false).
				BorderRight(false).
				BorderForeground(colorAccent).
				Padding(0, 1)

	StyleKeyDim  = lipgloss.NewStyle().Foreground(colorFGDim)
	StyleKeyBold = lipgloss.NewStyle().Foreground(colorFG).Bold(true)
	StyleKeySep  = lipgloss.NewStyle().Foreground(colorFGMuted)

	// Sidebar items -------------------------------------------------------
	StyleSidebarItem = lipgloss.NewStyle().
				Foreground(colorFGDim).
				Padding(0, 1)

	StyleSidebarItemActive = lipgloss.NewStyle().
				Foreground(colorAccent).
				Bold(true).
				Padding(0, 1)

	StyleSidebarBar = lipgloss.NewStyle().
			Foreground(colorAccent)

	StyleSidebarTitle = lipgloss.NewStyle().
				Foreground(colorFGMuted).
				Bold(true).
				Padding(0, 1)

	// Section titles / labels --------------------------------------------
	StyleSectionTitle = lipgloss.NewStyle().
				Foreground(colorSecondary).
				Bold(true)

	StyleLabel = lipgloss.NewStyle().Foreground(colorFGDim)
	StyleValue = lipgloss.NewStyle().Foreground(colorFG)

	// Table ---------------------------------------------------------------
	StyleTableHeader = lipgloss.NewStyle().
				Foreground(colorFGMuted).
				Bold(true).
				Padding(0, 1)

	StyleTableRow = lipgloss.NewStyle().
			Foreground(colorFG).
			Padding(0, 1)

	StyleTableRowAlt = lipgloss.NewStyle().
				Foreground(colorFG).
				Background(colorRowAlt).
				Padding(0, 1)

	StyleTableRowSelected = lipgloss.NewStyle().
				Foreground(colorFG).
				Background(colorRowSel).
				Bold(true).
				Padding(0, 1)

	StyleCursorBar = lipgloss.NewStyle().Foreground(colorAccent).Bold(true)

	// Pills / chips -------------------------------------------------------
	StylePill = lipgloss.NewStyle().
			Foreground(colorFG).
			Background(colorAccentSoft).
			Padding(0, 1)

	StyleChipDim = lipgloss.NewStyle().
			Foreground(colorFGDim).
			Padding(0, 1)

	StyleChipAccent = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true).
			Padding(0, 1)

	// Status pill colors --------------------------------------------------
	StylePillSuccess = lipgloss.NewStyle().Foreground(colorSuccess).Bold(true)
	StylePillError   = lipgloss.NewStyle().Foreground(colorError).Bold(true)
	StylePillWarn    = lipgloss.NewStyle().Foreground(colorWarning).Bold(true)
	StylePillRunning = lipgloss.NewStyle().Foreground(colorSecondary).Bold(true)
	StylePillIdle    = lipgloss.NewStyle().Foreground(colorFGMuted)

	// Banners -------------------------------------------------------------
	StyleErrorBanner = lipgloss.NewStyle().
				Foreground(colorError).
				Bold(true).
				Padding(0, 1)

	StyleToast = lipgloss.NewStyle().
			Foreground(colorWarning).
			Padding(0, 1)

	// Modal / overlay -----------------------------------------------------
	StyleModalCard = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colorAccent).
			Padding(1, 2).
			Foreground(colorFG)

	StyleModalTitle = lipgloss.NewStyle().
			Foreground(colorAccent).
			Bold(true)

	StyleCenter = lipgloss.NewStyle().
			Align(lipgloss.Center).
			Foreground(colorFGDim)

	// Status glyph styles (used by StatusGlyph) --------------------------
	StyleStatusRunning = lipgloss.NewStyle().Foreground(colorSecondary).Bold(true)
	StyleStatusOK      = lipgloss.NewStyle().Foreground(colorSuccess).Bold(true)
	StyleStatusFail    = lipgloss.NewStyle().Foreground(colorError).Bold(true)
	StyleStatusIdle    = lipgloss.NewStyle().Foreground(colorFGMuted)
	StyleChangedDot    = lipgloss.NewStyle().Foreground(colorWarning).Bold(true)

	// Backwards-compat shims (used by older view code) -------------------
	StylePanel        = StyleStage
	StylePanelFocused = StyleStageFocused
	StyleStatusBar    = StyleStatusLine
	StyleKeyHint      = StyleHints
	StyleLoading      = lipgloss.NewStyle().Foreground(colorAccent).Padding(1, 2)
	StyleDimmed       = StyleDim
)
