package runner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// stagedWorkspace describes a per-job isolated working tree materialized from
// the source repo using copy-on-write where the filesystem allows it. It exists
// to keep parallel jobs from racing on shared mutable state (node_modules,
// .turbo, dist, .terraform, ...).
type stagedWorkspace struct {
	root    string
	source  string
	jobID   string
	keep    bool
	cleanup func()
	// gitDir is the source repository's git directory; gitIndex is this
	// job's private copy of its index, beside the staged tree.
	gitDir   string
	gitIndex string
}

func (s *stagedWorkspace) Path() string {
	if s == nil {
		return ""
	}
	return s.root
}

func (s *stagedWorkspace) Cleanup() {
	if s == nil || s.keep || s.cleanup == nil {
		return
	}
	s.cleanup()
}

// Env is what a job in the staged tree needs to keep behaving as if it were
// in the source tree: git answers from the source repository (the tree never
// carries .git), and the turbo cache is one per run, shared by every job, so
// the second job of a lane hits what the first built
// (specs/orun-runner-efficiency §5.3).
//
// The work tree git sees is the staged tree itself, with an index of its own.
// The staged tree lives inside the source tree (.orun/runs/…, ignored), so
// with the source as the work tree every git command scoped to the working
// directory — `git ls-files`, `git status .`, `git rev-parse --show-prefix` —
// answered for an ignored subdirectory and listed nothing (orun#757). The
// private index keeps a job's `git status` from rewriting the index every
// other job reads; objects and refs still come from the source repository.
// Without an index to copy (no commit yet), git falls back to the source tree.
func (s *stagedWorkspace) Env(execID string) map[string]string {
	if s == nil {
		return nil
	}
	env := map[string]string{}
	if s.gitDir != "" {
		env["GIT_DIR"] = s.gitDir
		if s.gitIndex != "" {
			env["GIT_WORK_TREE"] = s.root
			env["GIT_INDEX_FILE"] = s.gitIndex
		} else {
			env["GIT_WORK_TREE"] = s.source
		}
	}
	if execID != "" {
		env["TURBO_CACHE_DIR"] = filepath.Join(s.source, ".orun", "runs", execID, "cache", "turbo")
	}
	return env
}

// outputDirs are regenerable build outputs and caches, skipped during staging:
// the job recreates them, and copying them defeats the purpose (they are the
// largest and most volatile contents).
var outputDirs = map[string]struct{}{
	".next":         {},
	".nuxt":         {},
	".turbo":        {},
	".svelte-kit":   {},
	"dist":          {},
	"build":         {},
	"target":        {},
	".venv":         {},
	"venv":          {},
	"__pycache__":   {},
	".terraform":    {},
	".pytest_cache": {},
	".mypy_cache":   {},
}

// depDirs are installed dependencies. A job that installs them itself
// recreates them, so they are skipped like outputs; a job whose lane already
// installed them (a lane-scoped step ran) keeps them: the top-level one is
// linked to the source tree's and nested ones are copied, their symlinks
// into the top-level one intact.
var depDirs = map[string]struct{}{
	"node_modules": {},
}

// vcsDirs are never staged: a job never needs its own copy of the
// repository's history, and git in the staged tree is pointed at the source
// repository through the environment (stagedWorkspace.Env).
var vcsDirs = map[string]struct{}{
	".git": {},
}

// stagePolicy says what staging keeps, copies and links.
type stagePolicy struct {
	// mode is workspace or component.
	mode IsolationMode
	// jobPath is the job's directory relative to the source tree; in
	// component mode it is the only subtree copied.
	jobPath string
	// sharedDeps keeps installed dependencies: the lane installed them once.
	sharedDeps bool
}

// skipDir reports whether a directory is left out of a copy.
func (p stagePolicy) skipDir(name string) bool {
	if _, ok := vcsDirs[name]; ok {
		return true
	}
	if _, ok := outputDirs[name]; ok {
		return true
	}
	if _, ok := depDirs[name]; ok {
		return !p.sharedDeps
	}
	return false
}

