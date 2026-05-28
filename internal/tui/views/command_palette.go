package views

import tea "github.com/charmbracelet/bubbletea"

// CommandPaletteModel is a stub for Phase 3.
type CommandPaletteModel struct {
	Visible bool
}

func NewCommandPaletteModel() CommandPaletteModel { return CommandPaletteModel{} }

func (m CommandPaletteModel) Init() tea.Cmd                                     { return nil }
func (m CommandPaletteModel) Update(msg tea.Msg) (CommandPaletteModel, tea.Cmd) { return m, nil }
func (m CommandPaletteModel) View() string {
	return ":"
}
