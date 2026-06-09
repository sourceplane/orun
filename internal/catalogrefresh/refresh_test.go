package catalogrefresh

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// seedWorkspace plants a clean git repo with one resolvable component and
// returns the workspace root + a fresh (empty) object-model root.
func seedWorkspace(t *testing.T) (workspaceRoot, objModelRoot string) {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
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
	run("config", "commit.gpgsign", "false") // sandboxes may enforce signing
	run("checkout", "-q", "-b", "main")

	comp := filepath.Join(dir, "svc-a")
	if err := os.MkdirAll(comp, 0o755); err != nil {
		t.Fatal(err)
	}
	mustWrite(t, filepath.Join(comp, "component.yaml"),
		"apiVersion: orun.io/v1alpha1\nkind: Component\nmetadata:\n  name: svc-a\nspec:\n  type: service\n  owner: team/x\n  system: payments\n")
	mustWrite(t, filepath.Join(dir, "intent.yaml"),
		"apiVersion: orun.io/v1alpha1\nkind: Intent\nmetadata:\n  name: demo\n")
	run("add", "-A")
	run("commit", "-qm", "init")

	return dir, filepath.Join(t.TempDir(), "objectmodel")
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestEnsureFresh_CreateThenFreshThenForce(t *testing.T) {
	ws, om := seedWorkspace(t)
	ctx := context.Background()

	// First call: no catalog yet → resolves + writes.
	r1, err := EnsureFresh(ctx, om, ws, false, Config{OrunVersion: "test"})
	if err != nil {
		t.Fatalf("EnsureFresh #1: %v", err)
	}
	if !r1.Refreshed || r1.CatalogID == "" {
		t.Fatalf("first refresh should write a catalog, got %+v", r1)
	}

	// Second call, unchanged tree: the staleness gate skips the resolve.
	r2, err := EnsureFresh(ctx, om, ws, false, Config{OrunVersion: "test"})
	if err != nil {
		t.Fatalf("EnsureFresh #2: %v", err)
	}
	if !r2.Fresh || r2.Refreshed {
		t.Fatalf("unchanged refresh should be Fresh (no resolve), got %+v", r2)
	}
	if r2.CatalogID != r1.CatalogID {
		t.Errorf("fresh catalog id drift: %q != %q", r2.CatalogID, r1.CatalogID)
	}

	// Force on an unchanged tree: resolves but produces the same id → Reused.
	r3, err := EnsureFresh(ctx, om, ws, true, Config{OrunVersion: "test"})
	if err != nil {
		t.Fatalf("EnsureFresh #3 (force): %v", err)
	}
	if !r3.Refreshed || !r3.Reused {
		t.Fatalf("forced refresh of an unchanged tree should be Reused, got %+v", r3)
	}
	if r3.CatalogID != r1.CatalogID {
		t.Errorf("forced catalog id drift: %q != %q", r3.CatalogID, r1.CatalogID)
	}
}

func TestEnsureFresh_LockHeldSkips(t *testing.T) {
	ws, om := seedWorkspace(t)
	ctx := context.Background()
	if _, err := EnsureFresh(ctx, om, ws, false, Config{OrunVersion: "test"}); err != nil {
		t.Fatalf("seed refresh: %v", err)
	}

	// Hold the advisory lock, then force a refresh — it must skip (non-blocking).
	release, ok, err := tryLock(om)
	if err != nil || !ok {
		t.Fatalf("acquire lock: ok=%v err=%v", ok, err)
	}
	defer release()

	r, err := EnsureFresh(ctx, om, ws, true, Config{OrunVersion: "test"})
	if err != nil {
		t.Fatalf("EnsureFresh under held lock: %v", err)
	}
	if !r.Skipped || r.Refreshed {
		t.Fatalf("expected Skipped under a held lock, got %+v", r)
	}
}

func TestIsStale(t *testing.T) {
	ws, om := seedWorkspace(t)
	ctx := context.Background()

	if stale, err := IsStale(ctx, om, ws); err != nil || !stale {
		t.Fatalf("a missing catalog must be stale (stale=%v err=%v)", stale, err)
	}
	if _, err := EnsureFresh(ctx, om, ws, false, Config{OrunVersion: "test"}); err != nil {
		t.Fatalf("refresh: %v", err)
	}
	if stale, err := IsStale(ctx, om, ws); err != nil || stale {
		t.Fatalf("a just-refreshed catalog must be fresh (stale=%v err=%v)", stale, err)
	}
}

func TestAuthoritative(t *testing.T) {
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
		if got := Authoritative(tc.scope, tc.dirty); got != tc.want {
			t.Errorf("Authoritative(%q, dirty=%v) = %v, want %v", tc.scope, tc.dirty, got, tc.want)
		}
	}
}

func TestShortRepoName(t *testing.T) {
	cases := []struct{ wsRepo, root, want string }{
		{"sourceplane/orun", "/x/y", "orun"},
		{"orun", "/x/y", "orun"},
		{"", "/x/myworkspace", "myworkspace"},
		{"a/b/c", "/x/y", "c"},
	}
	for _, tc := range cases {
		if got := ShortRepoName(tc.wsRepo, tc.root); got != tc.want {
			t.Errorf("ShortRepoName(%q,%q) = %q, want %q", tc.wsRepo, tc.root, got, tc.want)
		}
	}
}

func TestRepoForInputs(t *testing.T) {
	if got := RepoForInputs("sourceplane/orun", "/x/y"); got != "sourceplane/orun" {
		t.Errorf("RepoForInputs(remote) = %q, want sourceplane/orun", got)
	}
	if got := RepoForInputs("", "/x/myws"); got != "myws" {
		t.Errorf("RepoForInputs(no-remote) = %q, want myws", got)
	}
}

// TestEnsureFresh_CatalogID_VersionIndependent proves the object-model catalog
// id does not depend on OrunVersion — so the cockpit (TUI) can pass its own
// version string and still converge on the CLI's catalog (no churn).
func TestEnsureFresh_CatalogID_VersionIndependent(t *testing.T) {
	ws, om := seedWorkspace(t)
	ctx := context.Background()

	r1, err := EnsureFresh(ctx, om, ws, true, Config{OrunVersion: "cli-1.2.3"})
	if err != nil {
		t.Fatalf("refresh v1: %v", err)
	}
	r2, err := EnsureFresh(ctx, om, ws, true, Config{OrunVersion: "tui-9.9.9"})
	if err != nil {
		t.Fatalf("refresh v2: %v", err)
	}
	if r1.CatalogID == "" || r1.CatalogID != r2.CatalogID {
		t.Fatalf("catalog id must be OrunVersion-independent: %q vs %q", r1.CatalogID, r2.CatalogID)
	}
	if !r2.Reused {
		t.Errorf("second forced refresh should reuse the same catalog id, got %+v", r2)
	}
}
