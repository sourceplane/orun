// Package views holds the per-mode Bubble Tea models that render inside
// the cockpit's Main panel, plus the Navigator, Inspector, and overlay
// models.
//
// Phase 1 ships compiling stubs for every view named in the spec; the
// fully-implemented behavior lands in later phases (Browse + Plan Studio
// from Phase 2, Run Dashboard / Log Explorer / History / command palette
// from Phase 3).
package views

import tea "github.com/charmbracelet/bubbletea"

// NavItem is one entry in the resource-type tree.
type NavItem struct {
	Label string
	Mode  string // mode name this item activates
}

// NavigatorModel renders the fixed-width resource-type navigator on the
// left.
type NavigatorModel struct {
	Items   []NavItem
	Cursor  int
	Focused bool
	Width   int
	Height  int
}

// NewNavigatorModel returns a navigator pre-populated with the default
// resource-type rows from the spec.
func NewNavigatorModel() NavigatorModel {
	return NavigatorModel{
		Items: []NavItem{
			{Label: "Components", Mode: "browse"},
			{Label: "Environments", Mode: "browse"},
			{Label: "Plans", Mode: "plan-studio"},
			{Label: "Runs", Mode: "run-dashboard"},
			{Label: "Jobs", Mode: "run-dashboard"},
			{Label: "Logs", Mode: "log-explorer"},
			{Label: "History", Mode: "history"},
		},
	}
}

func (m NavigatorModel) Init() tea.Cmd { return nil }

func (m NavigatorModel) Update(msg tea.Msg) (NavigatorModel, tea.Cmd) {
	return m, nil
}

func (m NavigatorModel) View() string {
	out := ""
	for i, item := range m.Items {
		prefix := "  "
		if i == m.Cursor {
			prefix = "› "
		}
		out += prefix + item.Label + "\n"
	}
	return out
}
