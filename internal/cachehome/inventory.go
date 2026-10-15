package cachehome

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/fslock"
)

// Kind names one of the caches orun keeps (specs/orun-cache-efficiency CE5).
type Kind string

const (
	// KindActions is a remote action at one commit: ~/.orun/actions/<owner>/<repo>/<sha>.
	KindActions Kind = "actions"
	// KindToolCache is one version of a tool a setup action installed:
	// <tool-cache>/<tool>/<version>.
	KindToolCache Kind = "tool-cache"
	// KindShared is a store every job shares: ~/.orun/actions/shared/<store>
	// (npm, pnpm-store, terraform-plugins, go-build, …).
	KindShared Kind = "shared"
	// KindDeclared is a cache a composition step declared: ~/.orun/actions/shared/caches/<name>.
	KindDeclared Kind = "declared"
	// KindCompositions is a composition package at one digest:
	// ~/.orun/cache/compositions/<digest>.
	KindCompositions Kind = "compositions"
	// KindRuns is a run's staging directory in a checkout: .orun/runs/<exec-id>.
	KindRuns Kind = "runs"
	// KindResolveMemo is one entry of a checkout's resolve memo:
	// .orun/objectmodel/cache/resolve/<key>.
	KindResolveMemo Kind = "resolve-memo"
)

// Kinds lists every kind, in the order `orun cache ls` prints them.
var Kinds = []Kind{KindActions, KindToolCache, KindShared, KindDeclared, KindCompositions, KindRuns, KindResolveMemo}

// MachineWide reports whether entries of the kind live under the cache home
// (and count against `orun cache gc`'s budget) rather than in a checkout.
func (k Kind) MachineWide() bool {
	return k != KindRuns && k != KindResolveMemo
}

// Entry is one cache entry: a directory (or, for the resolve memo, a file)
// that can be removed on its own.
type Entry struct {
	Kind     Kind      `json:"kind"`
	Name     string    `json:"name"`
	Path     string    `json:"path"`
	Size     int64     `json:"size"`
	LastUsed time.Time `json:"lastUsed"`
	// InUse is set when a live lock shows an orun process holding the
	// entry; Remove skips it.
	InUse bool `json:"inUse,omitempty"`
}

// Compositions is where composition packages pulled from an archive or an
// OCI registry are cached, one directory per digest.
func Compositions() string { return filepath.Join(Root(), "cache", "compositions") }

// lastUsedSuffix names the stamp beside an entry that orun touches on a
// hit, so an entry's last use is known without relying on atime.
const lastUsedSuffix = ".last-used"

// Touch records that the entry at path was used now. Errors are ignored:
// the stamp is advisory, and an entry without one is aged by its mtime.
func Touch(path string) {
	stamp := path + lastUsedSuffix
	now := time.Now()
	if err := os.Chtimes(stamp, now, now); err == nil {
		return
	}
	f, err := os.OpenFile(stamp, os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_ = f.Close()
}

// LastUsed is when the entry at path was last used: its stamp's time when
// orun has touched it, else the newer of the path's own modification time
// and that of the file it was created with.
func LastUsed(path string) time.Time {
	var t time.Time
	if info, err := os.Stat(path + lastUsedSuffix); err == nil {
		t = info.ModTime()
	}
	if info, err := os.Lstat(path); err == nil && info.ModTime().After(t) {
		t = info.ModTime()
	}
	return t
}

// Inventory lists every cache entry on the machine and, when checkout is
// not empty, the per-checkout ones under its .orun directory. A kind whose
// root does not exist contributes nothing.
func Inventory(checkout string) ([]Entry, error) {
	var entries []Entry
	var errs []error
	add := func(kind Kind, name, path string, inUse bool) {
		size, err := dirSize(path)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s %s: %w", kind, name, err))
		}
		entries = append(entries, Entry{Kind: kind, Name: name, Path: path, Size: size, LastUsed: LastUsed(path), InUse: inUse})
	}

	// Actions: any directory under the actions root, outside shared/, that
	// holds a .ready marker; a live fetch lock (CE0's, refreshed while the
	// download runs) means a process is filling it.
	actionsRoot := Actions()
	_ = filepath.WalkDir(actionsRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil || !d.IsDir() {
			return nil
		}
		if path == filepath.Join(actionsRoot, "shared") || d.Name() == "refs" {
			return filepath.SkipDir
		}
		if path == actionsRoot {
			return nil
		}
		if _, err := os.Stat(filepath.Join(path, ".ready")); err != nil {
			return nil
		}
		rel, _ := filepath.Rel(actionsRoot, path)
		add(KindActions, filepath.ToSlash(rel), path, lockFileLive(path+".lock", time.Minute))
		return filepath.SkipDir
	})

	// Tool cache: <tool>/<version>; an installer holds the cache's setup
	// lock, and while one does every version counts as in use.
	toolRoot := ToolCache()
	installing := flockHeld(filepath.Join(toolRoot, ".orun-setup.lock"))
	for _, tool := range subdirs(toolRoot) {
		for _, version := range subdirs(filepath.Join(toolRoot, tool)) {
			add(KindToolCache, tool+"/"+version, filepath.Join(toolRoot, tool, version), installing)
		}
	}

	// Shared stores, less the declared caches (listed on their own); a
	// Terraform plugin cache slot is in use while a job holds its lock.
	for _, store := range subdirs(Shared()) {
		if store == "caches" {
			continue
		}
		path := filepath.Join(Shared(), store)
		add(KindShared, store, path, strings.HasPrefix(store, "terraform-plugins") && flockHeld(path+".lock"))
	}
	for _, name := range subdirs(Declared()) {
		if strings.Contains(name, ".tmp-") || strings.Contains(name, ".old-") {
			continue
		}
		path := filepath.Join(Declared(), name)
		add(KindDeclared, name, path, flockHeld(path+".lock"))
	}
	for _, digest := range subdirs(Compositions()) {
		if strings.Contains(digest, "-tmp-") {
			continue
		}
		path := filepath.Join(Compositions(), digest)
		add(KindCompositions, digest, path, flockHeld(path+".lock"))
	}

	if checkout != "" {
		runs := filepath.Join(checkout, ".orun", "runs")
		for _, exec := range subdirs(runs) {
			add(KindRuns, exec, filepath.Join(runs, exec), false)
		}
		memo := filepath.Join(checkout, ".orun", "objectmodel", "cache", "resolve")
		if items, err := os.ReadDir(memo); err == nil {
			for _, item := range items {
				if item.IsDir() || strings.HasSuffix(item.Name(), ".tmp") {
					continue
				}
				add(KindResolveMemo, item.Name(), filepath.Join(memo, item.Name()), false)
			}
		}
	}

	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Kind != entries[j].Kind {
			return kindIndex(entries[i].Kind) < kindIndex(entries[j].Kind)
		}
		return entries[i].LastUsed.Before(entries[j].LastUsed)
	})
	return entries, errors.Join(errs...)
}

