package shell

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui2/design"
	"github.com/sourceplane/orun/internal/tui2/frame"
)

// Settings is the `,` overlay: scope, connection, version — the manage
// drawer, demoted from a surface exactly as the console demoted it
// (design §5). Sign-in/out joins it at TR8.
type Settings struct {
	scope   string
	version string
}

// NewSettings builds the overlay.
func NewSettings(scope, version string) *Settings {
	return &Settings{scope: scope, version: version}
}

// HandleKey implements Overlay: any key closes.
func (s *Settings) HandleKey(tea.KeyMsg) (tea.Cmd, bool) { return nil, true }

// Rev implements Overlay.
func (s *Settings) Rev() string { return "settings" }

// connectionLine renders the connection state from the resolved scope:
// a cloud lane renames the scope at startup, so scope is the truth.
func connectionLine(scope string) string {
	switch scope {
	case "local", "mock", "":
		return design.Text.Render("⏺ local") + design.Dim.Render("  — set ORUN_BACKEND_URL + ORUN_WORKSPACE and run `orun login` for the cloud lanes")
	default:
		return design.ToneSuccess.Style().Render("⏺ cloud") + design.Dim.Render("  — "+scope)
	}
}

// View implements Overlay.
func (s *Settings) View(max frame.Size) string {
	if s.version == "" {
		s.version = "dev"
	}
	lines := []string{
		design.Dim.Render("scope       ") + design.Text.Render(s.scope),
		design.Dim.Render("connection  ") + connectionLine(s.scope),
		design.Dim.Render("version     ") + design.Ref.Render(s.version),
		"",
		design.Dim.Render("keys are listed under ? — all of them run from : too"),
	}
	return design.Box("settings", strings.Join(lines, "\n"), max)
}
