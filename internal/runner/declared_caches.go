package runner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/sourceplane/orun/internal/cachehome"
	"github.com/sourceplane/orun/internal/fslock"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/ui"
)

// Declared caches (specs/orun-cache-efficiency CE3). A composition step may
// declare `caches:` — directories under the job's working directory that a
// tool fills (a Next.js build cache, a Playwright download) and that would
// otherwise be rebuilt by every job: the per-job tree is staged fresh and the
// per-job HOME is thrown away. orun keeps each named cache on the machine,
// restores it into the tree before the job's first step runs there, and
// saves what the job left behind once the job succeeded. A copy on each
// side, never a shared directory: two jobs filling the same cache at once
// stay apart, and a save swaps the machine's copy in whole.

// declaredCaches is the job's caches by name, the first declaration of a
// name winning.
func declaredCaches(job model.PlanJob) []model.CacheDecl {
	seen := map[string]bool{}
	var out []model.CacheDecl
	for _, step := range job.Steps {
		for _, c := range step.Caches {
			if c.Name == "" || seen[c.Name] {
				continue
			}
			seen[c.Name] = true
			out = append(out, c)
		}
	}
	return out
}

func (r *Runner) declaredCacheRoot() string {
	if r.DeclaredCacheRoot != "" {
		return r.DeclaredCacheRoot
	}
	return cachehome.Declared()
}

// restoreDeclaredCaches fills each declared cache path from the machine's
// copy when the tree has none of its own.
func (r *Runner) restoreDeclaredCaches(job model.PlanJob, workDir string) {
	for _, c := range declaredCaches(job) {
		src := filepath.Join(r.declaredCacheRoot(), c.Name)
		dst := filepath.Join(workDir, filepath.FromSlash(c.Path))
		if info, err := os.Stat(src); err != nil || !info.IsDir() {
			continue // nothing saved yet
		}
		if _, err := os.Stat(dst); err == nil {
			continue // the tree brought its own
		}
		if err := copyDirTree(src, dst); err != nil {
			fmt.Fprintf(r.Stderr, "warning: cache %s: restore into %s failed: %v\n", c.Name, c.Path, err)
			continue
		}
		cachehome.Touch(src)
		if r.Verbose {
			fmt.Fprintf(r.Stdout, "  %s cache %s restored into %s\n", ui.Dim(r.Color, "↺"), c.Name, c.Path)
		}
	}
}

// saveDeclaredCaches keeps what the job left at each declared cache path as
// the machine's copy: written beside the old copy and swapped in, under a
// lock, so a reader never sees a half-written cache and two jobs saving the
// same name take turns.
func (r *Runner) saveDeclaredCaches(ctx context.Context, job model.PlanJob, workDir string) {
	for _, c := range declaredCaches(job) {
		src := filepath.Join(workDir, filepath.FromSlash(c.Path))
		if info, err := os.Stat(src); err != nil || !info.IsDir() {
			continue // the job produced nothing to keep
		}
		dst := filepath.Join(r.declaredCacheRoot(), c.Name)
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			fmt.Fprintf(r.Stderr, "warning: cache %s: %v\n", c.Name, err)
			continue
		}
		lock, err := fslock.Acquire(ctx, dst+".lock")
		if err != nil {
			fmt.Fprintf(r.Stderr, "warning: cache %s: not saved: %v\n", c.Name, err)
			continue
		}
		err = swapInDir(src, dst)
		_ = lock.Release()
		if err != nil {
			fmt.Fprintf(r.Stderr, "warning: cache %s: not saved: %v\n", c.Name, err)
			continue
		}
		if r.Verbose {
			fmt.Fprintf(r.Stdout, "  %s cache %s saved from %s\n", ui.Dim(r.Color, "↻"), c.Name, c.Path)
		}
	}
}

// swapInDir replaces dst with a copy of src: the copy is built beside dst
// and renamed over it, so dst is always whole or absent.
func swapInDir(src, dst string) error {
	suffix := "-" + strconv.Itoa(os.Getpid())
	tmp := dst + ".tmp" + suffix
	_ = os.RemoveAll(tmp)
	if err := copyDirTree(src, tmp); err != nil {
		_ = os.RemoveAll(tmp)
		return err
	}
	old := dst + ".old" + suffix
	_ = os.RemoveAll(old)
	hadOld := false
	if _, err := os.Stat(dst); err == nil {
		if err := os.Rename(dst, old); err != nil {
			_ = os.RemoveAll(tmp)
			return err
		}
		hadOld = true
	}
	if err := os.Rename(tmp, dst); err != nil {
		if hadOld {
			_ = os.Rename(old, dst)
		}
		_ = os.RemoveAll(tmp)
		return err
	}
	_ = os.RemoveAll(old)
	return nil
}

// copyDirTree recreates src under dst: directories, symlinks as symlinks,
// regular files cloned where the filesystem allows and copied elsewhere.
func copyDirTree(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		mode := info.Mode()
		switch {
		case mode.IsDir():
			return os.MkdirAll(target, mode.Perm()|0o700)
		case mode&os.ModeSymlink != 0:
			link, err := os.Readlink(path)
			if err != nil {
				return err
			}
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return os.Symlink(link, target)
		case mode.IsRegular():
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			return materializeFile(path, target, info)
		default:
			return nil
		}
	})
}
