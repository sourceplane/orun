package gha

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/bmatcuk/doublestar/v4"
	"github.com/sourceplane/orun/internal/cachehome"
	"github.com/sourceplane/orun/internal/fslock"
)

// Native actions/cache (specs/orun-cache-efficiency CE7). GitHub's cache
// service is not reachable from orun (ACTIONS_RUNTIME_URL points nowhere, so
// the real action warned and restored nothing); `actions/cache`,
// `actions/cache/restore` and `actions/cache/save` are run by the engine
// itself instead, against a store under the shared cache directory, so a
// composition written for GitHub restores its caches under orun on any
// runner. The inputs and outputs are the action's: `key`, `path` (one per
// line, globs and `~` allowed, relative to the workspace), `restore-keys`
// (prefixes, in order; the newest entry wins), `fail-on-cache-miss`,
// `lookup-only`; `cache-hit` (an exact match of the primary key),
// `cache-primary-key`, `cache-matched-key`. The main action saves in its
// post step when the primary key was not an exact hit; a save never replaces
// an entry another process saved under the same key.

// actionsCacheStoreName is the store's directory under the shared cache.
const actionsCacheStoreName = "actions-cache"

// nativeCacheKind reports whether a reference names actions/cache or one of
// its sub-actions, and which: "main", "restore" or "save".
func nativeCacheKind(ref ActionReference) (string, bool) {
	if ref.Kind != referenceKindRemote || ref.Owner != "actions" || ref.Repo != "cache" {
		return "", false
	}
	switch strings.Trim(strings.TrimSpace(ref.Path), "/") {
	case "":
		return "main", true
	case "restore":
		return "restore", true
	case "save":
		return "save", true
	}
	return "", false
}

type cacheInputs struct {
	key         string
	paths       []string
	restoreKeys []string
	lookupOnly  bool
	failOnMiss  bool
}

func parseCacheInputs(with map[string]string, kind string) (cacheInputs, error) {
	in := cacheInputs{key: strings.TrimSpace(with["key"])}
	if in.key == "" {
		return in, fmt.Errorf("actions/cache: input 'key' is required")
	}
	in.paths = splitLines(with["path"])
	if len(in.paths) == 0 {
		return in, fmt.Errorf("actions/cache: input 'path' is required")
	}
	in.restoreKeys = splitLines(with["restore-keys"])
	in.lookupOnly = isTrue(with["lookup-only"])
	in.failOnMiss = isTrue(with["fail-on-cache-miss"])
	if kind == "save" {
		in.restoreKeys = nil
	}
	return in, nil
}

