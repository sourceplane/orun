package affected

import (
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/objcatalog"
)

// classKind is the classification of a changed path (data-model.md §2).
type classKind int

const (
	classIgnore classKind = iota
	classComponent
	classStructural
	classGlobal
)

type classification struct {
	kind         classKind
	componentKey string // set when kind == classComponent
}

// componentEntry is the engine's per-component projection of the catalog.
type componentEntry struct {
	key     string
	name    string
	watches []string
}

// catalogIndex is the engine's precomputed, lookup-friendly view of one catalog:
// the ownership map (dir → component, plus the classification rule sets), the
// component table (key/name/watches), and the dependency adjacency.
type catalogIndex struct {
	// ownership rule sets.
	dirs                []string // component dirs, longest-first (deepest-prefix wins)
	dirToKey            map[string]string
	globalPaths         map[string]bool
	structuralFilenames map[string]bool
	ignoreDirs          map[string]bool

	components []componentEntry
	nameToKey  map[string]string

	// dependency adjacency: deps[from] = its forward deps; dependents[to] = the
	// components that depend on it. depsAlways is the forward adjacency
	// restricted to include:always edges — the plan/run selection closure.
	deps       map[string][]string
	depsAlways map[string][]string
	dependents map[string][]string
	allComps   []string
}

// index builds the catalogIndex from the detector's catalog view.
func (d *Detector) index() *catalogIndex {
	idx := &catalogIndex{
		dirToKey:            map[string]string{},
		globalPaths:         map[string]bool{},
		structuralFilenames: map[string]bool{},
		ignoreDirs:          map[string]bool{},
		nameToKey:           map[string]string{},
		deps:                map[string][]string{},
		depsAlways:          map[string][]string{},
		dependents:          map[string][]string{},
	}
	if d.catalog == nil {
		return idx
	}

	if o := d.catalog.Ownership; o != nil {
		for dir, key := range o.Components {
			idx.dirToKey[dir] = key
			idx.dirs = append(idx.dirs, dir)
		}
		for _, p := range o.GlobalPaths {
			idx.globalPaths[p] = true
		}
		for _, f := range o.StructuralFilenames {
			idx.structuralFilenames[f] = true
		}
		for _, dDir := range o.IgnoreDirs {
			idx.ignoreDirs[dDir] = true
		}
	}
	// Longest dir first so deepest-prefix wins.
	sort.Slice(idx.dirs, func(i, j int) bool {
		if len(idx.dirs[i]) != len(idx.dirs[j]) {
			return len(idx.dirs[i]) > len(idx.dirs[j])
		}
		return idx.dirs[i] > idx.dirs[j]
	})

	for _, c := range d.catalog.Components {
		idx.components = append(idx.components, componentEntry{
			key:     c.ComponentKey,
			name:    c.Name,
			watches: componentWatches(c),
		})
		if c.Name != "" {
			idx.nameToKey[c.Name] = c.ComponentKey
		}
		idx.allComps = append(idx.allComps, c.ComponentKey)
	}

	// Dependency edges live in the "dependencies" graph slice: From depends_on To.
	if g, ok := d.catalog.Graph["dependencies"]; ok {
		for _, e := range g.Edges {
			idx.deps[e.From] = append(idx.deps[e.From], e.To)
			idx.dependents[e.To] = append(idx.dependents[e.To], e.From)
			if e.Include == "always" {
				idx.depsAlways[e.From] = append(idx.depsAlways[e.From], e.To)
			}
		}
	}
	return idx
}

// includeAlwaysClosure returns the transitive forward closure of the seed set
// over include:always edges only — the dependencies a --changed plan must pull
// in, excluding the seeds themselves, sorted.
func (idx *catalogIndex) includeAlwaysClosure(seed map[string]bool) []string {
	return idx.closure(seed, idx.depsAlways)
}

// classify maps one workspace-relative path to its class (the reference
// algorithm, data-model.md §2): global → structural → ignore → component →
// ignore.
func (idx *catalogIndex) classify(path string) classification {
	if idx.globalPaths[path] {
		return classification{kind: classGlobal}
	}
	if idx.structuralFilenames[baseName(path)] {
		return classification{kind: classStructural}
	}
	for _, seg := range strings.Split(path, "/") {
		if idx.ignoreDirs[seg] {
			return classification{kind: classIgnore}
		}
	}
	if key := idx.ownerOf(path); key != "" {
		return classification{kind: classComponent, componentKey: key}
	}
	return classification{kind: classIgnore}
}

// ownerOf returns the component key owning path by longest-matching dir prefix,
// or "" if none.
func (idx *catalogIndex) ownerOf(path string) string {
	for _, dir := range idx.dirs { // longest-first
		if dir == "." || pathUnderDir(path, dir) {
			if dir == "." && strings.Contains(path, "/") {
				continue // root component only owns root-level files
			}
			return idx.dirToKey[dir]
		}
	}
	return ""
}

func (idx *catalogIndex) keyForName(name string) string { return idx.nameToKey[name] }

func (idx *catalogIndex) allKeys() []string {
	out := append([]string(nil), idx.allComps...)
	sort.Strings(out)
	return out
}

// forwardClosure returns the transitive forward dependencies of the seed set,
// excluding the seeds themselves, sorted.
func (idx *catalogIndex) forwardClosure(seed map[string]bool) []string {
	return idx.closure(seed, idx.deps)
}

// reverseClosure returns the transitive reverse dependencies (dependents) of the
// seed set, excluding the seeds themselves, sorted.
func (idx *catalogIndex) reverseClosure(seed map[string]bool) []string {
	return idx.closure(seed, idx.dependents)
}

func (idx *catalogIndex) closure(seed map[string]bool, adj map[string][]string) []string {
	visited := map[string]bool{}
	var stack []string
	for k := range seed {
		stack = append(stack, k)
	}
	for len(stack) > 0 {
		cur := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, next := range adj[cur] {
			if !visited[next] {
				visited[next] = true
				stack = append(stack, next)
			}
		}
	}
	out := make([]string, 0, len(visited))
	for k := range visited {
		if !seed[k] { // closure excludes the seeds themselves
			out = append(out, k)
		}
	}
	sort.Strings(out)
	return out
}

// componentWatches extracts the component's intent-watch sections from the
// verbatim manifest spec (spec.change.watches). Empty until the catalog carries
// watches (the intent-impact enrichment milestone); the engine reads them here
// so no further wiring is needed once present.
func componentWatches(c objcatalog.CatalogComponentView) []string {
	change, ok := c.Spec["change"].(map[string]any)
	if !ok {
		return nil
	}
	raw, ok := change["watches"].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, w := range raw {
		if s, ok := w.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func baseName(path string) string {
	if i := strings.LastIndexByte(path, '/'); i >= 0 {
		return path[i+1:]
	}
	return path
}

// pathUnderDir reports whether path is dir itself or a file/dir under dir.
func pathUnderDir(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, dir+"/")
}