// stageJobWorkspace creates an isolated working copy of sourceDir under
// <sourceDir>/.orun/runs/<execID>/<safeJobID>/work and returns the path. The
// directory layout intentionally lives inside the source repo (per user choice)
// so tool discovery (workspace-relative configs, package.json hoisting, etc.)
// keeps working. We auto-add .orun/runs/ to .gitignore so the staged trees
// never leak into commits.
//
// In workspace mode the whole tree is copied, less .git, outputs and (unless
// the lane shares them) dependencies. In component mode only the job's own
// directory is copied; every other top-level entry is a symlink to the
// source tree's, so the staged tree is O(component) and a lane's one install
// serves every job (specs/orun-runner-efficiency §5.3).
func stageJobWorkspace(sourceDir, execID, jobID string, keep bool, policy stagePolicy) (*stagedWorkspace, error) {
	src, err := filepath.Abs(sourceDir)
	if err != nil {
		return nil, fmt.Errorf("resolve workspace source: %w", err)
	}
	if err := ensureOrunGitignore(src); err != nil {
		// Non-fatal — staging still works without it, user just sees noise in git status.
		_ = err
	}

	safe := safeJobID(jobID)
	root := filepath.Join(src, ".orun", "runs", execID, safe, "work")
	if err := os.RemoveAll(root); err != nil {
		return nil, fmt.Errorf("clear stale stage %s: %w", root, err)
	}
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil {
		return nil, fmt.Errorf("create stage parent: %w", err)
	}

	if policy.mode == IsolationComponent && componentRel(policy.jobPath) != "" {
		err = materializeComponent(src, root, componentRel(policy.jobPath), policy)
	} else {
		err = materializeTree(src, root, policy, true)
	}
	if err != nil {
		_ = os.RemoveAll(root)
		return nil, fmt.Errorf("stage workspace: %w", err)
	}

	ws := &stagedWorkspace{
		root:   root,
		source: src,
		jobID:  jobID,
		keep:   keep,
		cleanup: func() {
			// Remove the per-job parent (work/'s grandparent stays for sibling jobs;
			// the per-job dir is owned by us alone).
			_ = os.RemoveAll(filepath.Dir(root))
			// Best-effort: drop the per-exec dir if it became empty.
			_ = os.Remove(filepath.Dir(filepath.Dir(root)))
		},
	}
	if gd := sourceGitDir(src); gd != "" {
		ws.gitDir = gd
		// The private index sits beside work/, so it goes with the job's dir.
		idx := filepath.Join(filepath.Dir(root), "git-index")
		if info, err := os.Stat(filepath.Join(gd, "index")); err == nil && !info.IsDir() {
			if err := copyFileBytes(filepath.Join(gd, "index"), idx, 0o644); err == nil {
				ws.gitIndex = idx
			}
		}
	}
	return ws, nil
}

// sourceGitDir is the source tree's git directory: its .git directory, or the
// directory a .git file names (a linked worktree or a submodule), or "" when
// the tree is not a repository.
func sourceGitDir(src string) string {
	p := filepath.Join(src, ".git")
	info, err := os.Stat(p)
	if err != nil {
		return ""
	}
	if info.IsDir() {
		return p
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(data))
	if !strings.HasPrefix(line, "gitdir:") {
		return ""
	}
	dir := strings.TrimSpace(strings.TrimPrefix(line, "gitdir:"))
	if dir == "" {
		return ""
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(src, dir)
	}
	return filepath.Clean(dir)
}

// componentRel is a job path as a clean relative path inside the source
// tree, or "" when it names the tree itself (or escapes it).
func componentRel(jobPath string) string {
	p := filepath.ToSlash(filepath.Clean(strings.TrimSpace(jobPath)))
	if p == "" || p == "." || p == "/" || filepath.IsAbs(jobPath) || p == ".." || strings.HasPrefix(p, "../") {
		return ""
	}
	return p
}