func splitLines(value string) []string {
	var out []string
	for _, line := range strings.Split(strings.ReplaceAll(value, "\r\n", "\n"), "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}

func isTrue(value string) bool {
	return strings.EqualFold(strings.TrimSpace(value), "true")
}

// cacheEntryMeta is the record beside a saved entry's tree.
type cacheEntryMeta struct {
	Key     string    `json:"key"`
	Paths   []string  `json:"paths"`
	SavedAt time.Time `json:"savedAt"`
}

func (e *Engine) actionsCacheStore() string {
	return filepath.Join(e.cacheDir, "shared", actionsCacheStoreName)
}

func cacheEntryDir(store, key string) string {
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(store, hex.EncodeToString(sum[:16]))
}

func readCacheEntryMeta(dir string) (cacheEntryMeta, bool) {
	data, err := os.ReadFile(filepath.Join(dir, "meta.json"))
	if err != nil {
		return cacheEntryMeta{}, false
	}
	var meta cacheEntryMeta
	if json.Unmarshal(data, &meta) != nil || meta.Key == "" {
		return cacheEntryMeta{}, false
	}
	return meta, true
}

// lookupCacheEntry finds the entry for the primary key, else the newest
// entry whose key starts with the first restore key that matches any.
func lookupCacheEntry(store, key string, restoreKeys []string) (dir string, meta cacheEntryMeta, exact bool) {
	if m, ok := readCacheEntryMeta(cacheEntryDir(store, key)); ok {
		return cacheEntryDir(store, key), m, true
	}
	if len(restoreKeys) == 0 {
		return "", cacheEntryMeta{}, false
	}
	items, err := os.ReadDir(store)
	if err != nil {
		return "", cacheEntryMeta{}, false
	}
	type candidate struct {
		dir  string
		meta cacheEntryMeta
	}
	var all []candidate
	for _, item := range items {
		if !item.IsDir() || strings.Contains(item.Name(), "-tmp-") {
			continue
		}
		d := filepath.Join(store, item.Name())
		if m, ok := readCacheEntryMeta(d); ok {
			all = append(all, candidate{dir: d, meta: m})
		}
	}
	for _, prefix := range restoreKeys {
		var best *candidate
		for i := range all {
			if !strings.HasPrefix(all[i].meta.Key, prefix) {
				continue
			}
			if best == nil || all[i].meta.SavedAt.After(best.meta.SavedAt) {
				best = &all[i]
			}
		}
		if best != nil {
			return best.dir, best.meta, false
		}
	}
	return "", cacheEntryMeta{}, false
}

// cachePathTarget is one path the action caches: where it is on disk and
// where it lives in an entry's tree (ws/<relative> inside the workspace,
// abs/<path> elsewhere).
type cachePathTarget struct {
	disk string
	tree string
}

// resolveCachePaths expands the action's path lines against the workspace:
// `~` is the job's home, a relative path is under the workspace, a glob
// (doublestar) matches files and directories, a `!` line excludes.
func resolveCachePaths(patterns []string, workspaceDir, homeDir string) ([]cachePathTarget, error) {
	seen := map[string]struct{}{}
	var matched []string
	var excluded []string
	for _, pattern := range patterns {
		negate := strings.HasPrefix(pattern, "!")
		pattern = strings.TrimPrefix(pattern, "!")
		if pattern == "~" || strings.HasPrefix(pattern, "~/") {
			pattern = filepath.Join(homeDir, strings.TrimPrefix(pattern, "~"))
		}
		if !filepath.IsAbs(pattern) {
			pattern = filepath.Join(workspaceDir, pattern)
		}
		var hits []string
		if strings.ContainsAny(pattern, "*?[{") {
			found, err := doublestar.FilepathGlob(pattern)
			if err != nil {
				return nil, fmt.Errorf("actions/cache: path %q: %w", pattern, err)
			}
			hits = found
		} else if _, err := os.Lstat(pattern); err == nil {
			hits = []string{pattern}
		}
		if negate {
			excluded = append(excluded, hits...)
			continue
		}
		for _, hit := range hits {
			clean := filepath.Clean(hit)
			if _, dup := seen[clean]; dup {
				continue
			}
			seen[clean] = struct{}{}
			matched = append(matched, clean)
		}
	}
	for _, ex := range excluded {
		delete(seen, filepath.Clean(ex))
	}
	var targets []cachePathTarget
	for _, path := range matched {
		if _, keep := seen[path]; !keep {
			continue
		}
		targets = append(targets, cachePathTarget{disk: path, tree: cacheTreePath(path, workspaceDir)})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].disk < targets[j].disk })
	return targets, nil
}

func cacheTreePath(path, workspaceDir string) string {
	if rel, err := filepath.Rel(workspaceDir, path); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return filepath.Join("ws", rel)
	}
	vol := filepath.VolumeName(path)
	return filepath.Join("abs", strings.TrimPrefix(strings.TrimPrefix(path, vol), string(filepath.Separator)))
}

// cacheTreeTarget is the disk path an entry's tree path restores to.
func cacheTreeTarget(treePath, workspaceDir string) string {
	if rel, ok := strings.CutPrefix(filepath.ToSlash(treePath), "ws/"); ok {
		return filepath.Join(workspaceDir, filepath.FromSlash(rel))
	}
	if rel, ok := strings.CutPrefix(filepath.ToSlash(treePath), "abs/"); ok {
		return string(filepath.Separator) + filepath.FromSlash(rel)
	}
	return ""
}

