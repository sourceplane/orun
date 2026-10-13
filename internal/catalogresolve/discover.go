package catalogresolve

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// defaultExcludes is the dir-name skip set applied even when intent.yaml
// supplies no catalog.discovery.exclude (resolution-pipeline.md §2).
var defaultExcludes = []string{
	".git",
	".orun",
	"build",
	"dist",
	"node_modules",
	"vendor",
}

// DefaultExcludes returns a copy of the directory-basename skip set discovery
// applies even when intent.yaml supplies no catalog.discovery.exclude. Exported
// so change-detection consumers (the ownership map) can mirror the prune set.
func DefaultExcludes() []string {
	return append([]string(nil), defaultExcludes...)
}

// EffectiveExcludes returns the sorted, de-duplicated directory-basename prune
// set discovery used for a resolve: the defaults merged with the intent's
// catalog.discovery.exclude (basename entries only, matching the walk).
func EffectiveExcludes(intentExcludes []string) []string {
	set := buildExcludeSet(intentExcludes)
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// discoveryScope is where discovery walks: the absolute directories to scan
// (each inside workspaceRoot) and the intent's extra directory-basename
// excludes. Build one with discoveryScopeFor so the roots rule is applied in
// one place for every walk (DiscoverAndLoad and the manifest-set digest).
type discoveryScope struct {
	// Roots are absolute, cleaned, de-duplicated directories under the
	// workspace root. nil means the workspace root itself; an empty non-nil
	// slice means the intent declared roots and none lies in the workspace,
	// so there is nothing to scan.
	Roots []string
	// Excludes is intent.yaml's catalog.discovery.exclude (may be nil).
	Excludes []string
}

// discoveryScopeFor resolves the planner's `discovery.roots` into the catalog's
// discovery scope so the catalog and the planner agree on where component
// manifests live.
//
//   - No intent, no roots, or an intent that inherits presets (`extends:`)
//     keeps the historical whole-workspace walk. Presets may contribute roots
//     the resolver cannot see (the planner's loader merges them), and a
//     partial root set would hide components the planner does plan, so the
//     walk stays wide instead.
//   - Roots are relative to the intent file, like the planner reads them. A
//     root that resolves outside the workspace is not scanned: catalog paths
//     are workspace-relative, and such a root cannot be named that way.
//   - A root that does not exist or is not a directory is an
//     *ErrIntentInvalid, the same failure the planner's loader reports.
//
// Overlapping roots (`.` and `apps/`) are fine: the walk de-duplicates hits.
func discoveryScopeFor(workspaceRoot, intentAbs, intentRel string, intent *intentFile) (discoveryScope, error) {
	scope := discoveryScope{Excludes: intent.discoveryExcludes()}
	if intent == nil || intent.Discovery == nil || intent.hasExtends() {
		return scope, nil
	}
	intentDir := filepath.Dir(intentAbs)
	seen := map[string]struct{}{}
	for _, raw := range intent.Discovery.Roots {
		rel := strings.TrimSpace(raw)
		if rel == "" {
			continue
		}
		if scope.Roots == nil {
			// A declared root set, even one that resolves to nothing inside
			// the workspace, replaces the whole-workspace walk.
			scope.Roots = []string{}
		}
		abs := filepath.Clean(filepath.Join(intentDir, filepath.FromSlash(rel)))
		if wsRel := toRelSlash(workspaceRoot, abs); wsRel == ".." || strings.HasPrefix(wsRel, "../") || filepath.IsAbs(wsRel) {
			continue // outside the workspace: not the catalog's to scan
		}
		if _, dup := seen[abs]; dup {
			continue
		}
		info, err := os.Stat(abs)
		if err != nil {
			return discoveryScope{}, &ErrIntentInvalid{File: intentRel, Reason: fmt.Sprintf("discovery.roots entry %q: %v", rel, err)}
		}
		if !info.IsDir() {
			return discoveryScope{}, &ErrIntentInvalid{File: intentRel, Reason: fmt.Sprintf("discovery.roots entry %q is not a directory", rel)}
		}
		seen[abs] = struct{}{}
		scope.Roots = append(scope.Roots, abs)
	}
	sort.Strings(scope.Roots)
	return scope, nil
}

// discover walks the scope's roots under workspaceRoot and returns the
// workspace-relative, slash-separated paths of every authored component
// manifest. Both component.yaml and component.yml are accepted; a directory
// containing both forms is a typed validation error.
//
// scope.Excludes is appended to defaultExcludes before the walk; entries
// are matched as exact directory basenames at any depth (i.e. a value of
// "fixtures" prunes every directory whose basename is "fixtures",
// regardless of where it sits in the tree). Empty entries are ignored. A
// root directory itself is always entered, even when its basename is in
// the exclude set, so that `discovery.roots` can point at one.
//
// The returned slice is lexically sorted for determinism.
//
// Caller is responsible for ensuring workspaceRoot is an existing
// directory; DiscoverAndLoad does this Stat+IsDir check before
// dispatching here.
func discover(ctx context.Context, workspaceRoot string, scope discoveryScope) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	excludes := buildExcludeSet(scope.Excludes)
	roots := scope.Roots
	if roots == nil {
		roots = []string{workspaceRoot}
	}

	// Two passes: collect every (dir, basename) hit, then resolve
	// .yaml/.yml collisions per dir before flattening to a sorted list.
	hitsByDir := map[string][]string{} // dir → sorted basenames found there
	seenFile := map[string]struct{}{}  // overlapping roots visit a file twice

	for _, root := range roots {
		walkErr := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if cancelErr := ctx.Err(); cancelErr != nil {
				return cancelErr
			}
			if d.IsDir() {
				// Always descend into the root itself.
				if path == root {
					return nil
				}
				if _, skip := excludes[d.Name()]; skip {
					return filepath.SkipDir
				}
				return nil
			}
			name := d.Name()
			if name != "component.yaml" && name != "component.yml" {
				return nil
			}
			if _, dup := seenFile[path]; dup {
				return nil
			}
			seenFile[path] = struct{}{}
			dir := filepath.Dir(path)
			hitsByDir[dir] = append(hitsByDir[dir], name)
			return nil
		})
		if walkErr != nil {
			return nil, walkErr
		}
	}

	out := make([]string, 0, len(hitsByDir))
	for dir, names := range hitsByDir {
		sort.Strings(names)
		if len(names) > 1 {
			rels := make([]string, 0, len(names))
			for _, n := range names {
				rels = append(rels, toRelSlash(workspaceRoot, filepath.Join(dir, n)))
			}
			sort.Strings(rels)
			return nil, &ErrManifestMixedExtension{
				Dir:   toRelSlash(workspaceRoot, dir),
				Paths: rels,
			}
		}
		out = append(out, toRelSlash(workspaceRoot, filepath.Join(dir, names[0])))
	}
	sort.Strings(out)
	return out, nil
}

// buildExcludeSet merges the default and user-supplied exclude lists,
// trimming whitespace and dropping empty entries. Returned map is keyed
// by exact dir basename.
func buildExcludeSet(extra []string) map[string]struct{} {
	set := make(map[string]struct{}, len(defaultExcludes)+len(extra))
	for _, e := range defaultExcludes {
		set[e] = struct{}{}
	}
	for _, e := range extra {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		// Strip a single leading or trailing slash so that values like
		// "node_modules/" still match the directory basename.
		e = strings.Trim(e, "/")
		if e == "" || strings.ContainsRune(e, '/') {
			// Path-style globs aren't supported in this PR; defer to
			// the C2 second PR if needed. Skipping is safer than
			// silently mis-matching.
			continue
		}
		set[e] = struct{}{}
	}
	return set
}

// toRelSlash returns path made relative to root, with OS separators
// normalised to forward slashes. root is assumed absolute.
func toRelSlash(root, path string) string {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return filepath.ToSlash(path)
	}
	return filepath.ToSlash(rel)
}

// (no extra helpers)
