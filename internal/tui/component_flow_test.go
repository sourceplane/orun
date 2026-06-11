package tui

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/views"
)

// loadedModel builds a model with a workspace snapshot and a settled selected
// env, independent of any on-disk prefs.
func loadedModel(t *testing.T, snap *services.WorkspaceSnapshot) Model {
	t.Helper()
	m := NewModel(&services.MockOrunService{})
	m.prefs.SelectedEnv = ""
	m.selectedEnv = ""
	next, _ := m.Update(services.WorkspaceLoadedMsg{Snapshot: snap})
	return next.(Model)
}

func TestModel_SelectedEnvDefaultsToFirstSorted(t *testing.T) {
	m := loadedModel(t, &services.WorkspaceSnapshot{
		Environments: []string{"prod", "dev"},
	})
	if m.selectedEnv != "dev" {
		t.Fatalf("selectedEnv = %q, want dev (first sorted)", m.selectedEnv)
	}
}

func TestModel_CycleEnv(t *testing.T) {
	m := loadedModel(t, &services.WorkspaceSnapshot{
		Environments: []string{"dev", "prod"},
	})
	if m.selectedEnv != "dev" {
		t.Fatalf("precondition: selectedEnv = %q, want dev", m.selectedEnv)
	}
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = next.(Model)
	if m.selectedEnv != "prod" {
		t.Fatalf("after e: selectedEnv = %q, want prod", m.selectedEnv)
	}
	// Wraps back around.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}})
	m = next.(Model)
	if m.selectedEnv != "dev" {
		t.Fatalf("after second e: selectedEnv = %q, want dev (wrap)", m.selectedEnv)
	}
}

func TestModel_StartsOnCatalogSurface(t *testing.T) {
	m := loadedModel(t, &services.WorkspaceSnapshot{
		Environments: []string{"dev"},
		Components:   []services.ComponentSummary{{Name: "api", Type: "worker"}},
	})
	if m.ActiveMode() != ModeCatalog {
		t.Fatalf("ActiveMode = %v, want ModeCatalog (the home surface)", m.ActiveMode())
	}
}

func TestModel_ComponentRun_InactiveEnvIsRejected(t *testing.T) {
	m := loadedModel(t, &services.WorkspaceSnapshot{
		Environments: []string{"dev", "prod"},
		Components: []services.ComponentSummary{
			{Name: "api", Envs: []string{"prod"}}, // not active in dev
		},
	})
	if m.selectedEnv != "dev" {
		t.Fatalf("precondition selectedEnv = %q, want dev", m.selectedEnv)
	}
	next, _ := m.Update(views.ComponentRunRequestedMsg{Name: "api"})
	m = next.(Model)
	if m.runAfterGenerate {
		t.Fatal("run for an env the component is inactive in must not arm generation")
	}
	if m.toast == "" {
		t.Fatal("expected an explanatory toast for the rejected run")
	}
}

func TestModel_ComponentRun_ActiveEnvArmsGenerateThenConfirms(t *testing.T) {
	m := loadedModel(t, &services.WorkspaceSnapshot{
		Environments: []string{"prod"},
		Components: []services.ComponentSummary{
			{Name: "api", Envs: []string{"prod"}},
		},
	})
	next, cmd := m.Update(views.ComponentRunRequestedMsg{Name: "api"})
	m = next.(Model)
	if !m.runAfterGenerate {
		t.Fatal("an active-env run request must arm runAfterGenerate")
	}
	if cmd == nil {
		t.Fatal("expected a plan-generation command")
	}

	// Simulate generation completing: the model must pop the confirm modal with
	// the plan scoped to the selected env.
	plan := &model.Plan{}
	next, _ = m.Update(services.PlanGeneratedMsg{Result: &services.PlanResult{Plan: plan, JobCount: 1}})
	m = next.(Model)
	if m.runAfterGenerate {
		t.Fatal("runAfterGenerate must clear once generation completes")
	}
	if !m.showConfirm {
		t.Fatal("expected the run-confirm modal to be shown")
	}
	if m.pendingRun == nil || m.pendingRun.Plan != plan || m.pendingRun.Env != "prod" {
		t.Fatalf("pendingRun = %+v, want plan scoped to prod", m.pendingRun)
	}
}
