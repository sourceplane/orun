package tui

import "github.com/charmbracelet/bubbles/key"

// GlobalKeyMap is the set of bindings active across all modes.
type GlobalKeyMap struct {
	NextPanel key.Binding
	PrevPanel key.Binding
	Help      key.Binding
	Palette   key.Binding
	Search    key.Binding
	Quit      key.Binding
	Cancel    key.Binding
	Reload    key.Binding
}

// DefaultGlobalKeyMap returns the canonical global bindings (see spec §7).
func DefaultGlobalKeyMap() GlobalKeyMap {
	return GlobalKeyMap{
		NextPanel: key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next panel")),
		PrevPanel: key.NewBinding(key.WithKeys("shift+tab"), key.WithHelp("shift+tab", "prev panel")),
		Help:      key.NewBinding(key.WithKeys("?"), key.WithHelp("?", "help")),
		Palette:   key.NewBinding(key.WithKeys(":"), key.WithHelp(":", "command palette")),
		Search:    key.NewBinding(key.WithKeys("/"), key.WithHelp("/", "search")),
		Quit:      key.NewBinding(key.WithKeys("q", "ctrl+c"), key.WithHelp("q", "quit")),
		Cancel:    key.NewBinding(key.WithKeys("esc"), key.WithHelp("esc", "cancel/back")),
		Reload:    key.NewBinding(key.WithKeys("ctrl+r"), key.WithHelp("ctrl+r", "reload")),
	}
}

// ShortHelp returns the row of bindings shown in the key-hint bar.
func (k GlobalKeyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.NextPanel, k.Reload, k.Help, k.Palette, k.Quit}
}

// FullHelp returns the table-style help shown by the `?` overlay.
func (k GlobalKeyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.NextPanel, k.PrevPanel, k.Reload},
		{k.Help, k.Palette, k.Search},
		{k.Cancel, k.Quit},
	}
}
