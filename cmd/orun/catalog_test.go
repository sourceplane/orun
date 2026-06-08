package main

// catalog_test.go covers the C5 PR-1 `orun catalog` command family:
//
//   - parseCatalogSelector: every selector form + the malformed-selector
//     exit-1 contract (the shared helper used by all read subcommands).
//   - writeCatalogEnvelope: stable envelope shape (apiVersion/kind/data +
//     always-present warnings array).
//   - pure input-builder helpers (authoritative rule, short repo name,
//     working-tree label).
//   - E2E refresh → refs against a real seeded git workspace: created form,
//     idempotent reuse, refs enumeration, and the exit-code contract.
//
// The refresh path shells out to git via internal/sourcectx, so the E2E
// tests seed an actual git repo in a temp dir. Tests that only exercise the
// pure helpers skip git entirely.

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// ----- parseCatalogSelector ------------------------------------------

func TestParseCatalogSelector_Forms(t *testing.T) {
	cases := []struct {
		name     string
		source   string
		snapshot string
		wantKind string
		wantSnap string
	}{
		{"empty defaults to current", "", "", "current", ""},
		{"current", "current", "", "current", ""},
		{"main", "main", "", "main", ""},
		{"latest", "latest", "", "latest", ""},
		{"branch", "branches/feature-x", "", "branch", ""},
		{"pr canonical", "prs/139", "", "pr", ""},
		{"snapshot pin", "", "cat-deadbeef", "", "cat-deadbeef"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			prevSrc, prevSnap := catalogSourceFlag, catalogSnapshotFlag
			catalogSourceFlag, catalogSnapshotFlag = tc.source, tc.snapshot
			t.Cleanup(func() { catalogSourceFlag, catalogSnapshotFlag = prevSrc, prevSnap })

			sel, err := parseCatalogSelector()
			if err != nil {
				t.Fatalf("parseCatalogSelector(%q,%q): %v", tc.source, tc.snapshot, err)
			}
			if sel.Kind != tc.wantKind {
				t.Errorf("Kind = %q, want %q", sel.Kind, tc.wantKind)
			}
			if sel.Snapshot != tc.wantSnap {
				t.Errorf("Snapshot = %q, want %q", sel.Snapshot, tc.wantSnap)
			}
		})
	}
}

func TestParseCatalogSelector_MalformedExit1(t *testing.T) {
	prevSrc, prevSnap := catalogSourceFlag, catalogSnapshotFlag
	catalogSourceFlag, catalogSnapshotFlag = "branches/", ""
	t.Cleanup(func() { catalogSourceFlag, catalogSnapshotFlag = prevSrc, prevSnap })

	_, err := parseCatalogSelector()
	if err == nil {
		t.Fatal("expected error for malformed selector, got nil")
	}
	var coder interface{ ExitCode() int }
	if !asExit(err, &coder) || coder.ExitCode() != 1 {
		t.Errorf("expected exit code 1, got err=%v", err)
	}
}

// ----- writeCatalogEnvelope ------------------------------------------

