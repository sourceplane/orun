package tui

import "github.com/charmbracelet/bubbles/key"

// GlobalKeyMap is the set of bindings active across all modes.
type GlobalKeyMap struct {
	ToggleSidebar   key.Binding
	ToggleInspector key.Binding
	Help            key.Binding
	Palette         key.Binding
	Search          key.Binding
	Quit            key.Binding
	Cancel          key.Binding
	Reload          key.Binding

	// Three top-level surfaces.
	GoBrowse   key.Binding
	GoActivity key.Binding
	GoCatalog  key.Binding

	// Activity-mode optional bottom panel.
	ToggleBottom key.Binding

	// Tab cycles through the top-level surfaces.
	ToggleMode key.Binding

	// Back / Forward across mode history.
	Back    key.Binding
	Forward key.Binding

	// Legacy aliases retained for compatibility — kept defined but
	// repurposed/inert so older callers and tests still compile.
	GoPlan    key.Binding
	GoRun     key.Binding
	GoLogs    key.Binding
	GoHistory key.Binding
	NextPanel key.Binding
	PrevPanel key.Binding
}

// DefaultGlobalKeyMap returns the canonical global bindings.
func DefaultGlobalKeyMap() GlobalKeyMap {
	return GlobalKeyMap{
		ToggleSidebar:   key.NewBinding(key.WithKeys("ctrl+b"), key.WithHelp("⌃b", "toggle sidebar")),
		ToggleInspector: key.NewBinding(key.WithKeys("i"), key.WithHelp("i", "toggle inspector")),
		Help:            key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Palette:         key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "commands")),
		Search:          key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Quit:            key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Cancel:          key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "back")),
		Reload:          key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("⌃r", "reload")),
		GoBrowse:        key.NewBinding(key.WithKeys("1"), key.WithHelp("1", "components")),
		GoActivity:      key.NewBinding(key.WithKeys("2"), key.WithHelp("2", "activity")),
		GoCatalog:       key.NewBinding(key.WithKeys("3"), key.WithHelp("3", "catalog")),
		ToggleBottom:    key.NewBinding(key.WithKeys("b"), key.WithHelp("b", "bottom panel")),
		ToggleMode:      key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "cycle surface")),
		Back:            key.NewBinding(key.WithKeys("backspace", "ctrl+o"), key.WithHelp("⌫", "back")),
		Forward:         key.NewBinding(key.WithKeys("ctrl+i"), key.WithHelp("⌃i", "forward")),
		// Legacy / inert
		GoPlan:    key.NewBinding(key.WithKeys("p"), key.WithHelp("p", "compose")),
		GoRun:     key.NewBinding(key.WithKeys("ctrl+2"), key.WithHelp("⌃2", "runs")),
		GoLogs:    key.NewBinding(key.WithKeys("ctrl+3"), key.WithHelp("⌃3", "logs")),
		GoHistory: key.NewBinding(key.WithKeys("ctrl+4"), key.WithHelp("⌃4", "history")),
		NextPanel: key.NewBinding(key.WithKeys("shift+right"), key.WithHelp("⇧→", "next focus")),
		PrevPanel: key.NewBinding(key.WithKeys("shift+left"), key.WithHelp("⇧←", "prev focus")),
	}
}

// ShortHelp returns the row of bindings shown in the key-hint bar.
func (k GlobalKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.ToggleMode, k.Back, k.ToggleInspector, k.Search, k.Palette, k.Help, k.Quit}
}

// FullHelp returns the table-style help shown by the `?` overlay.
func (k GlobalKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.GoBrowse, k.GoActivity, k.GoCatalog, k.ToggleMode},
		{k.Back, k.Forward, k.Cancel},
		{k.ToggleSidebar, k.ToggleInspector, k.Reload},
		{k.Help, k.Palette, k.Search},
		{k.Quit},
	}
}