// saveCacheEntry writes the matched paths under a new entry for key: built
// beside the store under a lock every orun process respects and renamed into
// place; an entry saved by another process under the same key is kept.
func saveCacheEntry(store, key string, targets []cachePathTarget) (saved bool, err error) {
	if err := os.MkdirAll(store, 0o755); err != nil {
		return false, err
	}
	entry := cacheEntryDir(store, key)
	if _, ok := readCacheEntryMeta(entry); ok {
		return false, nil
	}
	lock, err := fslock.Acquire(firstNonEmptyContext(nil), entry+".lock")
	if err != nil {
		return false, fmt.Errorf("actions/cache: lock %s: %w", entry+".lock", err)
	}
	defer lock.Release()
	if _, ok := readCacheEntryMeta(entry); ok {
		return false, nil
	}
	tmp, err := os.MkdirTemp(store, filepath.Base(entry)+"-tmp-")
	if err != nil {
		return false, err
	}
	defer os.RemoveAll(tmp)
	meta := cacheEntryMeta{Key: key, SavedAt: time.Now().UTC()}
	for _, t := range targets {
		if err := copyPath(t.disk, filepath.Join(tmp, "tree", t.tree)); err != nil {
			return false, fmt.Errorf("actions/cache: save %s: %w", t.disk, err)
		}
		meta.Paths = append(meta.Paths, filepath.ToSlash(t.tree))
	}
	data, err := json.MarshalIndent(meta, "", "  ")
	if err != nil {
		return false, err
	}
	if err := os.WriteFile(filepath.Join(tmp, "meta.json"), data, 0o644); err != nil {
		return false, err
	}
	_ = os.RemoveAll(entry) // a leftover without meta.json, never a live entry (checked under the lock)
	if err := os.Rename(tmp, entry); err != nil {
		return false, err
	}
	cachehome.Touch(entry)
	return true, nil
}

// restoreCacheEntry copies the entry's tree back to where it came from.
func restoreCacheEntry(dir string, meta cacheEntryMeta, workspaceDir string) error {
	for _, treePath := range meta.Paths {
		target := cacheTreeTarget(treePath, workspaceDir)
		if target == "" {
			continue
		}
		if err := copyPath(filepath.Join(dir, "tree", filepath.FromSlash(treePath)), target); err != nil {
			return fmt.Errorf("actions/cache: restore %s: %w", target, err)
		}
	}
	cachehome.Touch(dir)
	return nil
}

// copyPath copies a file, a symlink or a directory tree from src to dst,
// over what is there.
func copyPath(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	switch {
	case info.Mode()&fs.ModeSymlink != 0:
		target, err := os.Readlink(src)
		if err != nil {
			return err
		}
		_ = os.RemoveAll(dst)
		return os.Symlink(target, dst)
	case info.IsDir():
		return filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			rel, _ := filepath.Rel(src, path)
			out := filepath.Join(dst, rel)
			if d.IsDir() {
				return os.MkdirAll(out, 0o755)
			}
			if d.Type()&fs.ModeSymlink != 0 {
				target, err := os.Readlink(path)
				if err != nil {
					return err
				}
				_ = os.Remove(out)
				return os.Symlink(target, out)
			}
			fi, err := d.Info()
			if err != nil {
				return err
			}
			return copyFile(path, out, fi.Mode().Perm())
		})
	default:
		return copyFile(src, dst, info.Mode().Perm())
	}
}

