package tui

import (
	"context"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/tui/services"
)

// workSurfaceWorkspace pairs the catalog snapshot from modeTestSnapshot with
// a matching workspace (api is active in dev) and run history.
func workSurfaceWorkspace() (*services.WorkspaceSnapshot, []services.RunSummary) {
	ws := &services.WorkspaceSnapshot{
		IntentName:   "ws",
		IntentFile:   "intent.yaml",
		Environments: []string{"dev", "prod"},
		Components: []services.ComponentSummary{
			{Name: "api", Type: "worker", Envs: []string{"dev", "prod"},
				Changed: true, ChangeKind: "changed", LastRunStatus: "success"},
		},
	}
	runs := []services.RunSummary{
		{ExecID: "01HXRUN001", Status: "success", Duration: 9 * time.Second,
			Components: []string{"api"}, StartedAt: time.Now()},
	}
	return ws, runs
}

// Pressing r on a component row in the Catalog drives the same run flow as
// the Component page: plan generation for (component, selected env), then the
// confirm modal once the plan lands.
func TestModel_CatalogRunFlow(t *testing.T) {
	var gotReq services.PlanRequest
	svc := &services.MockOrunService{
		GeneratePlanFn: func(_ context.Context, req services.PlanRequest) (*services.PlanResult, error) {
			gotReq = req
			return &services.PlanResult{
				Plan:     &model.Plan{Jobs: []model.PlanJob{{ID: "api-dev", Component: "api"}}},
				Checksum: "sha256-test", JobCount: 1,
			}, nil
		},
	}
	m := NewModel(svc)
	ws, runs := workSurfaceWorkspace()
	next, _ := m.Update(services.WorkspaceLoadedMsg{Snapshot: ws})
	m = next.(Model)
	next, _ = m.Update(services.RunsListedMsg{Runs: runs})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m = next.(Model)
	next, _ = m.Update(services.CatalogLoadedMsg{Snapshot: modeTestSnapshot()})
	m = next.(Model)
	if m.selectedEnv != "dev" {
		t.Fatalf("selectedEnv = %q, want dev", m.selectedEnv)
	}

	// r on the api row (cursor 0 on All) → ComponentRunRequestedMsg → plan
	// generation for api/dev.
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("r should emit a command")
	}
	next, cmd = m.Update(cmd())
	m = next.(Model)
	if cmd == nil {
		t.Fatal("run request should kick plan generation")
	}
	// The batch contains GeneratePlanCmd + a toast; drain it for the plan msg.
	var planMsg tea.Msg
	for _, msg := range drainCmd(cmd) {
		if pm, ok := msg.(services.PlanGeneratedMsg); ok {
			planMsg = pm
		}
	}
	if planMsg == nil {
		t.Fatal("expected PlanGeneratedMsg from the run request")
	}
	if len(gotReq.Components) != 1 || gotReq.Components[0] != "api" || gotReq.Environment != "dev" {
		t.Fatalf("plan request = %+v, want api/dev", gotReq)
	}
	next, _ = m.Update(planMsg)
	m = next.(Model)
	if !m.showConfirm {
		t.Fatal("confirm modal should open once the component-scoped plan lands")
	}
}

// Pressing enter on an execution row hands off to the Activity drilldown.
func TestModel_CatalogExecutionOpensActivity(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	ws, runs := workSurfaceWorkspace()
	next, _ := m.Update(services.WorkspaceLoadedMsg{Snapshot: ws})
	m = next.(Model)
	next, _ = m.Update(services.RunsListedMsg{Runs: runs})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m = next.(Model)
	next, _ = m.Update(services.CatalogLoadedMsg{Snapshot: modeTestSnapshot()})
	m = next.(Model)

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter}) // drill api
	m = next.(Model)
	rows := m.catalog
	if rows.AtRoot() {
		t.Fatal("expected drilled state")
	}
	// api in modeTestSnapshot has 1 connection (partOf checkout) then the
	// execution; move onto the execution row and enter.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("enter on the execution row should emit a command")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.ActiveMode() != ModeActivity {
		t.Fatalf("ActiveMode = %v, want ModeActivity after opening an execution", m.ActiveMode())
	}
}

// e cycles the selected environment from the Catalog surface.
func TestModel_CatalogEnvCycle(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	ws, _ := workSurfaceWorkspace()
	next, _ := m.Update(services.WorkspaceLoadedMsg{Snapshot: ws})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'3'}})
	m = next.(Model)
	before := m.selectedEnv
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = next.(Model)
	if m.selectedEnv == before {
		t.Fatalf("e should cycle the env in catalog mode (still %q)", m.selectedEnv)
	}
}

// drainCmd executes a tea.Cmd, flattening nested batches into messages.
func drainCmd(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range batch {
			out = append(out, drainCmd(c)...)
		}
		return out
	}
	return []tea.Msg{msg}
}
