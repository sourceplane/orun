package views

import (
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/sourceplane/orun/internal/tui/services"
)

func browseWith(comps ...services.ComponentSummary) BrowseModel {
	return BrowseModel{Workspace: &services.WorkspaceSnapshot{Components: comps}}
}

func TestBrowse_ChangedOnlyFilter(t *testing.T) {
	m := browseWith(
		services.ComponentSummary{Name: "api", Changed: true, ChangeKind: "changed"},
		services.ComponentSummary{Name: "web", Changed: true, ChangeKind: "affected"},
		services.ComponentSummary{Name: "shared"},
	)
	if got := len(m.filtered()); got != 3 {
		t.Fatalf("unfiltered = %d rows, want 3", got)
	}

	m = m.ToggleChangedOnly()
	rows := m.filtered()
	if len(rows) != 2 {
		t.Fatalf("changed-only = %d rows, want 2", len(rows))
	}
	for _, r := range rows {
		if !r.Changed {
			t.Errorf("changed-only surfaced an unchanged row: %s", r.Name)
		}
	}

	// Toggling off restores the full list.
	m = m.ToggleChangedOnly()
	if got := len(m.filtered()); got != 3 {
		t.Fatalf("after toggle off = %d rows, want 3", got)
	}
}

func TestBrowse_ChangedOnlyComposesWithSearch(t *testing.T) {
	m := browseWith(
		services.ComponentSummary{Name: "api-edge", Changed: true},
		services.ComponentSummary{Name: "api-core"},
		services.ComponentSummary{Name: "web", Changed: true},
	)
	m = m.SetFilter("api").ToggleChangedOnly()
	rows := m.filtered()
	if len(rows) != 1 || rows[0].Name != "api-edge" {
		t.Fatalf("filter=api + changed-only = %+v, want [api-edge]", rows)
	}
}

func TestBrowse_EnterOpensComponentPage(t *testing.T) {
	m := browseWith(services.ComponentSummary{Name: "api"})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	switch msg := cmd().(type) {
	case ComponentOpenMsg:
		if msg.Name != "api" {
			t.Errorf("ComponentOpenMsg.Name = %q, want api", msg.Name)
		}
	default:
		t.Fatalf("enter emitted %T, want ComponentOpenMsg", msg)
	}
}

func TestBrowse_GComposes(t *testing.T) {
	m := browseWith(services.ComponentSummary{Name: "api"})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'g'}})
	if cmd == nil {
		t.Fatal("g produced no command")
	}
	if _, ok := cmd().(ComponentEnterMsg); !ok {
		t.Fatalf("g emitted %T, want ComponentEnterMsg", cmd())
	}
}

// TestBrowse_ViewClipsToHeight guards against the catalog overflowing past the
// top of the stage: with more components than fit, the rendered View must stay
// within the height budget instead of pushing rows off-screen.
func TestBrowse_ViewClipsToHeight(t *testing.T) {
	comps := make([]services.ComponentSummary, 0, 50)
	for i := 0; i < 50; i++ {
		comps = append(comps, services.ComponentSummary{Name: fmt.Sprintf("comp-%02d", i)})
	}
	m := browseWith(comps...)
	m.Width = 120
	m.Height = 20

	out := m.View()
	if h := lipgloss.Height(out); h > m.Height {
		t.Fatalf("View rendered %d lines, exceeds height budget %d", h, m.Height)
	}
}

// TestBrowse_ViewScrollsToCursor verifies the viewport follows the cursor so the
// selected row stays visible even when it sits far past the first screenful.
func TestBrowse_ViewScrollsToCursor(t *testing.T) {
	comps := make([]services.ComponentSummary, 0, 50)
	for i := 0; i < 50; i++ {
		comps = append(comps, services.ComponentSummary{Name: fmt.Sprintf("comp-%02d", i)})
	}
	m := browseWith(comps...)
	m.Width = 120
	m.Height = 20
	m.Cursor = 49 // last row

	out := m.View()
	if !strings.Contains(out, "comp-49") {
		t.Fatal("View did not scroll to show the cursor row comp-49")
	}
	if lipgloss.Height(out) > m.Height {
		t.Fatalf("View exceeds height budget %d while scrolled", m.Height)
	}
}

func TestBrowse_CTogglesChangedOnly(t *testing.T) {
	m := browseWith(services.ComponentSummary{Name: "api", Changed: true})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	if !m.ChangedOnly {
		t.Fatal("c did not enable changed-only")
	}
}
