package gha

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// materializeAction returns an isolated, per-job copy of the resolved action's
// directory. Local actions (tracked in the workspace) are returned as-is. Remote
// or composite actions are materialized into the job temp dir on first use so
// that a malicious or buggy action cannot mutate the shared cache and affect
// concurrent or subsequent jobs. Files are linked by default (zero-cost) and
// fall back to a recursive copy on cross-filesystem boundaries.
func (e *Engine) materializeAction(state *jobState, resolved *resolvedAction) (string, error) {
	if resolved == nil || resolved.ActionDir == "" {
		return "", nil
	}
	if resolved.Reference.Kind == referenceKindLocal {
		return resolved.ActionDir, nil
	}

	key := resolved.ResolvedRef + "#" + resolved.Reference.Path
	if key == "#" {
		key = resolved.ActionDir
	}

	state.actionDirsMu.Lock()
	defer state.actionDirsMu.Unlock()
	if dir, ok := state.actionDirs[key]; ok {
		return dir, nil
	}

	repoSlug := sanitizePathComponent(resolved.Reference.Repository())
	if repoSlug == "" {
		repoSlug = "action"
	}
	refSlug := strings.ToLower(resolved.ResolvedRef)
	if len(refSlug) > 12 {
		refSlug = refSlug[:12]
	}
	if refSlug == "" {
		refSlug = "ref"
	}
	pathSlug := sanitizePathComponent(resolved.Reference.Path)
	if pathSlug != "" {
		repoSlug = repoSlug + "_" + pathSlug
	}

	target := filepath.Join(state.tempDir, "actions", repoSlug+"-"+refSlug)
	if err := materializeTree(resolved.ActionDir, target); err != nil {
		return "", fmt.Errorf("materialize action %s: %w", resolved.Reference.Repository(), err)
	}
	state.actionDirs[key] = target
	return target, nil
}

// materializeTree mirrors src to dst. Regular files are hardlinked when
// possible to avoid duplicating bytes; if hardlinking fails (e.g. cross-device)
// the file is copied. Directories and symlinks are recreated rather than
// shared so that a job writing into the materialized tree cannot mutate the
// shared on-disk cache.
func materializeTree(src, dst string) error {
	cleanedSrc := filepath.Clean(src)
	if err := os.MkdirAll(filepath.Dir(dst), 0755); err != nil {
		return err
	}
	if _, err := os.Stat(dst); err == nil {
		return nil
	}
	tmp := dst + ".tmp"
	_ = os.RemoveAll(tmp)
	if err := os.MkdirAll(tmp, 0755); err != nil {
		return err
	}
	walkErr := filepath.Walk(cleanedSrc, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(cleanedSrc, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(tmp, info.Mode().Perm()|0700)
		}
		target := filepath.Join(tmp, rel)
		mode := info.Mode()
		switch {
		case mode&os.ModeSymlink != 0:
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			return os.Symlink(linkTarget, target)
		case mode.IsDir():
			return os.MkdirAll(target, mode.Perm()|0700)
		case mode.IsRegular():
			if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
				return err
			}
			if err := os.Link(path, target); err == nil {
				return nil
			}
			return copyFile(path, target, mode.Perm())
		default:
			return nil
		}
	})
	if walkErr != nil {
		_ = os.RemoveAll(tmp)
		return walkErr
	}
	if err := os.Rename(tmp, dst); err != nil {
		_ = os.RemoveAll(tmp)
		if _, statErr := os.Stat(dst); statErr == nil {
			return nil
		}
		return err
	}
	return nil
}

func copyFile(src, dst string, perm os.FileMode) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, perm)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		_ = out.Close()
		return err
	}
	return out.Close()
}
