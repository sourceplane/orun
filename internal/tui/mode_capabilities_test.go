package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
)

func TestMode_Capabilities(t *testing.T) {
	searchable := map[Mode]bool{
		ModeCatalog: true, ModeHistory: true, ModeLogExplorer: true,
		ModePlanStudio: false, ModeRunDashboard: false, ModeActivity: false,
	}
	for mode, want := range searchable {
		if got := mode.searchable(); got != want {
			t.Errorf("%v.searchable() = %v, want %v", mode, got, want)
		}
	}
	autoInspector := map[Mode]bool{
		ModeActivity: true, ModePlanStudio: true, ModeCatalog: true,
		ModeHistory: false, ModeLogExplorer: false, ModeRunDashboard: false,
	}
	for mode, want := range autoInspector {
		if got := mode.autoInspector(); got != want {
			t.Errorf("%v.autoInspector() = %v, want %v", mode, got, want)
		}
	}
}

// Back (⌫/⌃o) pops one drill level — same as esc — instead of silently
// consuming the key inside a drilled mode.
func TestModel_BackKeyPopsDrillLevel(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	// Visit Activity then return to Catalog so the mode-back stack has an
	// entry under the catalog.
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'2'}})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'1'}})
	m = next.(Model)
	next, _ = m.Update(services.CatalogLoadedMsg{Snapshot: modeTestSnapshot()})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // drill into api
	m = next.(Model)
	if m.catalog.AtRoot() {
		t.Fatal("expected drilled state after enter")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(Model)
	if !m.catalog.AtRoot() {
		t.Fatal("backspace should pop the drill level")
	}
	if m.ActiveMode() != ModeCatalog {
		t.Fatalf("backspace on a drilled entity must stay in catalog, got %v", m.ActiveMode())
	}
	// At the root, backspace falls through to mode-back history.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	m = next.(Model)
	if m.ActiveMode() != ModeActivity {
		t.Fatalf("backspace at root should pop the mode stack, got %v", m.ActiveMode())
	}
}