// OlderThan selects the entries last used before now-age.
func OlderThan(entries []Entry, age time.Duration, now time.Time) []Entry {
	cutoff := now.Add(-age)
	var out []Entry
	for _, e := range entries {
		if e.LastUsed.Before(cutoff) {
			out = append(out, e)
		}
	}
	return out
}

// OverBudget selects, least recently used first, the machine-wide entries
// to remove so that the rest fit under maxSize. Entries in use are never
// selected and still count against the budget.
func OverBudget(entries []Entry, maxSize int64) []Entry {
	var total int64
	var candidates []Entry
	for _, e := range entries {
		if !e.Kind.MachineWide() {
			continue
		}
		total += e.Size
		if !e.InUse {
			candidates = append(candidates, e)
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return candidates[i].LastUsed.Before(candidates[j].LastUsed) })
	var out []Entry
	for _, e := range candidates {
		if total <= maxSize {
			break
		}
		out = append(out, e)
		total -= e.Size
	}
	return out
}

// Remove deletes the entry, taking its lock first where one guards it; an
// entry another process holds is left alone with an error.
func Remove(e Entry) error {
	if e.InUse {
		return fmt.Errorf("%s %s is in use", e.Kind, e.Name)
	}
	switch e.Kind {
	case KindDeclared, KindCompositions:
		lock, ok, err := fslock.TryAcquire(e.Path + ".lock")
		if err == nil && !ok {
			return fmt.Errorf("%s %s is in use", e.Kind, e.Name)
		}
		if err == nil {
			defer lock.Release()
		}
	case KindShared:
		if strings.HasPrefix(e.Name, "terraform-plugins") {
			lock, ok, err := fslock.TryAcquire(e.Path + ".lock")
			if err == nil && !ok {
				return fmt.Errorf("%s %s is in use", e.Kind, e.Name)
			}
			if err == nil {
				defer lock.Release()
			}
		}
	}
	if err := os.RemoveAll(e.Path); err != nil {
		return err
	}
	_ = os.Remove(e.Path + lastUsedSuffix)
	return nil
}

// Total sums the entries' sizes.
func Total(entries []Entry) int64 {
	var n int64
	for _, e := range entries {
		n += e.Size
	}
	return n
}

func kindIndex(k Kind) int {
	for i, known := range Kinds {
		if known == k {
			return i
		}
	}
	return len(Kinds)
}

func subdirs(root string) []string {
	items, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var names []string
	for _, item := range items {
		if item.IsDir() && !strings.HasPrefix(item.Name(), ".") {
			names = append(names, item.Name())
		}
	}
	return names
}

func dirSize(root string) (int64, error) {
	var size int64
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if path == root {
				return err
			}
			return nil
		}
		if d.Type().IsRegular() {
			if info, err := d.Info(); err == nil {
				size += info.Size()
			}
		}
		return nil
	})
	return size, err
}

// lockFileLive reports whether a lock file of the action cache's kind (a
// file its holder refreshes while working) was touched within freshFor.
func lockFileLive(path string, freshFor time.Duration) bool {
	info, err := os.Stat(path)
	return err == nil && time.Since(info.ModTime()) < freshFor
}

// flockHeld reports whether another process holds the advisory lock at path
// (which exists only once something took it).
func flockHeld(path string) bool {
	if _, err := os.Stat(path); err != nil {
		return false
	}
	lock, ok, err := fslock.TryAcquire(path)
	if err != nil {
		return false
	}
	if !ok {
		return true
	}
	_ = lock.Release()
	return false
}
