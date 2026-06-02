package objplan

import (
	"encoding/json"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/nodes"
)

// graphKinds is the canonical edge-kind order the resolver emits graphs in
// (catalog_hash.go: [dependencies, systems, apis, resources, owners]). The
// catalogmodel.CatalogGraph type carries no edge-kind field — it is positional —
// so the adapter assigns kinds by index.
var graphKinds = []string{"dependencies", "systems", "apis", "resources", "owners"}

// BuildCatalogNodes maps a resolved catalogresolve.CatalogView into the
// object-model node types: a CatalogSnapshot, its ComponentManifests, and its
// CatalogGraphs. resolverVersion stamps the snapshot (it participates in the
// resolve memo key, not in catalog identity).
func BuildCatalogNodes(view *catalogresolve.CatalogView, resolverVersion int) (nodes.CatalogSnapshot, []nodes.ComponentManifest, []nodes.CatalogGraph) {
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
	return cat, manifests, graphs
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
