package tui

import (
	"context"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/views"
)

func modeTestSnapshot() *services.CatalogSnapshot {
	return &services.CatalogSnapshot{
		HumanKey:     "cat-test",
		CountsByKind: map[string]int{"Component": 1, "System": 1},
		Entities: []services.EntitySummary{
			{Kind: "Component", EntityKey: "ns/repo/api", Name: "api", Owner: "group:edge"},
			{Kind: "System", EntityKey: "ns/repo/checkout", Name: "checkout", MemberCount: 1, Members: []string{"ns/repo/api"}},
		},
		Relations: []services.RelationSummary{
			{From: "ns/repo/api", FromKind: "Component", Type: "partOf", To: "ns/repo/checkout", ToKind: "System"},
		},
	}
}

// Key 3 switches to the Catalog surface and kicks a catalog load.
func TestModel_CatalogModeKeyAndLoad(t *testing.T) {
	loaded := false
	svc := &services.MockOrunService{
		LoadCatalogFn: func(context.Context) (*services.CatalogSnapshot, error) {
			loaded = true
			return modeTestSnapshot(), nil
		},
	}
	m := NewModel(svc)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m = next.(Model)
	if m.ActiveMode() != ModeCatalog {
		t.Fatalf("ActiveMode = %v, want ModeCatalog", m.ActiveMode())
	}
	if cmd == nil {
		t.Fatal("expected a load command when entering the catalog")
	}
	msg := cmd()
	if !loaded {
		t.Fatal("LoadCatalog was not invoked")
	}
	next, _ = m.Update(msg)
	m = next.(Model)
	m.width, m.height = 120, 40
	out := m.View()
	if !strings.Contains(out, "Catalog") || !strings.Contains(out, "api") {
		t.Errorf("catalog stage missing content")
	}
}

// loadCatalogCmd surfaces snapshot + error as CatalogLoadedMsg, and a failed
// load keeps the previous snapshot.
func TestModel_CatalogLoadBestEffort(t *testing.T) {
	svc := &services.MockOrunService{
		LoadCatalogFn: func(context.Context) (*services.CatalogSnapshot, error) {
			return modeTestSnapshot(), nil
		},
	}
	m := NewModel(svc)
	next, _ := m.Update(services.CatalogLoadedMsg{Snapshot: modeTestSnapshot()})
	m = next.(Model)
	// A failed background reload must not blank the surface.
	next, _ = m.Update(services.CatalogLoadedMsg{Err: context.DeadlineExceeded})
	m = next.(Model)
	if m.catalog.Snapshot == nil {
		t.Fatal("failed reload blanked the catalog snapshot")
	}
}

// Esc inside a drilled catalog entity pops the drill level, not the mode.
func TestModel_CatalogEscPopsDrillFirst(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m = next.(Model)
	next, _ = m.Update(services.CatalogLoadedMsg{Snapshot: modeTestSnapshot()})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // drill into api
	m = next.(Model)
	if m.catalog.AtRoot() {
		t.Fatal("expected drilled state after enter")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.ActiveMode() != ModeCatalog {
		t.Fatalf("esc on a drilled entity must stay in catalog, got %v", m.ActiveMode())
	}
	if !m.catalog.AtRoot() {
		t.Fatal("esc should pop the drill level")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.ActiveMode() != ModeBrowse {
		t.Fatalf("esc at catalog root should pop the mode stack, got %v", m.ActiveMode())
	}
}

// The sidebar highlights the catalog item while the mode is active.
func TestModel_CatalogSidebarKey(t *testing.T) {
	if ModeCatalog.sidebarKey() != "catalog" {
		t.Fatalf("sidebarKey = %q, want catalog", ModeCatalog.sidebarKey())
	}
	if ModeCatalog.String() != "catalog" {
		t.Fatalf("String = %q, want catalog", ModeCatalog.String())
	}
}

// The palette command routes to the catalog surface.
func TestModel_PaletteGotoCatalog(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	next, cmd := m.Update(views.PaletteCommandSelectedMsg{
		Command: views.CommandPaletteCommand{ID: "goto.catalog"},
	})
	m = next.(Model)
	if m.ActiveMode() != ModeCatalog {
		t.Fatalf("ActiveMode = %v, want ModeCatalog", m.ActiveMode())
	}
	if cmd == nil {
		t.Fatal("goto.catalog should kick a catalog load")
	}
}

// Slash search filters the catalog rows.
func TestModel_CatalogSearchFilters(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m = next.(Model)
	next, _ = m.Update(services.CatalogLoadedMsg{Snapshot: modeTestSnapshot()})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	m = next.(Model)
	if !m.searchActive {
		t.Fatal("/ should activate search in catalog mode")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = next.(Model)
	if m.catalog.Filter != "c" {
		t.Fatalf("catalog filter = %q, want c", m.catalog.Filter)
	}
}
