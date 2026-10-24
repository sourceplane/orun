package runner

import (
	"bytes"
	"os"
	"os/exec"
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
	env := ws.Env()
	// This .git has no index (no commit yet), so git falls back to the
	// source tree as its work tree; a real repository gets the staged tree
	// and a private index (TestStage_GitInTheStagedTree*).
	if env["GIT_DIR"] != filepath.Join(src, ".git") || env["GIT_WORK_TREE"] != src || env["GIT_INDEX_FILE"] != "" {
		t.Fatalf("git must answer from the source repository: %v", env)
	}
	if env["TURBO_CACHE_DIR"] != filepath.Join(src, ".orun", "cache", "turbo") {
		t.Fatalf("one turbo cache per checkout, shared by every job and run: %v", env)
	}
	ignore, err := os.ReadFile(filepath.Join(src, ".gitignore"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(ignore), "\n.orun/runs/\n") || !strings.Contains(string(ignore), "\n.orun/cache/\n") {
		t.Fatalf("staging ignores the staged trees and the checkout's caches: %q", ignore)
	}
}

func TestEnsureOrunGitignore_AddsWhatIsMissingOnce(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, ".gitignore")
	if err := os.WriteFile(path, []byte("node_modules\n/.orun/runs/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := ensureOrunGitignore(root); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := "node_modules\n/.orun/runs/\n\n# orun per-job staged workspaces and the checkout's caches\n.orun/cache/\n"
	if string(got) != want {
		t.Fatalf("a checkout that already ignores the staged trees gets the cache line, once:\n%q\nwant\n%q", got, want)
	}
	if err := ensureOrunGitignore(filepath.Join(root, "missing")); err == nil {
		t.Fatal("no .gitignore is never created")
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

// gitRepo makes a real repository with one commit: a root file and two apps.
func gitRepo(t *testing.T) string {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not installed")
	}
	src := t.TempDir()
	for rel, content := range map[string]string{
		"package.json":          `{"name":"root"}`,
		"apps/web/package.json": `{"name":"web"}`,
		"apps/web/src/index.ts": "export {}\n",
		"apps/api/src/index.ts": "export {}\n",
	} {
		p := filepath.Join(src, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gitIn(t, src, nil, "init", "-q", "-b", "main")
	gitIn(t, src, nil, "add", "-A")
	gitIn(t, src, nil, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "-m", "init")
	return src
}

// gitIn runs git in dir with extra environment, outside any ambient repo.
func gitIn(t *testing.T, dir string, extra map[string]string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "GIT_") {
			cmd.Env = append(cmd.Env, kv)
		}
	}
	for k, v := range extra {
		cmd.Env = append(cmd.Env, k+"="+v)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
	}
	return strings.TrimSpace(string(out))
}

// orun#757: the staged tree sits inside the source tree (.orun/runs/…, which
// is ignored), so with the source as git's work tree every command scoped to
// the working directory answered for an ignored directory: `git ls-files`
// from the staged root listed nothing.
func TestStage_GitInTheStagedTreeAnswersForTheStagedTree(t *testing.T) {
	src := gitRepo(t)
	indexBefore, err := os.ReadFile(filepath.Join(src, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	ws, err := stageJobWorkspace(src, "exec1", "web.stage", false, stagePolicy{mode: IsolationWorkspace, jobPath: "apps/web"})
	if err != nil {
		t.Fatal(err)
	}
	root := ws.Path()
	env := ws.Env()
	if env["GIT_DIR"] != filepath.Join(src, ".git") || env["GIT_WORK_TREE"] != root {
		t.Fatalf("git's work tree is the staged tree, its objects the source's: %v", env)
	}
	if idx := env["GIT_INDEX_FILE"]; idx == "" || filepath.Dir(idx) != filepath.Dir(root) {
		t.Fatalf("a private index beside the staged tree: %v", env)
	}

	if got := gitIn(t, root, env, "ls-files"); !strings.Contains(got, "apps/web/src/index.ts") || !strings.Contains(got, "package.json") {
		t.Fatalf("ls-files from the staged root lists the tracked files, got %q", got)
	}
	if got := gitIn(t, filepath.Join(root, "apps/web"), env, "ls-files"); got != "package.json\nsrc/index.ts" {
		t.Fatalf("ls-files is scoped to the working directory, got %q", got)
	}
	if got := gitIn(t, filepath.Join(root, "apps/web"), env, "rev-parse", "--show-prefix"); got != "apps/web/" {
		t.Fatalf("show-prefix is the job's directory, got %q", got)
	}
	if got, _ := filepath.EvalSymlinks(gitIn(t, root, env, "rev-parse", "--show-toplevel")); got != mustEval(t, root) {
		t.Fatalf("the top level is the staged tree, got %q", got)
	}
	if got := gitIn(t, root, env, "status", "--porcelain"); got != "" {
		t.Fatalf("an unchanged staged tree is clean, got %q", got)
	}
	if err := os.WriteFile(filepath.Join(root, "apps/web/src/index.ts"), []byte("export const x = 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := gitIn(t, root, env, "status", "--porcelain"); got != "M apps/web/src/index.ts" {
		t.Fatalf("an edit in the staged tree is the staged tree's, got %q", got)
	}

	indexAfter, err := os.ReadFile(filepath.Join(src, ".git", "index"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(indexBefore, indexAfter) {
		t.Fatal("nothing a job runs writes the source repository's index")
	}
	if got := gitIn(t, src, nil, "diff", "--name-only"); got != "" {
		t.Fatalf("no tracked file of the source tree changed, got %q", got)
	}
	ws.Cleanup()
	if exists(env["GIT_INDEX_FILE"]) {
		t.Fatal("the private index goes with the job's directory")
	}
}

func TestStage_GitInTheStagedTreeComponentMode(t *testing.T) {
	src := gitRepo(t)
	ws, err := stageJobWorkspace(src, "exec1", "web.stage", false, stagePolicy{mode: IsolationComponent, jobPath: "apps/web"})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	env := ws.Env()
	if got := gitIn(t, filepath.Join(ws.Path(), "apps/web"), env, "ls-files"); got != "package.json\nsrc/index.ts" {
		t.Fatalf("ls-files in the job's private directory, got %q", got)
	}
	if got := gitIn(t, ws.Path(), env, "ls-files"); !strings.Contains(got, "apps/api/src/index.ts") {
		t.Fatalf("the index still names every tracked file, got %q", got)
	}
}

// A linked worktree (or a submodule) has a .git FILE naming its git
// directory; staging from one used to leave git with no repository at all.
func TestStage_GitInTheStagedTreeOfALinkedWorktree(t *testing.T) {
	repo := gitRepo(t)
	wt := filepath.Join(t.TempDir(), "wt")
	gitIn(t, repo, nil, "worktree", "add", "-q", "-b", "feature", wt)
	ws, err := stageJobWorkspace(wt, "exec1", "web.stage", false, stagePolicy{mode: IsolationWorkspace, jobPath: "apps/web"})
	if err != nil {
		t.Fatal(err)
	}
	defer ws.Cleanup()
	env := ws.Env()
	if !strings.HasPrefix(mustEval(t, env["GIT_DIR"]), mustEval(t, filepath.Join(repo, ".git", "worktrees"))) {
		t.Fatalf("GIT_DIR is the worktree's git directory: %v", env)
	}
	if got := gitIn(t, ws.Path(), env, "rev-parse", "--abbrev-ref", "HEAD"); got != "feature" {
		t.Fatalf("git sees the worktree's branch, got %q", got)
	}
	if got := gitIn(t, filepath.Join(ws.Path(), "apps/web"), env, "ls-files"); got != "package.json\nsrc/index.ts" {
		t.Fatalf("ls-files from a worktree's staged tree, got %q", got)
	}
}

func mustEval(t *testing.T, p string) string {
	t.Helper()
	got, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return got
}
