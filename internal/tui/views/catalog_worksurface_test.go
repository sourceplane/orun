package views

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
)

// workSurfaceModel returns a catalog model with the Component work-surface
// context installed: api is directly changed with two executions; web is
// affected through the dependency edge.
func workSurfaceModel() CatalogModel {
	m := NewCatalogModel().SetSnapshot(testCatalogSnapshot())
	comps := []services.ComponentSummary{
		{Name: "api", Changed: true, ChangeKind: "changed", LastRunStatus: "success",
			Envs: []string{"dev", "prod"}},
		{Name: "web", Changed: true, ChangeKind: "affected"},
	}
	runs := map[string][]services.RunSummary{
		"api": {
			{ExecID: "01HXAPI001", Status: "success", Duration: 12 * time.Second, Trigger: "manual"},
			{ExecID: "01HXAPI000", Status: "failed", Duration: 3 * time.Second, Trigger: "ci", DryRun: true},
		},
	}
	return m.SetComponentContext(comps, runs)
}

func TestCatalog_ChangedOnlyFilter(t *testing.T) {
	m := workSurfaceModel()
	m, _ = m.Update(keyRunes("[")) // Component (default) → All
	m, _ = m.Update(keyRunes("c"))
	rows := m.filtered()
	// Only the two changed/affected components survive — the System, Group,
	// and Composition entities are hidden.
	if len(rows) != 2 {
		t.Fatalf("changed-only rows = %d, want 2: %+v", len(rows), rows)
	}
	for _, r := range rows {
		if r.Kind != "Component" {
			t.Errorf("changed-only must keep only components, got %s %s", r.Kind, r.Name)
		}
	}
	m, _ = m.Update(keyRunes("c"))
	if got := len(m.filtered()); got != 5 {
		t.Fatalf("toggle off rows = %d, want 5", got)
	}
}

func TestCatalog_ChangeAndLastRunColumns(t *testing.T) {
	m := workSurfaceModel().SetSize(120, 30) // Component tab is the default
	out := m.View()
	for _, want := range []string{"2 changed", "success"} {
		if !strings.Contains(out, want) {
			t.Errorf("component tab missing %q", want)
		}
	}
}

func TestCatalog_ComponentActions(t *testing.T) {
	m := workSurfaceModel()
	// Cursor 0 on All = api (a Component) — r/g emit the work messages.
	cases := []struct {
		key  string
		want any
	}{
		{"r", ComponentRunRequestedMsg{Name: "api"}},
		{"g", ComponentEnterMsg{Name: "api"}},
	}
	for _, tc := range cases {
		_, cmd := m.Update(keyRunes(tc.key))
		if cmd == nil {
			t.Fatalf("%s on a component row should emit a command", tc.key)
		}
		if got := cmd(); got != tc.want {
			t.Errorf("%s emitted %#v, want %#v", tc.key, got, tc.want)
		}
	}
	// On a non-component row the action keys are inert.
	m2 := m
	m2.kindIdx = 2 // System tab (All, Component, System, …)
	if m2.ActiveKind() != "System" {
		t.Fatalf("expected System tab, got %s", m2.ActiveKind())
	}
	if _, cmd := m2.Update(keyRunes("r")); cmd != nil {
		t.Error("r on a System row must not emit a run command")
	}
	// The same actions work from the drilled detail page.
	m3, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // drill api
	if _, cmd := m3.Update(keyRunes("r")); cmd == nil {
		t.Error("r on a drilled component should emit a run command")
	} else if got := cmd(); got != (ComponentRunRequestedMsg{Name: "api"}) {
		t.Errorf("drilled r emitted %#v", got)
	}
}

func TestCatalog_DetailExecutionsAndDrillThrough(t *testing.T) {
	m := workSurfaceModel()
	m = m.SetSize(120, 40)
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // drill api

	out := m.View()
	for _, want := range []string{"EXECUTIONS (2)", "01HXAPI001", "success", "manual", "dry-run", "change", "last run"} {
		if !strings.Contains(out, want) {
			t.Errorf("detail view missing %q", want)
		}
	}

	// api has 3 connections (dependsOn, partOf, ownedBy) then 2 executions;
	// move past the connections onto the first execution and enter.
	rows := m.detailRows()
	if len(rows) != 5 {
		t.Fatalf("detail rows = %d, want 5 (3 links + 2 runs)", len(rows))
	}
	for i := 0; i < 3; i++ {
		m, _ = m.Update(keyRunes("j"))
	}
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter on an execution row should emit a command")
	}
	if got := cmd(); got != (ComponentJobOpenMsg{ExecID: "01HXAPI001"}) {
		t.Errorf("enter emitted %#v, want ComponentJobOpenMsg{01HXAPI001}", got)
	}
}

func TestCatalog_InspectorDescWithWorkSurface(t *testing.T) {
	m := workSurfaceModel()
	d := m.InspectorDesc()
	if d == nil || d.Name != "api" {
		t.Fatalf("desc = %+v, want api", d)
	}
	var hasChange, hasLast, hasRuns bool
	for _, f := range d.Fields {
		switch f.Label {
		case "change":
			hasChange = f.Value == "changed"
		case "last run":
			hasLast = f.Value == "success"
		case "recent runs":
			hasRuns = strings.Contains(f.Value, "01HXAPI0")
		}
	}
	if !hasChange || !hasLast || !hasRuns {
		t.Fatalf("inspector missing work-surface fields: %+v", d.Fields)
	}
}

func TestCatalog_ContextRefreshClampsCursors(t *testing.T) {
	m := workSurfaceModel()
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // drill api (5 rows)
	for i := 0; i < 4; i++ {
		m, _ = m.Update(keyRunes("j"))
	}
	// A refresh that drops the execution history shrinks the row set under
	// the cursor; the cursor must clamp back onto a real row.
	m = m.SetComponentContext(nil, nil)
	if rows := m.detailRows(); m.detailCursor >= len(rows) {
		t.Fatalf("detailCursor %d not clamped to %d rows", m.detailCursor, len(rows))
	}
}