func TestWriteCatalogEnvelope_Shape(t *testing.T) {
	out := captureStdout(t, func() error {
		return writeCatalogEnvelope("CatalogTestResult", map[string]any{"k": "v"}, nil)
	})
	var env struct {
		APIVersion string         `json:"apiVersion"`
		Kind       string         `json:"kind"`
		Data       map[string]any `json:"data"`
		Warnings   []string       `json:"warnings"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("envelope not valid JSON: %v\n%s", err, out)
	}
	if env.APIVersion != catalogmodel.APIVersionV1Alpha1 {
		t.Errorf("apiVersion = %q", env.APIVersion)
	}
	if env.Kind != "CatalogTestResult" {
		t.Errorf("kind = %q", env.Kind)
	}
	if env.Data["k"] != "v" {
		t.Errorf("data not round-tripped: %v", env.Data)
	}
	if env.Warnings == nil {
		t.Error("warnings must be a non-nil array even when empty")
	}
}

// ----- pure input-builder helpers ------------------------------------

func TestComputeCatalogAuthoritative(t *testing.T) {
	cases := []struct {
		scope string
		dirty bool
		want  bool
	}{
		{catalogmodel.SourceScopeBranchMain, false, true},
		{catalogmodel.SourceScopeBranchProtected, false, true},
		{catalogmodel.SourceScopeBranchMain, true, false},
		{catalogmodel.SourceScopeBranchFeature, false, false},
		{catalogmodel.SourceScopePR, false, false},
		{catalogmodel.SourceScopeLocalDirty, false, false},
	}
	for _, tc := range cases {
		if got := computeCatalogAuthoritative(tc.scope, tc.dirty); got != tc.want {
			t.Errorf("authoritative(%q, dirty=%v) = %v, want %v", tc.scope, tc.dirty, got, tc.want)
		}
	}
}

func TestShortRepoName(t *testing.T) {
	cases := []struct {
		wsRepo string
		root   string
		want   string
	}{
		{"sourceplane/orun", "/x/y", "orun"},
		{"orun", "/x/y", "orun"},
		{"", "/x/myworkspace", "myworkspace"},
		{"a/b/c", "/x/y", "c"},
	}
	for _, tc := range cases {
		if got := shortRepoName(tc.wsRepo, tc.root); got != tc.want {
			t.Errorf("shortRepoName(%q,%q) = %q, want %q", tc.wsRepo, tc.root, got, tc.want)
		}
	}
}

func TestWorkingTreeLabel(t *testing.T) {
	if workingTreeLabel(true) != catalogmodel.WorkingTreeDirty {
		t.Error("dirty=true should map to dirty")
	}
	if workingTreeLabel(false) != catalogmodel.WorkingTreeClean {
		t.Error("dirty=false should map to clean")
	}
}

// ----- E2E refresh → refs --------------------------------------------

// seedGitCatalogWorkspace plants a real git repo with one valid component
// under the global storeDir() (via withTempIntentRoot). The resolver shells
// out to git, so a real repo with a remote + main branch is required for a
// branch-main / authoritative snapshot. Skips the test if git is absent.
func seedGitCatalogWorkspace(t *testing.T, dir string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	run := func(args ...string) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-q")
	run("config", "user.email", "t@t.co")
	run("config", "user.name", "t")
	run("remote", "add", "origin", "https://github.com/sourceplane/orun.git")
	run("checkout", "-q", "-b", "main")

	compDir := filepath.Join(dir, "svc-a")
	if err := os.MkdirAll(compDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(compDir, "component.yaml"), []byte(""+
		"apiVersion: orun.io/v1alpha1\n"+
		"kind: Component\n"+
		"metadata:\n"+
		"  name: svc-a\n"+
		"spec:\n"+
		"  type: service\n"+
		"  owner: team/x\n"+
		"  system: payments\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "intent.yaml"), []byte(""+
		"apiVersion: orun.io/v1alpha1\n"+
		"kind: Intent\n"+
		"metadata:\n"+
		"  name: demo\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "init")
}

func TestCatalogRefresh_E2E_CreatedThenReused(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedGitCatalogWorkspace(t, dir)

	resetCatalogFlags(t)
	catalogJSONFlag = true

	// First refresh: created.
	out := captureStdout(t, func() error { return runCatalogRefresh(nil) })
	var first catalogEnvelope
	first.Data = &catalogRefreshData{}
	if err := json.Unmarshal([]byte(out), &first); err != nil {
		t.Fatalf("refresh envelope: %v\n%s", err, out)
	}
	d := first.Data.(*catalogRefreshData)
	if !d.Created || d.Reused {
		t.Errorf("expected created=true reused=false, got %+v", d)
	}
	if d.CatalogSnapshotKey == "" || !strings.HasPrefix(d.CatalogSnapshotKey, "cat-") {
		t.Errorf("bad catalogSnapshotKey: %q", d.CatalogSnapshotKey)
	}
	if !d.Authoritative || d.Mode != "authoritative" {
		t.Errorf("branch-main clean should be authoritative, got %+v", d)
	}
	if d.Components != 1 {
		t.Errorf("Components = %d, want 1", d.Components)
	}
	firstKey := d.CatalogSnapshotKey

	// Second refresh: reused (idempotent), same key.
	out2 := captureStdout(t, func() error { return runCatalogRefresh(nil) })
	var second catalogEnvelope
	second.Data = &catalogRefreshData{}
	if err := json.Unmarshal([]byte(out2), &second); err != nil {
		t.Fatalf("second refresh envelope: %v\n%s", err, out2)
	}
	d2 := second.Data.(*catalogRefreshData)
	if d2.Created || !d2.Reused {
		t.Errorf("expected created=false reused=true, got %+v", d2)
	}
	if d2.CatalogSnapshotKey != firstKey {
		t.Errorf("idempotent key drift: %q != %q", d2.CatalogSnapshotKey, firstKey)
	}
}

func TestCatalogRefs_E2E_AfterRefresh(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedGitCatalogWorkspace(t, dir)

	resetCatalogFlags(t)
	catalogJSONFlag = true

	_ = captureStdout(t, func() error { return runCatalogRefresh(nil) })

	out := captureStdout(t, func() error { return runCatalogRefs(nil) })
	var env catalogEnvelope
	env.Data = &catalogRefsData{}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("refs envelope: %v\n%s", err, out)
	}
	if env.Kind != kindCatalogRefsResult {
		t.Errorf("kind = %q", env.Kind)
	}
	data := env.Data.(*catalogRefsData)
	byName := map[string]catalogRefEntry{}
	for _, r := range data.Refs {
		byName[r.Name] = r
	}
	for _, want := range []string{"current", "main", "latest"} {
		r, ok := byName[want]
		if !ok {
			t.Errorf("missing ref %q", want)
			continue
		}
		if r.CatalogSnapshotKey == "" || r.SourceSnapshotKey == "" {
			t.Errorf("ref %q has empty keys: %+v", want, r)
		}
		if !r.Authoritative {
			t.Errorf("ref %q expected authoritative", want)
		}
	}
}

func TestCatalogRefs_E2E_EmptyStore(t *testing.T) {
	withTempIntentRoot(t)
	resetCatalogFlags(t)
	catalogJSONFlag = true

	out := captureStdout(t, func() error { return runCatalogRefs(nil) })
	var env catalogEnvelope
	env.Data = &catalogRefsData{}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("refs envelope: %v\n%s", err, out)
	}
	data := env.Data.(*catalogRefsData)
	if len(data.Refs) != 0 {
		t.Errorf("expected empty refs on fresh store, got %d", len(data.Refs))
	}
}

func TestCatalogRefresh_SyncNoop(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedGitCatalogWorkspace(t, dir)
	resetCatalogFlags(t)
	catalogSyncFlag = true

	// --sync now performs the full local refresh first, then reports the
	// not-configured notice from the wired NoopSyncer (and still exits 0).
	out := captureStdout(t, func() error {
		if err := runCatalogRefresh(nil); err != nil {
			t.Fatalf("refresh --sync must exit 0, got %v", err)
		}
		return nil
	})
	if !strings.Contains(out, "Catalog snapshot created") {
		t.Errorf("expected the local refresh summary, got:\n%s", out)
	}
	if !strings.Contains(out, "remote sync not configured") {
		t.Errorf("expected sync not-configured notice, got:\n%s", out)
	}
}

// ----- helpers -------------------------------------------------------

// resetCatalogFlags zeroes the package-level catalog flag vars and restores
// them on cleanup, so each test starts from a known state regardless of
// cobra binding order.
func resetCatalogFlags(t *testing.T) {
	t.Helper()
	prev := struct {
		src, snap, diffBase, diffHead string
		strict, noInfer, json, sync   bool
	}{catalogSourceFlag, catalogSnapshotFlag, catalogDiffBaseFlag, catalogDiffHeadFlag, catalogStrictFlag, catalogNoInferFlag, catalogJSONFlag, catalogSyncFlag}
	catalogSourceFlag, catalogSnapshotFlag = "", ""
	catalogDiffBaseFlag, catalogDiffHeadFlag = "", ""
	catalogStrictFlag, catalogNoInferFlag, catalogJSONFlag, catalogSyncFlag = false, false, false, false
	t.Cleanup(func() {
		catalogSourceFlag, catalogSnapshotFlag = prev.src, prev.snap
		catalogDiffBaseFlag, catalogDiffHeadFlag = prev.diffBase, prev.diffHead
		catalogStrictFlag, catalogNoInferFlag = prev.strict, prev.noInfer
		catalogJSONFlag, catalogSyncFlag = prev.json, prev.sync
	})
}

// asExit is a tiny errors.As shim kept local so the test file's intent is
// obvious at the call site.
func asExit(err error, target *interface{ ExitCode() int }) bool {
	for err != nil {
		if c, ok := err.(interface{ ExitCode() int }); ok {
			*target = c
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
