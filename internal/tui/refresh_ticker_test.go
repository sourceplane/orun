package tui

import (
	"testing"

	"github.com/sourceplane/orun/internal/tui/services"
)

// TestCatalogRefreshTick_RearmsAndReloads proves the live-view ticker keeps
// itself running: handling a tick returns a non-nil command (the silent reload
// batched with the re-armed tick), so the cockpit never stops polling.
func TestCatalogRefreshTick_RearmsAndReloads(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	next, cmd := m.Update(catalogRefreshTickMsg{})
	if _, ok := next.(Model); !ok {
		t.Fatalf("expected Model, got %T", next)
	}
	if cmd == nil {
		t.Fatal("a catalog tick must return a command (reload + re-armed tick)")
	}
}

// TestWorkspaceRefreshed_SwapsSnapshot proves a successful background refresh
// swaps the in-memory snapshot into the model and the browse view.
func TestWorkspaceRefreshed_SwapsSnapshot(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	fresh := &services.WorkspaceSnapshot{
		IntentName: "demo",
		Components: []services.ComponentSummary{{Name: "svc-a", Type: "service"}},
	}

	next, _ := m.Update(workspaceRefreshedMsg{Snapshot: fresh})
	m = next.(Model)

	if m.Workspace() != fresh {
		t.Fatal("expected the refreshed snapshot to be swapped in")
	}
}

// TestWorkspaceRefreshed_BestEffortOnError proves a failed background refresh
// keeps the current snapshot instead of replacing good data with an error.
func TestWorkspaceRefreshed_BestEffortOnError(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	good := &services.WorkspaceSnapshot{IntentName: "demo"}
	m.workspace = good
	m.loading = false

	next, cmd := m.Update(workspaceRefreshedMsg{Err: errLoad})
	m = next.(Model)

	if m.Workspace() != good {
		t.Fatal("a failed background refresh must keep the current snapshot")
	}
	if m.loading {
		t.Fatal("a background refresh must not toggle the loading spinner")
	}
	if cmd != nil {
		t.Fatal("a best-effort failed refresh should issue no follow-up command")
	}
}

// TestWorkspaceRefreshed_NilSnapshotKept proves a nil snapshot (no error) is
// also treated as best-effort and does not clear the current view.
func TestWorkspaceRefreshed_NilSnapshotKept(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	good := &services.WorkspaceSnapshot{IntentName: "demo"}
	m.workspace = good

	next, _ := m.Update(workspaceRefreshedMsg{Snapshot: nil})
	m = next.(Model)

	if m.Workspace() != good {
		t.Fatal("a nil background snapshot must not clear the current snapshot")
	}
}

var errLoad = errTest("load failed")

type errTest string

func (e errTest) Error() string { return string(e) }
