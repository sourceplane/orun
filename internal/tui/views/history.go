package views

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
)

// HistoryModel is a minimal Phase 1 history view: lists recent runs as
// returned by services.OrunService.ListRuns. The bubbles/list-backed
// filterable variant arrives in Phase 3.
type HistoryModel struct {
	Runs []services.RunSummary
}

func NewHistoryModel() HistoryModel { return HistoryModel{} }

func (m HistoryModel) Init() tea.Cmd                              { return nil }
func (m HistoryModel) Update(msg tea.Msg) (HistoryModel, tea.Cmd) { return m, nil }

func (m HistoryModel) View() string {
	if len(m.Runs) == 0 {
		return "No runs recorded yet."
	}
	out := fmt.Sprintf("Recent runs (%d)\n\n", len(m.Runs))
	for _, r := range m.Runs {
		out += fmt.Sprintf("  %s  %-10s  %s\n", r.ExecID, r.Status, r.PlanName)
	}
	return out
}
