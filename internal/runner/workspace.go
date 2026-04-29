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
	jobID   string
	keep    bool
	cleanup func()
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

// regenerable trees are skipped during staging — they're recreated by the job
// itself and copying them defeats the purpose (they're the largest and most
// volatile contents). Same approach the reverted da3ea7b used.
var skipDirs = map[string]struct{}{
	"node_modules": {},
	".next":        {},
	".nuxt":        {},
	".turbo":       {},
	".svelte-kit":  {},
	"dist":         {},
	"build":        {},
	"target":       {},
	".venv":        {},
	"venv":         {},
	"__pycache__":  {},
	".terraform":   {},
	".pytest_cache":{},
	".mypy_cache":  {},
}

// stageJobWorkspace creates an isolated working copy of sourceDir under
// <sourceDir>/.orun/runs/<execID>/<safeJobID>/work and returns the path. The
// directory layout intentionally lives inside the source repo (per user choice)
// so .git operations and tool discovery (workspace-relative configs, package.json
// hoisting, etc.) keep working. We auto-add .orun/runs/ to .gitignore so the
// staged trees never leak into commits.
func stageJobWorkspace(sourceDir, execID, jobID string, keep bool) (*stagedWorkspace, error) {
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

	if err := materializeTree(src, root); err != nil {
		_ = os.RemoveAll(root)
		return nil, fmt.Errorf("stage workspace: %w", err)
	}

	return &stagedWorkspace{
		root:  root,
		jobID: jobID,
		keep:  keep,
		cleanup: func() {
			// Remove the per-job parent (work/'s grandparent stays for sibling jobs;
			// the per-job dir is owned by us alone).
			_ = os.RemoveAll(filepath.Dir(root))
			// Best-effort: drop the per-exec dir if it became empty.
			_ = os.Remove(filepath.Dir(filepath.Dir(root)))
		},
	}, nil
}

// materializeTree walks src and recreates it under dst. It tries the fastest
// available cloning primitive (clonefile/FICLONE on supported filesystems),
// falls back to hardlink for plain files, and finally to byte copy. The skip
// set above is honored at every directory level. The .orun directory itself
// is skipped to avoid recursive staging.
func materializeTree(src, dst string) error {
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
			if _, skip := skipDirs[name]; skip {
				continue
			}
			if err := materializeTree(srcPath, dstPath); err != nil {
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
