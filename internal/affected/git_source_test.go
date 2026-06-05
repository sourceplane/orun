package affected

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/git"
)

// TestGitChangeSource_Untracked exercises ChangedPaths against a real git repo
// using only untracked files — no commit, so it is hermetic against the
// environment's commit-signing setup.
func TestGitChangeSource_Untracked(t *testing.T) {
	dir := t.TempDir()
	if out, err := exec.Command("git", "-C", dir, "init").CombinedOutput(); err != nil {
		t.Skipf("git init unavailable: %v (%s)", err, out)
	}
	mustWrite(t, dir, "intent.yaml", "components: []\n")
	mustWrite(t, dir, "apps/api/main.go", "package main\n")

	t.Chdir(dir) // git helpers run in CWD

	src := GitChangeSource{Options: git.ChangeOptions{Untracked: true}, IntentPath: "intent.yaml"}
	files, ic, err := src.ChangedPaths(context.Background())
	if err != nil {
		t.Fatalf("ChangedPaths: %v", err)
	}
	if !containsPath(files, "intent.yaml") || !containsPath(files, "apps/api/main.go") {
		t.Fatalf("untracked files not reported: %v", files)
	}
	if !ic.Changed {
		t.Fatalf("intent change not detected")
	}
	// No commits exist, so base/head reads fail → bytes nil (the undiffable path).
	if ic.Base != nil || ic.Head != nil {
		t.Errorf("expected nil base/head bytes with no commits, got %v / %v", ic.Base, ic.Head)
	}
}

func mustWrite(t *testing.T, root, rel, body string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func containsPath(files []string, want string) bool {
	for _, f := range files {
		if normPath(f) == want {
			return true
		}
	}
	return false
}
