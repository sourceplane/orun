package tui

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
)

// NewProgram constructs a tea.Program configured with the cockpit's
// standard options (alt-screen + mouse cell motion). It is the canonical
// entry point used by cmd/orun/command_tui.go.
func NewProgram(svc services.OrunService) *tea.Program {
	return tea.NewProgram(
		NewModel(svc),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
}

// NewProgramInAgentMode is the `orun agent` (bare) front door: the cockpit
// opened straight onto the Agent surface (orun-agents-live AL3).
func NewProgramInAgentMode(svc services.OrunService) *tea.Program {
	return tea.NewProgram(
		NewModel(svc).StartInAgentMode(),
		tea.WithAltScreen(),
		tea.WithMouseCellMotion(),
	)
}
