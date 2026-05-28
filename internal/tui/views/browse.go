package views

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
)

// BrowseModel renders the component browse table. Phase 1 ships a minimal
// list; filter chips, dependency tree, and per-row last-run colour land
// in Phase 2.
type BrowseModel struct {
	Workspace *services.WorkspaceSnapshot
	Cursor    int
	Width     int
	Height    int
}

func NewBrowseModel() BrowseModel { return BrowseModel{} }

func (m BrowseModel) Init() tea.Cmd { return nil }

func (m BrowseModel) Update(msg tea.Msg) (BrowseModel, tea.Cmd) {
	return m, nil
}

func (m BrowseModel) View() string {
	if m.Workspace == nil {
		return "Loading workspace…"
	}
	if len(m.Workspace.Components) == 0 {
		return "No components found in this intent."
	}
	out := fmt.Sprintf("Components (%d) — intent: %s\n\n",
		len(m.Workspace.Components), m.Workspace.IntentName)
	for i, c := range m.Workspace.Components {
		prefix := "  "
		if i == m.Cursor {
			prefix = "› "
		}
		out += fmt.Sprintf("%s%-30s %-20s %s\n", prefix, c.Name, c.Type, c.Domain)
	}
	return out
}
