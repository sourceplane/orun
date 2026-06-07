package views

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/tui/services"
)

func TestRunsForComponent_MembershipAndFallback(t *testing.T) {
	runs := []services.RunSummary{
		{ExecID: "r1", Components: []string{"api", "web"}},
		{ExecID: "r2", Components: []string{"web"}},
		{ExecID: "r3", PlanName: "deploy-api-prod"}, // legacy: no Components → substring
		{ExecID: "r4", PlanName: "unrelated"},
	}
	got := runsForComponent("api", runs, 10)
	if len(got) != 2 || got[0].ExecID != "r1" || got[1].ExecID != "r3" {
		t.Fatalf("runsForComponent(api) = %+v, want [r1 r3]", got)
	}

	// Limit is respected.
	if got := runsForComponent("api", runs, 1); len(got) != 1 {
		t.Fatalf("limit=1 returned %d runs", len(got))
	}
	if got := runsForComponent("", runs, 10); got != nil {
		t.Fatalf("empty name must return nil, got %v", got)
	}
}

func TestComponentPage_SetComponentFiltersRuns(t *testing.T) {
	runs := []services.RunSummary{
		{ExecID: "r1", Components: []string{"api"}, StartedAt: time.Now()},
		{ExecID: "r2", Components: []string{"web"}},
	}
	m := NewComponentPageModel().SetComponent(
		&services.ComponentSummary{Name: "api"}, runs)
	if sel := m.SelectedRun(); sel == nil || sel.ExecID != "r1" {
		t.Fatalf("selected run = %+v, want r1", sel)
	}
}

func TestComponentPage_ActiveInSelectedEnv(t *testing.T) {
	m := NewComponentPageModel().SetComponent(
		&services.ComponentSummary{Name: "api", Envs: []string{"dev", "prod"}}, nil)

	m.Env = ""
	if m.ActiveInSelectedEnv() {
		t.Error("no env selected must report inactive")
	}
	m.Env = "prod"
	if !m.ActiveInSelectedEnv() {
		t.Error("prod is a subscribed env — want active")
	}
	m.Env = "staging"
	if m.ActiveInSelectedEnv() {
		t.Error("staging is not subscribed — want inactive")
	}
}

func TestComponentPage_EnterDrillsIntoRun(t *testing.T) {
	runs := []services.RunSummary{{ExecID: "exec-123", Components: []string{"api"}}}
	m := NewComponentPageModel().SetComponent(&services.ComponentSummary{Name: "api"}, runs)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("enter produced no command")
	}
	msg, ok := cmd().(ComponentJobOpenMsg)
	if !ok {
		t.Fatalf("enter emitted %T, want ComponentJobOpenMsg", cmd())
	}
	if msg.ExecID != "exec-123" {
		t.Errorf("ExecID = %q, want exec-123", msg.ExecID)
	}
}

func TestComponentPage_RRequestsRun(t *testing.T) {
	m := NewComponentPageModel().SetComponent(&services.ComponentSummary{Name: "api"}, nil)
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	if cmd == nil {
		t.Fatal("r produced no command")
	}
	msg, ok := cmd().(ComponentRunRequestedMsg)
	if !ok {
		t.Fatalf("r emitted %T, want ComponentRunRequestedMsg", cmd())
	}
	if msg.Name != "api" {
		t.Errorf("Name = %q, want api", msg.Name)
	}
}
