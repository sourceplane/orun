package main

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/git"
)

// TestEngineChangedSelection_E2E proves the live --changed engine path works
// end-to-end over a real workspace: it refreshes the full object-model catalog,
// runs the engine, and returns the selected component names.
func TestEngineChangedSelection_E2E(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedGitCatalogWorkspace(t, dir) // one component: svc-a at svc-a/component.yaml

	resetCatalogFlags(t)
	prev := intentImpact
	intentImpact = "watch"
	t.Cleanup(func() { intentImpact = prev })

	ctx := context.Background()

	// A change inside svc-a's dir selects svc-a.
	sel, err := engineChangedSelection(ctx, git.ChangeOptions{Files: []string{"svc-a/handler.go"}})
	if err != nil {
		t.Fatalf("engineChangedSelection: %v", err)
	}
	if !sel["svc-a"] {
		t.Errorf("svc-a should be selected, got %v", sel)
	}

	// A change outside any component selects nothing.
	sel2, err := engineChangedSelection(ctx, git.ChangeOptions{Files: []string{"unrelated/notes.txt"}})
	if err != nil {
		t.Fatalf("engineChangedSelection (unrelated): %v", err)
	}
	if len(sel2) != 0 {
		t.Errorf("unrelated change should select nothing, got %v", sel2)
	}
}
