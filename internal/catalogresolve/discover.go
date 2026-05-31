package catalogresolve

import (
	"context"
	"io/fs"
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

// discover walks workspaceRoot and returns the workspace-relative,
// slash-separated paths of every authored component manifest. Both
// component.yaml and component.yml are accepted; a directory containing
// both forms is a typed validation error.
//
// extraExcludes is appended to defaultExcludes before the walk; entries
// are matched as exact directory basenames at any depth (i.e. a value of
// "fixtures" prunes every directory whose basename is "fixtures",
// regardless of where it sits in the tree). Empty entries are ignored.
//
// The returned slice is lexically sorted for determinism.
//
// Caller is responsible for ensuring workspaceRoot is an existing
// directory; DiscoverAndLoad does this Stat+IsDir check before
// dispatching here.
func discover(ctx context.Context, workspaceRoot string, extraExcludes []string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	excludes := buildExcludeSet(extraExcludes)

	// Two passes: collect every (dir, basename) hit, then resolve
	// .yaml/.yml collisions per dir before flattening to a sorted list.
	hitsByDir := map[string][]string{} // dir → sorted basenames found there

	walkErr := filepath.WalkDir(workspaceRoot, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if cancelErr := ctx.Err(); cancelErr != nil {
			return cancelErr
		}
		if d.IsDir() {
			// Always descend into the workspace root itself.
			if path == workspaceRoot {
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
		dir := filepath.Dir(path)
		hitsByDir[dir] = append(hitsByDir[dir], name)
		return nil
	})
	if walkErr != nil {
		return nil, walkErr
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
