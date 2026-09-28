package main

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
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

// writeTestComponent writes a component manifest at rel under dir.
func writeTestComponent(t *testing.T, dir, rel, name, specExtra string) {
	t.Helper()
	p := filepath.Join(dir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	body := "apiVersion: orun.io/v1alpha1\nkind: Component\nmetadata:\n  name: " + name +
		"\nspec:\n  type: service\n  owner: team/x\n" + specExtra
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// seedNoGitWorkspace writes intent.yaml plus svc-a and svc-b, with no git repo:
// the source id is constant, so only the manifest digest can witness an edit.
func seedNoGitWorkspace(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "intent.yaml"), []byte("apiVersion: orun.io/v1alpha1\nkind: Intent\nmetadata:\n  name: demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeTestComponent(t, dir, "svc-a/component.yaml", "svc-a", "")
	writeTestComponent(t, dir, "svc-b/component.yaml", "svc-b", "")
}

func withIntentImpact(t *testing.T, v string) {
	t.Helper()
	prev := intentImpact
	intentImpact = v
	t.Cleanup(func() { intentImpact = prev })
}

// TestSelectChanged_StaleSnapshot_NewNestedComponent (OR3): a snapshot built
// before a nested component existed must not be used for --changed. The nested
// manifest is a component.yml, which the git dirty probe does not see, so only
// the manifest digest catches it. The selection reflects the working tree and
// the refresh is announced.
func TestSelectChanged_StaleSnapshot_NewNestedComponent(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedGitCatalogWorkspace(t, dir) // svc-a at svc-a/component.yaml
	resetCatalogFlags(t)
	withIntentImpact(t, "watch")
	ctx := context.Background()

	// Snapshot built from manifests A.
	var buf bytes.Buffer
	if _, err := selectChanged(ctx, git.ChangeOptions{Files: []string{"svc-a/handler.go"}}, false, &buf); err != nil {
		t.Fatalf("seed selection: %v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("first build should print no stale notice, got %q", buf.String())
	}

	// Working tree moves to manifests B: a nested component appears.
	writeTestComponent(t, dir, "svc-a/nested/component.yml", "svc-nested", "")

	buf.Reset()
	sel, err := selectChanged(ctx, git.ChangeOptions{Files: []string{"svc-a/nested/main.go"}}, false, &buf)
	if err != nil {
		t.Fatalf("selectChanged: %v", err)
	}
	if !sel["svc-nested"] {
		t.Errorf("the nested component should be selected from the working tree, got %v", sel)
	}
	if sel["svc-a"] {
		t.Errorf("a change inside the nested component belongs to it, not svc-a; got %v", sel)
	}
	notice := buf.String()
	if !strings.Contains(notice, "catalog was stale (1 component added); refreshed from the working tree") {
		t.Errorf("missing stale notice, got %q", notice)
	}

	// Fresh snapshot: no refresh notice, same catalog.
	buf.Reset()
	if _, err := selectChanged(ctx, git.ChangeOptions{Files: []string{"svc-a/nested/main.go"}}, false, &buf); err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if buf.Len() != 0 {
		t.Errorf("a fresh snapshot must not be reported stale, got %q", buf.String())
	}
}

// TestSelectChanged_StaleSnapshot_NewInputEdge (OR3): in a workspace without
// git, a dependsOn input edge added after the snapshot was built must drive the
// selection (a change to svc-b rescopes svc-a).
func TestSelectChanged_StaleSnapshot_NewInputEdge(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedNoGitWorkspace(t, dir)
	resetCatalogFlags(t)
	withIntentImpact(t, "watch")
	ctx := context.Background()

	var buf bytes.Buffer
	sel, err := selectChanged(ctx, git.ChangeOptions{Files: []string{"svc-b/main.go"}}, false, &buf)
	if err != nil {
		t.Fatalf("seed selection: %v", err)
	}
	if sel["svc-a"] || !sel["svc-b"] {
		t.Fatalf("before the edge only svc-b is selected, got %v", sel)
	}

	writeTestComponent(t, dir, "svc-a/component.yaml", "svc-a", "  dependsOn:\n    - component: svc-b\n      input: true\n")

	buf.Reset()
	sel, err = selectChanged(ctx, git.ChangeOptions{Files: []string{"svc-b/main.go"}}, false, &buf)
	if err != nil {
		t.Fatalf("selectChanged: %v", err)
	}
	if !sel["svc-a"] || !sel["svc-b"] {
		t.Errorf("the new input edge should rescope svc-a, got %v", sel)
	}
	if !strings.Contains(buf.String(), "catalog was stale (1 component with changed paths or dependencies)") {
		t.Errorf("missing stale notice, got %q", buf.String())
	}
}

// TestSelectChanged_NoRefresh_WarnsWhenStale: with --no-catalog-refresh the
// snapshot is used as-is, and a stale one is called out with the command that
// rebuilds it; a fresh one is not.
func TestSelectChanged_NoRefresh_WarnsWhenStale(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedNoGitWorkspace(t, dir)
	resetCatalogFlags(t)
	withIntentImpact(t, "watch")
	ctx := context.Background()

	// No snapshot yet: a clear error naming the refresh command.
	var buf bytes.Buffer
	if _, err := selectChanged(ctx, git.ChangeOptions{Files: []string{"svc-a/x.go"}}, true, &buf); err == nil || !strings.Contains(err.Error(), "orun catalog refresh") {
		t.Fatalf("expected an error naming `orun catalog refresh`, got %v", err)
	}

	// Build the snapshot, then use it unrefreshed: fresh, so silent.
	if _, err := selectChanged(ctx, git.ChangeOptions{Files: []string{"svc-a/x.go"}}, false, &buf); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if _, err := selectChanged(ctx, git.ChangeOptions{Files: []string{"svc-a/x.go"}}, true, &buf); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Errorf("fresh snapshot should not warn, got %q", buf.String())
	}

	// A new component the snapshot does not know: stale, warned, not selected.
	writeTestComponent(t, dir, "svc-c/component.yaml", "svc-c", "")
	buf.Reset()
	sel, err := selectChanged(ctx, git.ChangeOptions{Files: []string{"svc-c/x.go"}}, true, &buf)
	if err != nil {
		t.Fatal(err)
	}
	if sel["svc-c"] {
		t.Errorf("--no-catalog-refresh must not rebuild the snapshot, got %v", sel)
	}
	if !strings.Contains(buf.String(), "orun catalog refresh") {
		t.Errorf("expected a stale warning naming `orun catalog refresh`, got %q", buf.String())
	}
}
