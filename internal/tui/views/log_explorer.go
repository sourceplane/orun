package views

import tea "github.com/charmbracelet/bubbletea"

// LogExplorerModel is a stub for Phase 3.
type LogExplorerModel struct{}

func NewLogExplorerModel() LogExplorerModel { return LogExplorerModel{} }

func (m LogExplorerModel) Init() tea.Cmd                                  { return nil }
func (m LogExplorerModel) Update(msg tea.Msg) (LogExplorerModel, tea.Cmd) { return m, nil }
func (m LogExplorerModel) View() string {
	return "Log Explorer (Phase 3 — not yet implemented)"
}
