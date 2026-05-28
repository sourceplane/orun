package views

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
)

// InspectorModel renders the structured detail pane on the right.
type InspectorModel struct {
	Desc    *services.ResourceDescription
	Focused bool
	Width   int
	Height  int
}

func NewInspectorModel() InspectorModel { return InspectorModel{} }

func (m InspectorModel) Init() tea.Cmd { return nil }

func (m InspectorModel) Update(msg tea.Msg) (InspectorModel, tea.Cmd) {
	return m, nil
}

func (m InspectorModel) View() string {
	if m.Desc == nil {
		return "(select a resource)"
	}
	out := m.Desc.Kind + ": " + m.Desc.Name + "\n"
	if m.Desc.Summary != "" {
		out += m.Desc.Summary + "\n"
	}
	for _, f := range m.Desc.Fields {
		out += f.Label + ": " + f.Value + "\n"
	}
	return out
}
