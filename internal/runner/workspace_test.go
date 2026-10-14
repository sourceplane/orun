package runner

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// sourceTree builds a small monorepo: a .git directory, a top-level
// node_modules, two apps with their own node_modules (pnpm-style symlinks
// into the top-level one) and build outputs.
func sourceTree(t *testing.T) string {
	t.Helper()
	src := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write(".git/HEAD", "ref: refs/heads/main\n")
	write(".gitignore", "node_modules\n")
	write("package.json", `{"name":"root"}`)
	write("pnpm-lock.yaml", "lockfileVersion: 9\n")
	write("node_modules/.pnpm/left-pad@1/node_modules/left-pad/index.js", "module.exports = 1\n")
	write("apps/web/package.json", `{"name":"web"}`)
	write("apps/web/src/index.ts", "export {}\n")
	write("apps/web/dist/index.js", "built\n")
	write("apps/api/package.json", `{"name":"api"}`)
	write("apps/api/src/index.ts", "export {}\n")
	if err := os.MkdirAll(filepath.Join(src, "apps/web/node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../../../node_modules/.pnpm/left-pad@1/node_modules/left-pad", filepath.Join(src, "apps/web/node_modules/left-pad")); err != nil {
		t.Fatal(err)
	}
	return src
}

func exists(path string) bool { _, err := os.Lstat(path); return err == nil }

func isSymlinkTo(t *testing.T, path, target string) bool {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		return false
	}
	got, _ := os.Readlink(path)
	return got == target
}

func TestStage_WorkspaceNeverCopiesGitAndPointsGitAtTheSource(t *testing.T) {
	src := sourceTree(t)
	ws, err := stageJobWorkspace(src, "exec1", "web.stage", false, stagePolicy{mode: IsolationWorkspace, jobPath: "apps/web"})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	root := ws.Path()
	if exists(filepath.Join(root, ".git")) {
		t.Fatal(".git must never be staged")
	}
	if !exists(filepath.Join(root, "apps/api/src/index.ts")) || !exists(filepath.Join(root, "package.json")) {
		t.Fatal("workspace mode copies the whole tree")
	}
	if exists(filepath.Join(root, "node_modules")) || exists(filepath.Join(root, "apps/web/node_modules")) || exists(filepath.Join(root, "apps/web/dist")) {
		t.Fatal("without a lane install, dependencies and outputs are skipped as before")
	}
	env := ws.Env("exec1")
	if env["GIT_DIR"] != filepath.Join(src, ".git") || env["GIT_WORK_TREE"] != src {
		t.Fatalf("git must answer from the source repository: %v", env)
	}
	if env["TURBO_CACHE_DIR"] != filepath.Join(src, ".orun", "runs", "exec1", "cache", "turbo") {
		t.Fatalf("one turbo cache per run: %v", env)
	}
}

func TestStage_WorkspaceSharesDepsAfterALaneInstall(t *testing.T) {
	src := sourceTree(t)
	ws, err := stageJobWorkspace(src, "exec1", "web.stage", false, stagePolicy{mode: IsolationWorkspace, jobPath: "apps/web", sharedDeps: true})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	root := ws.Path()
	if !isSymlinkTo(t, filepath.Join(root, "node_modules"), filepath.Join(src, "node_modules")) {
		t.Fatal("the top-level node_modules is the lane's, linked")
	}
	if !isSymlinkTo(t, filepath.Join(root, "apps/web/node_modules/left-pad"), "../../../node_modules/.pnpm/left-pad@1/node_modules/left-pad") {
		t.Fatal("a nested node_modules is copied with its symlinks intact")
	}
	if _, err := os.Stat(filepath.Join(root, "apps/web/node_modules/left-pad/index.js")); err != nil {
		t.Fatalf("the nested symlink resolves through the linked top-level node_modules: %v", err)
	}
	if exists(filepath.Join(root, "apps/web/dist")) {
		t.Fatal("outputs stay private: skipped")
	}
}

func TestStage_ComponentCopiesTheJobsDirectoryAndLinksTheRest(t *testing.T) {
	src := sourceTree(t)
	ws, err := stageJobWorkspace(src, "exec1", "web.stage", false, stagePolicy{mode: IsolationComponent, jobPath: "apps/web"})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	root := ws.Path()
	for _, linked := range []string{"package.json", "pnpm-lock.yaml", "node_modules", "apps/api", ".gitignore"} {
		if !isSymlinkTo(t, filepath.Join(root, linked), filepath.Join(src, linked)) {
			t.Fatalf("%s must be a link to the source tree", linked)
		}
	}
	if exists(filepath.Join(root, ".git")) {
		t.Fatal(".git is neither copied nor linked")
	}
	if info, err := os.Lstat(filepath.Join(root, "apps/web")); err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		t.Fatal("the job's directory is a private copy")
	}
	if !exists(filepath.Join(root, "apps/web/src/index.ts")) || exists(filepath.Join(root, "apps/web/dist")) {
		t.Fatal("the copy carries sources and skips outputs")
	}
	if !isSymlinkTo(t, filepath.Join(root, "apps/web/node_modules/left-pad"), "../../../node_modules/.pnpm/left-pad@1/node_modules/left-pad") {
		t.Fatal("the component's node_modules is kept so its symlinks reach the linked top-level one")
	}
	// A write in the private copy never reaches the source.
	if err := os.WriteFile(filepath.Join(root, "apps/web/src/index.ts"), []byte("changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(src, "apps/web/src/index.ts")); strings.TrimSpace(string(b)) != "export {}" {
		t.Fatal("the source tree was modified through the staged copy")
	}
}

func TestStage_ComponentWithoutAPathIsTheWholeTree(t *testing.T) {
	src := sourceTree(t)
	for _, p := range []string{"", ".", "./", "../elsewhere"} {
		ws, err := stageJobWorkspace(src, "exec1", "root.stage", false, stagePolicy{mode: IsolationComponent, jobPath: p})
		if err != nil {
			t.Fatalf("%q: %v", p, err)
		}
		if info, err := os.Lstat(filepath.Join(ws.Path(), "apps")); err != nil || info.Mode()&os.ModeSymlink != 0 {
			t.Fatalf("%q: a job at the tree's root (or outside it) is staged as a whole copy", p)
		}
		ws.Cleanup()
	}
	if _, err := stageJobWorkspace(src, "exec1", "x", false, stagePolicy{mode: IsolationComponent, jobPath: "apps/nope"}); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("a missing job path is an error, got %v", err)
	}
}