// executeNativeCacheAction runs actions/cache, actions/cache/restore or
// actions/cache/save in the engine.
func (e *Engine) executeNativeCacheAction(ctx ExecContext, state *jobState, scope *scope, spec stepSpec, inheritedEnv map[string]string, ref ActionReference, kind string) (stepExecutionResult, error) {
	actionName := state.nextActionName("cache")
	githubContext := state.githubForScope(scope, actionName)
	env := mergeStringMaps(copyStringMap(state.globalEnv), inheritedEnv, githubEnv(githubContext))
	evaluator := state.evaluator(scope, env, githubContext)
	stepEnv := interpolateInterfaceMap(spec.Env, evaluator)
	env = mergeStringMaps(env, stepEnv)
	evaluator = state.evaluator(scope, env, githubContext)
	in, err := parseCacheInputs(interpolateInterfaceMap(spec.With, evaluator), kind)
	if err != nil {
		return stepExecutionResult{}, err
	}
	store := e.actionsCacheStore()
	workspaceDir := firstNonEmpty(state.workspaceDir, scope.workDir)
	homeDir := firstNonEmpty(env["HOME"], env["USERPROFILE"])
	var lines []string

	if kind == "save" {
		targets, err := resolveCachePaths(in.paths, workspaceDir, homeDir)
		if err != nil {
			return stepExecutionResult{}, err
		}
		return stepExecutionResult{Output: strings.Join(saveCachePaths(store, in.key, targets), "\n")}, nil
	}

	dir, meta, exact := lookupCacheEntry(store, in.key, in.restoreKeys)
	outputs := map[string]string{
		"cache-hit":         fmt.Sprintf("%t", exact),
		"cache-primary-key": in.key,
		"cache-matched-key": meta.Key,
	}
	switch {
	case dir == "":
		if in.failOnMiss {
			return stepExecutionResult{Outputs: outputs}, fmt.Errorf("actions/cache: failed to restore cache entry; a cache with key %q was not found (fail-on-cache-miss)", in.key)
		}
		lines = append(lines, fmt.Sprintf("Cache not found for input keys: %s", strings.Join(append([]string{in.key}, in.restoreKeys...), ", ")))
	case in.lookupOnly:
		lines = append(lines, fmt.Sprintf("Cache found and can be restored from key: %s", meta.Key))
	default:
		if err := restoreCacheEntry(dir, meta, workspaceDir); err != nil {
			return stepExecutionResult{Outputs: outputs}, err
		}
		lines = append(lines, fmt.Sprintf("Cache restored from key: %s", meta.Key))
	}

	if kind == "main" && !exact {
		// The post step saves what the job built under the primary key.
		key, paths := in.key, in.paths
		invocation := &actionInvocation{
			stepID:      specStepID(spec),
			displayName: firstNonEmpty(strings.TrimSpace(spec.Name), spec.Use),
			actionName:  actionName,
			reference:   ref,
			metadata:    &ActionMetadata{Name: "actions/cache", Runs: ActionRuns{Using: "orun-native", PostIf: "success()"}},
			inputs:      map[string]interface{}{},
			state:       map[string]string{},
			workDir:     scope.workDir,
		}
		state.posts = append(state.posts, &postAction{ifExpression: "success()", scope: scope, invocation: invocation, native: func(ExecContext) (string, error) {
			targets, err := resolveCachePaths(paths, workspaceDir, homeDir)
			if err != nil {
				return "", err
			}
			return strings.Join(saveCachePaths(store, key, targets), "\n"), nil
		}})
	}
	return stepExecutionResult{Output: strings.Join(lines, "\n"), Outputs: outputs}, nil
}

// saveCachePaths saves the targets under key and reports what happened in
// the action's own words.
func saveCachePaths(store, key string, targets []cachePathTarget) []string {
	if len(targets) == 0 {
		return []string{"Warning: Path Validation Error: Path(s) specified in the action for caching do(es) not exist, hence no cache is being saved."}
	}
	saved, err := saveCacheEntry(store, key, targets)
	switch {
	case err != nil:
		return []string{"Warning: " + err.Error()}
	case !saved:
		return []string{fmt.Sprintf("Cache already exists for key: %s, not saving cache.", key)}
	default:
		return []string{fmt.Sprintf("Cache saved with key: %s", key)}
	}
}
