package views

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/theme"
)

// SidebarItem is one entry in the cockpit's left rail.
type SidebarItem struct {
	Icon  string
	Label string
	Mode  string // mode name this item activates
}

// NavigatorModel renders the collapsible icon+label sidebar on the left.
type NavigatorModel struct {
	Items     []SidebarItem
	Cursor    int
	Focused   bool
	Collapsed bool
	Width     int
	Height    int
}

// NavItem is the historical name kept for compatibility.
type NavItem = SidebarItem

// NewNavigatorModel returns a navigator pre-populated with the cockpit's
// canonical sidebar items.
func NewNavigatorModel() NavigatorModel {
	return NavigatorModel{
		Items: []SidebarItem{
			{Icon: "◆", Label: "Components", Mode: "browse"},
			{Icon: "▶", Label: "Activity", Mode: "activity"},
			{Icon: "⬢", Label: "Catalog", Mode: "catalog"},
		},
	}
}

// SetActiveMode highlights the row matching mode (no-op if not found).
func (m NavigatorModel) SetActiveMode(mode string) NavigatorModel {
	for i, it := range m.Items {
		if it.Mode == mode {
			m.Cursor = i
			return m
		}
	}
	return m
}

func (m NavigatorModel) Init() tea.Cmd                              { return nil }
func (m NavigatorModel) Update(_ tea.Msg) (NavigatorModel, tea.Cmd) { return m, nil }

// View renders the sidebar; honors the Collapsed flag.
func (m NavigatorModel) View() string {
	var b strings.Builder
	if !m.Collapsed {
		b.WriteString(theme.StyleSidebarTitle.Render("ORUN"))
		b.WriteString("\n\n")
	} else {
		b.WriteString("\n")
	}
	for i, it := range m.Items {
		active := i == m.Cursor
		bar := " "
		if active {
			bar = theme.StyleSidebarBar.Render("▌")
		}
		var label string
		if m.Collapsed {
			label = it.Icon
		} else {
			label = it.Icon + "  " + it.Label
		}
		row := label
		if active {
			row = theme.StyleSidebarItemActive.Render(label)
		} else {
			row = theme.StyleSidebarItem.Render(label)
		}
		b.WriteString(bar + row + "\n")
	}
	return b.String()
}
