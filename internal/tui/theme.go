// Package tui implements the Orun Cockpit terminal UI as a component-native
// control plane over Orun internal packages. The TUI never shells out to
// `orun`; all data and actions flow through services.OrunService.
package tui

import "github.com/sourceplane/orun/internal/tui/theme"

// Style re-exports for backwards compatibility with older tui-internal
// references. New code should import the theme package directly.
var (
	StylePanel        = theme.StylePanel
	StylePanelFocused = theme.StylePanelFocused
	StyleStatusBar    = theme.StyleStatusBar
	StyleKeyHint      = theme.StyleKeyHint
	StyleErrorBanner  = theme.StyleErrorBanner
	StyleLoading      = theme.StyleLoading
	StyleDimmed       = theme.StyleDimmed
	StyleTitle        = theme.StyleTitle
	StyleAccent       = theme.StyleAccent
)