// materializeComponent builds dst as: the job's directory copied privately,
// every other entry on the way down and at the top linked to the source.
func materializeComponent(src, dst, rel string, policy stagePolicy) error {
	parts := strings.Split(rel, "/")
	curSrc, curDst := src, dst
	for i, part := range parts {
		if err := os.MkdirAll(curDst, 0o755); err != nil {
			return err
		}
		entries, err := os.ReadDir(curSrc)
		if err != nil {
			return err
		}
		found := false
		for _, entry := range entries {
			name := entry.Name()
			if name == ".orun" || (i == 0 && name == ".git") {
				continue
			}
			if name == part {
				found = true
				continue
			}
			if err := os.Symlink(filepath.Join(curSrc, name), filepath.Join(curDst, name)); err != nil {
				return fmt.Errorf("link %s: %w", filepath.Join(curDst, name), err)
			}
		}
		if !found {
			return fmt.Errorf("job path %s: %s not found in %s", rel, part, curSrc)
		}
		curSrc, curDst = filepath.Join(curSrc, part), filepath.Join(curDst, part)
	}
	// The component itself: a private copy, dependencies kept (their symlinks
	// point back up into the linked top-level node_modules).
	policy.sharedDeps = true
	return materializeTree(curSrc, curDst, policy, false)
}

// materializeTree walks src and recreates it under dst. It tries the fastest
// available cloning primitive (clonefile/FICLONE on supported filesystems),
// falls back to byte copy. Directories the policy skips are left out at every
// level; .orun is skipped to avoid recursive staging. At the top level of a
// shared-deps copy, dependency directories are linked to the source rather
// than copied: the lane's one install serves every job.
func materializeTree(src, dst string, policy stagePolicy, top bool) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		name := entry.Name()
		if name == ".orun" {
			continue
		}
		srcPath := filepath.Join(src, name)
		dstPath := filepath.Join(dst, name)

		if entry.IsDir() {
			if policy.skipDir(name) {
				continue
			}
			if _, dep := depDirs[name]; dep && top && policy.sharedDeps {
				if err := os.Symlink(srcPath, dstPath); err != nil {
					return fmt.Errorf("link %s: %w", dstPath, err)
				}
				continue
			}
			if err := materializeTree(srcPath, dstPath, policy, false); err != nil {
				return err
			}
			continue
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}
		mode := info.Mode()
		if mode&os.ModeSymlink != 0 {
			target, err := os.Readlink(srcPath)
			if err != nil {
				return fmt.Errorf("readlink %s: %w", srcPath, err)
			}
			if err := os.Symlink(target, dstPath); err != nil {
				return fmt.Errorf("symlink %s: %w", dstPath, err)
			}
			continue
		}
		if !mode.IsRegular() {
			// Skip sockets, devices, named pipes — never useful in a workspace stage.
			continue
		}
		if err := materializeFile(srcPath, dstPath, info); err != nil {
			return err
		}
	}
	return nil
}

// materializeFile picks the cheapest mechanism that produces an independent
// copy at dst. Hardlinks are intentionally NOT used for the stage tree because
// any in-place edit by the job would mutate the source (defeating isolation);
// platform-specific clonefile is preferred (real COW), with byte copy as the
// safe fallback. ioCloneFile is wired in workspace_clone_*.go per platform.
func materializeFile(src, dst string, info os.FileInfo) error {
	if err := ioCloneFile(src, dst); err == nil {
		return nil
	}
	return copyFileBytes(src, dst, info.Mode().Perm())
}

func copyFileBytes(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		_ = os.Remove(dst)
		return err
	}
	return out.Close()
}

var unsafeJobIDChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safeJobID(id string) string {
	cleaned := unsafeJobIDChars.ReplaceAllString(id, "_")
	cleaned = strings.Trim(cleaned, "._-")
	if cleaned == "" {
		return "job"
	}
	if len(cleaned) > 120 {
		cleaned = cleaned[:120]
	}
	return cleaned
}

// ensureOrunGitignore adds a .orun/runs/ ignore line to .gitignore if the
// repo has one and it's not already there. Idempotent. We deliberately don't
// create .gitignore if missing — that would surprise users with a brand-new
// file appearing.
func ensureOrunGitignore(repoRoot string) error {
	path := filepath.Join(repoRoot, ".gitignore")
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	const marker = ".orun/runs/"
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == marker || strings.TrimSpace(line) == "/.orun/runs/" {
			return nil
		}
	}
	suffix := ""
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		suffix = "\n"
	}
	appended := suffix + "\n# orun per-job staged workspaces\n" + marker + "\n"
	return os.WriteFile(path, append(data, []byte(appended)...), 0o644)
}
