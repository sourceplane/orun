package objplan

import (
	"encoding/json"
	"path"
	"sort"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/nodes"
)

// ownershipSchemaVersion is the on-disk ImpactOwnership schema this build emits.
// Bump on any shape change (data-model.md §2).
const ownershipSchemaVersion = 1

// catalogGlobalBlocks are the intent.yaml blocks a change-detection consumer
// treats as catalog-relevant (data-model.md §2). Fixed for schemaVersion 1.
var catalogGlobalBlocks = []string{
	"catalog.defaults",
	"catalog.inference",
	"catalog.discovery",
	"metadata.namespace",
	"metadata.repo",
}

// structuralFilenames are the manifest basenames whose add/remove/edit is a
// structural change (mirrors catalogresolve discovery).
var structuralFilenames = []string{"component.yaml", "component.yml"}

// graphKinds is the canonical edge-kind order the resolver emits graphs in
// (catalog_hash.go: [dependencies, systems, apis, resources, owners]). The
// catalogmodel.CatalogGraph type carries no edge-kind field — it is positional —
// so the adapter assigns kinds by index.
var graphKinds = []string{"dependencies", "systems", "apis", "resources", "owners"}

// BuildCatalogNodes maps a resolved catalogresolve.CatalogView into the
// object-model node types: a CatalogSnapshot, its ComponentManifests, its
// CatalogGraphs, and the change-detection ImpactOwnership map. resolverVersion
// stamps the snapshot (it participates in the resolve memo key, not in catalog
// identity).
func BuildCatalogNodes(view *catalogresolve.CatalogView, resolverVersion int) (nodes.CatalogSnapshot, []nodes.ComponentManifest, []nodes.CatalogGraph, nodes.ImpactOwnership) {
	cat := nodes.CatalogSnapshot{
		Kind:            nodes.KindCatalogSnapshot,
		ResolverVersion: resolverVersion,
	}
	if view != nil && view.Snapshot != nil {
		cat.HumanKey = view.Snapshot.CatalogSnapshotKey
	}

	var manifests []nodes.ComponentManifest
	if view != nil {
		for _, cm := range view.Manifests {
			if cm == nil {
				continue
			}
			manifests = append(manifests, mapManifest(cm))
		}
	}

	var graphs []nodes.CatalogGraph
	if view != nil {
		for i, g := range view.Graphs {
			if g == nil {
				continue
			}
			edgeKind := "graph" + itoa(i)
			if i < len(graphKinds) {
				edgeKind = graphKinds[i]
			}
			graphs = append(graphs, mapGraph(g, edgeKind))
		}
	}
	return cat, manifests, graphs, buildOwnership(view)
}

// buildOwnership derives the change-detection ownership map from the resolved
// view: each component's directory (the dirname of its component.yaml) maps to
// its componentKey, plus the fixed classification rules. Deterministic — no
// timestamps, sorted arrays — so it folds stably into the catalog Merkle root.
func buildOwnership(view *catalogresolve.CatalogView) nodes.ImpactOwnership {
	o := nodes.ImpactOwnership{
		Kind:                nodes.KindImpactOwnership,
		SchemaVersion:       ownershipSchemaVersion,
		GlobalBlocks:        append([]string(nil), catalogGlobalBlocks...),
		StructuralFilenames: append([]string(nil), structuralFilenames...),
	}

	intentPath := "intent.yaml"
	var excludes []string
	if view != nil && view.ResolvedCatalog != nil {
		if view.IntentPath != "" {
			intentPath = view.IntentPath
		}
		excludes = view.Excludes
	}
	o.GlobalPaths = []string{intentPath}

	o.IgnoreDirs = append([]string(nil), excludes...)
	if o.IgnoreDirs == nil {
		o.IgnoreDirs = catalogresolve.DefaultExcludes()
	}
	sort.Strings(o.IgnoreDirs)

	components := map[string]string{}
	if view != nil && view.ResolvedCatalog != nil {
		for _, cm := range view.Manifests {
			if cm == nil || cm.Identity.Path == "" {
				continue // synthetic root / unknown path: no ownership entry
			}
			dir := path.Dir(cm.Identity.Path)
			components[dir] = cm.Identity.ComponentKey
		}
	}
	if len(components) > 0 {
		o.Components = components
	}
	return o
}

// mapManifest converts a catalogmodel.ComponentManifest to the node form. The
// stable identity fields are mapped explicitly; the deep metadata/spec/runtime
// blocks are carried verbatim into the node's generic maps so no resolved
// information is lost.
func mapManifest(cm *catalogmodel.ComponentManifest) nodes.ComponentManifest {
	m := nodes.ComponentManifest{
		Kind: nodes.KindComponentManifest,
		Identity: nodes.ComponentIdentity{
			ComponentKey: cm.Identity.ComponentKey,
			Name:         cm.Identity.Name,
			Namespace:    cm.Identity.Namespace,
			Repo:         cm.Identity.Repo,
			Path:         cm.Identity.Path,
		},
		Metadata:   toMap(cm.Metadata),
		Spec:       toMap(cm.Spec),
		Provenance: toMap(cm.Resolution),
	}
	if t, ok := m.Spec["type"].(string); ok {
		m.Type = t
	}
	return m
}

func mapGraph(g *catalogmodel.CatalogGraph, edgeKind string) nodes.CatalogGraph {
	out := nodes.CatalogGraph{Kind: nodes.KindCatalogGraph, EdgeKind: edgeKind}
	for _, n := range g.Nodes {
		out.Nodes = append(out.Nodes, nodes.GraphNode{Key: n.Key, Kind: n.Kind, Name: n.Name})
	}
	for _, e := range g.Edges {
		out.Edges = append(out.Edges, nodes.GraphEdge{From: e.From, To: e.To, Type: e.Type})
	}
	return out
}

// toMap round-trips a value through JSON into a generic map so the node record
// carries the resolver's nested data canonically. Returns nil for an empty
// result so the field is omitted.
func toMap(v any) map[string]any {
	b, err := json.Marshal(v)
	if err != nil {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil || len(m) == 0 {
		return nil
	}
	return m
}
