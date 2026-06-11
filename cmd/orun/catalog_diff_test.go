package main

// catalog_diff_test.go is the C8 CLI E2E suite for `orun catalog diff`. It
// seeds a real git workspace, refreshes on main (the base), then mutates the
// components on a feature branch and refreshes again (the head — which moves
// the `current` ref while `main` stays pinned to the original snapshot). The
// diff is then driven through runCatalogDiff in both text and --json modes.
//
// Coverage:
//   - text output has the four §6 sections in order and reports the change.
//   - JSON envelope kind + the changed/added/removed payload.
//   - exit 0 even when differences exist.
//   - repeated --json runs are byte-identical (determinism contract).
//   - a single-component filter narrows the report.
//   - an unknown component exits 6.
//   - an invalid base/head selector exits 1.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// seedDiffWorkspace plants a git repo on main with svc-a, refreshes it (base),
// then on a feature branch changes svc-a's owner and adds svc-b, commits, and
// refreshes again (head). After this the `main` ref points at the original
// catalog and `current` at the mutated one.
func seedDiffWorkspace(t *testing.T) {
	t.Helper()
	dir := withTempIntentRoot(t)
	seedGitCatalogWorkspace(t, dir)
	resetCatalogFlags(t)

	git := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	// Base refresh on main.
	catalogJSONFlag = true
	_ = captureStdout(t, func() error { return runCatalogRefresh(nil) })
	catalogJSONFlag = false

	// Mutate on a feature branch: change svc-a owner, add svc-b.
	git("checkout", "-q", "-b", "feature-x")
	if err := os.WriteFile(filepath.Join(dir, "svc-a", "component.yaml"), []byte(""+
		"apiVersion: orun.io/v1alpha1\n"+
		"kind: Component\n"+
		"metadata:\n"+
		"  name: svc-a\n"+
		"spec:\n"+
		"  type: service\n"+
		"  owner: team/y\n"+ // changed from team/x
		"  system: payments\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	svcB := filepath.Join(dir, "svc-b")
	if err := os.MkdirAll(svcB, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(svcB, "component.yaml"), []byte(""+
		"apiVersion: orun.io/v1alpha1\n"+
		"kind: Component\n"+
		"metadata:\n"+
		"  name: svc-b\n"+
		"spec:\n"+
		"  type: service\n"+
		"  owner: team/x\n"+
		"  system: payments\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	git("add", "-A")
	git("commit", "-qm", "mutate")

	// Head refresh on the feature branch — moves `current`, leaves `main`.
	catalogJSONFlag = true
	_ = captureStdout(t, func() error { return runCatalogRefresh(nil) })
	catalogJSONFlag = false
}

func decodeDiffEnvelope(t *testing.T, out string) catalogDiffData {
	t.Helper()
	var env catalogEnvelope
	var data catalogDiffData
	env.Data = &data
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("diff envelope: %v\n%s", err, out)
	}
	if env.Kind != kindCatalogDiffResult {
		t.Errorf("kind = %q, want %q", env.Kind, kindCatalogDiffResult)
	}
	return data
}

func TestCatalogDiff_E2E_Text(t *testing.T) {
	seedDiffWorkspace(t)
	catalogDiffBaseFlag, catalogDiffHeadFlag = "main", "current"

	out := captureStdout(t, func() error { return runCatalogDiff(nil, "") })

	// The four §6 sections appear in order.
	sections := []string{"Changed components", "Added components", "Removed components", "Graph changes"}
	last := -1
	for _, s := range sections {
		idx := strings.Index(out, s)
		if idx < 0 {
			t.Errorf("missing section %q in:\n%s", s, out)
			continue
		}
		if idx < last {
			t.Errorf("section %q out of order in:\n%s", s, out)
		}
		last = idx
	}
	if !strings.Contains(out, "svc-a") || !strings.Contains(out, "metadata.owner") {
		t.Errorf("expected svc-a owner change, got:\n%s", out)
	}
	if !strings.Contains(out, "svc-b") {
		t.Errorf("expected svc-b added, got:\n%s", out)
	}
}

func TestCatalogDiff_E2E_JSON_Exit0(t *testing.T) {
	seedDiffWorkspace(t)
	catalogDiffBaseFlag, catalogDiffHeadFlag = "main", "current"
	catalogJSONFlag = true

	out := captureStdout(t, func() error {
		err := runCatalogDiff(nil, "")
		if err != nil {
			t.Fatalf("diff must exit 0 even with differences, got %v", err)
		}
		return nil
	})

	data := decodeDiffEnvelope(t, out)
	if data.Base.CatalogSnapshotKey == "" || data.Head.CatalogSnapshotKey == "" {
		t.Errorf("endpoints missing keys: %+v", data)
	}
	if data.Base.CatalogSnapshotKey == data.Head.CatalogSnapshotKey {
		t.Errorf("base and head resolved to the same catalog: %s", data.Base.CatalogSnapshotKey)
	}
	// svc-a changed (owner), svc-b added, nothing removed.
	if len(data.Changed) != 1 || data.Changed[0].Name != "svc-a" {
		t.Errorf("Changed = %+v, want [svc-a]", data.Changed)
	}
	foundOwner := false
	for _, f := range data.Changed[0].Fields {
		if f.Path == "metadata.owner" && f.Base == "group:team/x" && f.Head == "group:team/y" {
			foundOwner = true
		}
	}
	if !foundOwner {
		t.Errorf("expected metadata.owner group:team/x→group:team/y, got %+v", data.Changed[0].Fields)
	}
	if len(data.Added) != 1 || data.Added[0].Name != "svc-b" {
		t.Errorf("Added = %+v, want [svc-b]", data.Added)
	}
	if len(data.Removed) != 0 {
		t.Errorf("Removed = %+v, want none", data.Removed)
	}
	// Non-nil arrays for stable JSON shape.
	if data.GraphChanges == nil {
		t.Error("graphChanges must be a non-nil array")
	}
}

// TestCatalogDiff_E2E_Deterministic proves repeated --json diffs are
// byte-identical.
func TestCatalogDiff_E2E_Deterministic(t *testing.T) {
	seedDiffWorkspace(t)
	catalogDiffBaseFlag, catalogDiffHeadFlag = "main", "current"
	catalogJSONFlag = true

	var first string
	for i := 0; i < 5; i++ {
		out := captureStdout(t, func() error { return runCatalogDiff(nil, "") })
		if i == 0 {
			first = out
			continue
		}
		if out != first {
			t.Fatalf("diff output non-deterministic at run %d:\n got %s\nwant %s", i, out, first)
		}
	}
}

func TestCatalogDiff_E2E_ComponentFilter(t *testing.T) {
	seedDiffWorkspace(t)
	catalogDiffBaseFlag, catalogDiffHeadFlag = "main", "current"
	catalogJSONFlag = true

	out := captureStdout(t, func() error { return runCatalogDiff(nil, "svc-b") })
	data := decodeDiffEnvelope(t, out)
	if data.Component != "svc-b" {
		t.Errorf("component = %q, want svc-b", data.Component)
	}
	// Filtered to svc-b: only the addition remains, svc-a's change is dropped.
	if len(data.Changed) != 0 {
		t.Errorf("filtered Changed = %+v, want none", data.Changed)
	}
	if len(data.Added) != 1 || data.Added[0].Name != "svc-b" {
		t.Errorf("filtered Added = %+v, want [svc-b]", data.Added)
	}
}

func TestCatalogDiff_E2E_UnknownComponentExit6(t *testing.T) {
	seedDiffWorkspace(t)
	catalogDiffBaseFlag, catalogDiffHeadFlag = "main", "current"

	err := runCatalogDiff(nil, "ghost")
	if err == nil {
		t.Fatal("expected error for unknown component")
	}
	var coder interface{ ExitCode() int }
	if !asExit(err, &coder) || coder.ExitCode() != 6 {
		t.Errorf("expected exit 6 for unknown component, got %v", err)
	}
}

func TestCatalogDiff_E2E_BadSelectorExit1(t *testing.T) {
	seedDiffWorkspace(t)
	catalogDiffBaseFlag, catalogDiffHeadFlag = "branches/", "current"

	err := runCatalogDiff(nil, "")
	if err == nil {
		t.Fatal("expected error for malformed --base selector")
	}
	var coder interface{ ExitCode() int }
	if !asExit(err, &coder) || coder.ExitCode() != 1 {
		t.Errorf("expected exit 1 for bad selector, got %v", err)
	}
}

// TestCatalogDiff_E2E_NoDifferences proves diffing a snapshot against itself
// (base == head selector) reports no differences and exits 0.
func TestCatalogDiff_E2E_NoDifferences(t *testing.T) {
	seedDiffWorkspace(t)
	catalogDiffBaseFlag, catalogDiffHeadFlag = "current", "current"

	out := captureStdout(t, func() error { return runCatalogDiff(nil, "") })
	if !strings.Contains(out, "No differences") {
		t.Errorf("expected no-differences line, got:\n%s", out)
	}
}
