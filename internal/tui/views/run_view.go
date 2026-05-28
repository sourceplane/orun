package views

import tea "github.com/charmbracelet/bubbletea"

// RunViewModel is a stub for Phase 3 (live run dashboard).
type RunViewModel struct{}

func NewRunViewModel() RunViewModel { return RunViewModel{} }

func (m RunViewModel) Init() tea.Cmd                              { return nil }
func (m RunViewModel) Update(msg tea.Msg) (RunViewModel, tea.Cmd) { return m, nil }
func (m RunViewModel) View() string {
	return "Run Dashboard (Phase 3 — not yet implemented)"
}
