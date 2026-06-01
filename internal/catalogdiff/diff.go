package catalogdiff

import (
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// Snapshot is the comparison input: the resolved component set plus the
// relationship graphs for one catalog. Callers build this from a persisted
// CatalogSnapshot (its enumerated manifests + the per-kind graph files); the
// diff engine never touches the store.
//
// Graphs is keyed by the bare graph kind ("dependencies", "systems", "apis",
// "resources", "owners"). A kind absent from the map is treated as an empty
// graph — an absent graph on one side and an empty graph on the other produce
// no spurious changes.
type Snapshot struct {
	Components []catalogmodel.ComponentManifest
	Graphs     map[string]catalogmodel.CatalogGraph
}

// Result is the full diff between a base and a head Snapshot. Every slice is
// sorted deterministically; an empty diff has all-empty (non-nil) slices.
type Result struct {
	Changed      []ComponentChange `json:"changed"`
	Added        []ComponentRef    `json:"added"`
	Removed      []ComponentRef    `json:"removed"`
	GraphChanges []GraphChange     `json:"graphChanges"`
}

// ComponentRef identifies a component by its stable key and bare name. Used
// for the added/removed lists where no field-level detail applies.
type ComponentRef struct {
	ComponentKey string `json:"componentKey"`
	Name         string `json:"name"`
}

// ComponentChange is one component present in both snapshots whose resolved
// fields differ. Fields is the sorted list of changed field paths.
type ComponentChange struct {
	ComponentKey string        `json:"componentKey"`
	Name         string        `json:"name"`
	Fields       []FieldChange `json:"fields"`
}

// FieldChange is one differing field path with its base and head values
// rendered as canonical strings. For set-shaped fields the values are the
// sorted-set rendering; for list-shaped fields they preserve order.
type FieldChange struct {
	Path string `json:"path"`
	Base string `json:"base"`
	Head string `json:"head"`
}

// GraphChange is one node or edge added to or removed from a relationship
// graph between base and head. Graph is the bare graph kind. Change is one of
// "node-added", "node-removed", "edge-added", "edge-removed". Node changes
// carry Key/Name; edge changes carry From/To/Type.
type GraphChange struct {
	Graph  string `json:"graph"`
	Change string `json:"change"`
	Key    string `json:"key,omitempty"`
	Name   string `json:"name,omitempty"`
	From   string `json:"from,omitempty"`
	To     string `json:"to,omitempty"`
	Type   string `json:"type,omitempty"`
}

const (
	changeNodeAdded   = "node-added"
	changeNodeRemoved = "node-removed"
	changeEdgeAdded   = "edge-added"
	changeEdgeRemoved = "edge-removed"
)

// graphKinds is the closed set of relationship graphs diffed, in a fixed order
// so output ordering is stable regardless of map iteration.
var graphKinds = []string{"dependencies", "systems", "apis", "resources", "owners"}

// Diff compares base against head and returns the deterministic Result. The
// engine is pure: same inputs always yield the same output, byte-for-byte.
func Diff(base, head Snapshot) Result {
	res := Result{
		Changed:      []ComponentChange{},
		Added:        []ComponentRef{},
		Removed:      []ComponentRef{},
		GraphChanges: []GraphChange{},
	}

	baseByKey := indexManifests(base.Components)
	headByKey := indexManifests(head.Components)

	// Added / removed / changed by componentKey.
	for key, hm := range headByKey {
		if _, ok := baseByKey[key]; !ok {
			res.Added = append(res.Added, ComponentRef{ComponentKey: key, Name: hm.Identity.Name})
		}
	}
	for key, bm := range baseByKey {
		hm, ok := headByKey[key]
		if !ok {
			res.Removed = append(res.Removed, ComponentRef{ComponentKey: key, Name: bm.Identity.Name})
			continue
		}
		if fields := diffComponent(bm, hm); len(fields) > 0 {
			res.Changed = append(res.Changed, ComponentChange{
				ComponentKey: key,
				Name:         hm.Identity.Name,
				Fields:       fields,
			})
		}
	}

	if gc := diffGraphs(base.Graphs, head.Graphs); gc != nil {
		res.GraphChanges = gc
	}

	sortResult(&res)
	return res
}

// indexManifests keys a manifest slice by its stable componentKey. On a
// duplicate key (which a well-formed catalog never has) the first wins, keeping
// the diff deterministic rather than order-dependent.
func indexManifests(ms []catalogmodel.ComponentManifest) map[string]catalogmodel.ComponentManifest {
	out := make(map[string]catalogmodel.ComponentManifest, len(ms))
	for _, m := range ms {
		key := m.Identity.ComponentKey
		if _, ok := out[key]; !ok {
			out[key] = m
		}
	}
	return out
}

// fieldKind selects the comparison rule for a field value.
type fieldKind int

const (
	scalarField fieldKind = iota // compared as-is
	setField                     // order-insensitive (sorted before compare)
	listField                    // order-sensitive
)

// fieldExtractor names one comparable field of a manifest and how to read and
// compare it.
type fieldExtractor struct {
	path string
	kind fieldKind
	read func(m catalogmodel.ComponentManifest) []string
}

// componentFields is the closed, ordered set of resolved fields the diff
// compares. Scalars are single-element slices; set/list fields carry their
// members. The set-shaped fields (tags, providesApis, consumesApis) are
// compared order-insensitively; dependsOn is order-sensitive (§6).
var componentFields = []fieldExtractor{
	{"metadata.title", scalarField, func(m catalogmodel.ComponentManifest) []string { return scalar(m.Metadata.Title) }},
	{"metadata.description", scalarField, func(m catalogmodel.ComponentManifest) []string { return scalar(m.Metadata.Description) }},
	{"metadata.owner", scalarField, func(m catalogmodel.ComponentManifest) []string { return scalar(m.Metadata.Owner) }},
	{"metadata.tags", setField, func(m catalogmodel.ComponentManifest) []string { return m.Metadata.Tags }},
	{"spec.type", scalarField, func(m catalogmodel.ComponentManifest) []string { return scalar(m.Spec.Type) }},
	{"spec.lifecycle", scalarField, func(m catalogmodel.ComponentManifest) []string { return scalar(m.Spec.Lifecycle) }},
	{"spec.system", scalarField, func(m catalogmodel.ComponentManifest) []string { return scalar(m.Spec.System) }},
	{"spec.domain", scalarField, func(m catalogmodel.ComponentManifest) []string { return scalar(m.Spec.Domain) }},
	{"spec.tier", scalarField, func(m catalogmodel.ComponentManifest) []string { return scalar(m.Spec.Tier) }},
	{"spec.dependsOn", listField, func(m catalogmodel.ComponentManifest) []string { return dependsOn(m) }},
	{"spec.providesApis", setField, func(m catalogmodel.ComponentManifest) []string { return m.Spec.Dependencies.APIs.Provides }},
	{"spec.consumesApis", setField, func(m catalogmodel.ComponentManifest) []string { return m.Spec.Dependencies.APIs.Consumes }},
}

// diffComponent compares the closed field set of two manifests for the same
// componentKey and returns the changed paths, sorted by path.
func diffComponent(base, head catalogmodel.ComponentManifest) []FieldChange {
	var changes []FieldChange
	for _, f := range componentFields {
		bv := renderField(f.kind, f.read(base))
		hv := renderField(f.kind, f.read(head))
		if bv != hv {
			changes = append(changes, FieldChange{Path: f.path, Base: bv, Head: hv})
		}
	}
	sort.SliceStable(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes
}

// renderField canonicalizes a field value to the comparable/display string. A
// set field is sorted (order-insensitive); a list field keeps order; a scalar
// is taken verbatim. Multi-valued fields render as "[a, b, c]".
func renderField(kind fieldKind, vals []string) string {
	switch kind {
	case scalarField:
		if len(vals) == 0 {
			return ""
		}
		return vals[0]
	case setField:
		s := append([]string(nil), vals...)
		sort.Strings(s)
		return "[" + strings.Join(s, ", ") + "]"
	default: // listField
		return "[" + strings.Join(vals, ", ") + "]"
	}
}

// scalar wraps a single value as the one-element slice the extractor contract
// expects. An empty string renders as the empty field.
func scalar(v string) []string { return []string{v} }

// dependsOn renders the component → component dependency edges as an
// order-sensitive list of "name[relationship]" tokens. Order is preserved as
// the resolver emitted it so a reordering is itself a diff (§6).
func dependsOn(m catalogmodel.ComponentManifest) []string {
	deps := m.Spec.Dependencies.Components
	out := make([]string, 0, len(deps))
	for _, d := range deps {
		out = append(out, d.Name+"["+d.Relationship+"]")
	}
	return out
}

// diffGraphs compares each relationship graph kind between base and head and
// returns the union of node/edge add/remove changes, unsorted (sortResult
// orders them).
func diffGraphs(base, head map[string]catalogmodel.CatalogGraph) []GraphChange {
	var changes []GraphChange
	for _, kind := range graphKinds {
		bg := base[kind]
		hg := head[kind]
		changes = append(changes, diffGraph(kind, bg, hg)...)
	}
	return changes
}

// diffGraph compares one graph kind: nodes by Key, edges by (From,To,Type).
func diffGraph(kind string, base, head catalogmodel.CatalogGraph) []GraphChange {
	var changes []GraphChange

	baseNodes := indexNodes(base.Nodes)
	headNodes := indexNodes(head.Nodes)
	for key, n := range headNodes {
		if _, ok := baseNodes[key]; !ok {
			changes = append(changes, GraphChange{Graph: kind, Change: changeNodeAdded, Key: key, Name: n.Name})
		}
	}
	for key, n := range baseNodes {
		if _, ok := headNodes[key]; !ok {
			changes = append(changes, GraphChange{Graph: kind, Change: changeNodeRemoved, Key: key, Name: n.Name})
		}
	}

	baseEdges := indexEdges(base.Edges)
	headEdges := indexEdges(head.Edges)
	for ek, e := range headEdges {
		if _, ok := baseEdges[ek]; !ok {
			changes = append(changes, GraphChange{Graph: kind, Change: changeEdgeAdded, From: e.From, To: e.To, Type: e.Type})
		}
	}
	for ek, e := range baseEdges {
		if _, ok := headEdges[ek]; !ok {
			changes = append(changes, GraphChange{Graph: kind, Change: changeEdgeRemoved, From: e.From, To: e.To, Type: e.Type})
		}
	}
	return changes
}

func indexNodes(ns []catalogmodel.GraphNode) map[string]catalogmodel.GraphNode {
	out := make(map[string]catalogmodel.GraphNode, len(ns))
	for _, n := range ns {
		if _, ok := out[n.Key]; !ok {
			out[n.Key] = n
		}
	}
	return out
}

// edgeKey is the identity of an edge for set membership: direction + type. The
// Optional flag is intentionally excluded from identity so a calls→depends-on
// retype shows as one add + one remove rather than a silent flip.
func edgeKey(e catalogmodel.GraphEdge) string {
	return e.From + "\x00" + e.To + "\x00" + e.Type
}

func indexEdges(es []catalogmodel.GraphEdge) map[string]catalogmodel.GraphEdge {
	out := make(map[string]catalogmodel.GraphEdge, len(es))
	for _, e := range es {
		ek := edgeKey(e)
		if _, ok := out[ek]; !ok {
			out[ek] = e
		}
	}
	return out
}

// sortResult imposes the total order that makes the Result deterministic.
func sortResult(r *Result) {
	sort.SliceStable(r.Changed, func(i, j int) bool { return r.Changed[i].ComponentKey < r.Changed[j].ComponentKey })
	sort.SliceStable(r.Added, func(i, j int) bool { return r.Added[i].ComponentKey < r.Added[j].ComponentKey })
	sort.SliceStable(r.Removed, func(i, j int) bool { return r.Removed[i].ComponentKey < r.Removed[j].ComponentKey })
	sort.SliceStable(r.GraphChanges, func(i, j int) bool { return graphChangeLess(r.GraphChanges[i], r.GraphChanges[j]) })
}

// graphChangeLess is the total order on graph changes: graph kind, then change
// kind, then the identity tuple (node key / edge from,to,type).
func graphChangeLess(a, b GraphChange) bool {
	if a.Graph != b.Graph {
		return a.Graph < b.Graph
	}
	if a.Change != b.Change {
		return a.Change < b.Change
	}
	if a.Key != b.Key {
		return a.Key < b.Key
	}
	if a.From != b.From {
		return a.From < b.From
	}
	if a.To != b.To {
		return a.To < b.To
	}
	return a.Type < b.Type
}

// IsEmpty reports whether the diff found no differences at all.
func (r Result) IsEmpty() bool {
	return len(r.Changed) == 0 && len(r.Added) == 0 && len(r.Removed) == 0 && len(r.GraphChanges) == 0
}

// FilterComponent narrows the Result to a single component by key or bare name:
// changed/added/removed entries for that component and graph changes whose node
// or edge endpoints reference it. Used by `orun catalog diff <component>`.
//
// match is applied against both the componentKey and the bare name so either
// selector form works. The returned Result is a fresh value; the receiver is
// untouched.
func (r Result) FilterComponent(match string) Result {
	out := Result{
		Changed:      []ComponentChange{},
		Added:        []ComponentRef{},
		Removed:      []ComponentRef{},
		GraphChanges: []GraphChange{},
	}
	refMatches := func(key, name string) bool { return key == match || name == match }

	for _, c := range r.Changed {
		if refMatches(c.ComponentKey, c.Name) {
			out.Changed = append(out.Changed, c)
		}
	}
	for _, a := range r.Added {
		if refMatches(a.ComponentKey, a.Name) {
			out.Added = append(out.Added, a)
		}
	}
	for _, rm := range r.Removed {
		if refMatches(rm.ComponentKey, rm.Name) {
			out.Removed = append(out.Removed, rm)
		}
	}
	for _, g := range r.GraphChanges {
		if graphChangeTouches(g, match) {
			out.GraphChanges = append(out.GraphChanges, g)
		}
	}
	return out
}

// graphChangeTouches reports whether a graph change references match on any of
// its identity fields (node key/name or edge endpoints). Endpoint keys are
// 3-segment componentKeys, so a bare-name match also checks the trailing
// segment.
func graphChangeTouches(g GraphChange, match string) bool {
	if g.Key == match || g.Name == match {
		return true
	}
	for _, ep := range []string{g.From, g.To} {
		if ep == match {
			return true
		}
		if i := strings.LastIndex(ep, "/"); i >= 0 && ep[i+1:] == match {
			return true
		}
	}
	return false
}
