// Package theme holds the cockpit's shared lipgloss palette and reusable
// styles. Both the root tui package and the per-view models in
// internal/tui/views import from here so the look stays consistent.
package theme

import (
	"github.com/charmbracelet/lipgloss"

	"github.com/sourceplane/orun/internal/cockpit/style"
)

// Palette — sourced from the shared cockpit/style.DefaultPalette so the
// TUI and any future themed CLI never drift. To reskin Orun, edit
// style.DefaultPalette in one place.
var (
	pal = style.DefaultPalette

	adaptive = func(light, dark string) lipgloss.AdaptiveColor {
		return lipgloss.AdaptiveColor{Light: light, Dark: dark}
	}

	colorFG      = adaptive(pal.FG, pal.FGDark)
	colorFGDim   = adaptive(pal.Dim, pal.DimDark)
	colorFGMuted = adaptive(pal.Muted, pal.MutedDark)

	colorAccent     = adaptive(pal.Brand, pal.BrandDark)
	colorAccentSoft = adaptive(pal.BrandSoft, pal.BrandSoftDark)
	colorSecondary  = adaptive(pal.Secondary, pal.SecondaryDark)

	colorSuccess = adaptive(pal.Success, pal.SuccessDark)
	colorWarning = adaptive(pal.Warning, pal.WarningDark)
	colorError   = adaptive(pal.Error, pal.ErrorDark)

	colorBorder      = adaptive(pal.Border, pal.BorderDark)
	colorBorderFocus = adaptive(pal.Brand, pal.BrandDark)

	colorRowAlt = adaptive(pal.RowAlt, pal.RowAltDark)
	colorRowSel = adaptive(pal.RowSel, pal.RowSelDark)
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
