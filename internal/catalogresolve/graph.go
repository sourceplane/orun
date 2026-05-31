package catalogresolve

import (
	"sort"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// Graph kind labels for GraphNode.Kind, per data-model.md §4 vocabulary.
const (
	graphKindComponent = "Component"
	graphKindSystem    = "System"
	graphKindAPI       = "API"
	graphKindResource  = "Resource"
	graphKindOwner     = "Owner"
)

// Edge type vocabulary, per data-model.md §3 (component dep relationships)
// and §4 (graph siblings). The dependency graph reuses the §3 relationship
// values verbatim ({calls, depends-on, deploy-after, links-to}).
const (
	graphEdgePartOf   = "part-of"
	graphEdgeProvides = "provides"
	graphEdgeConsumes = "consumes"
	graphEdgeUses     = "uses"
	graphEdgeOwns     = "owns"
)

// buildGraphs constructs the five CatalogGraph siblings (dependencies,
// systems, apis, resources, owners) from the resolved manifest set per
// data-model.md §4. Each graph is independently sorted: nodes by `key`,
// edges by `(from, to, type, optional)`. Returned slice order is fixed:
// [dependencies, systems, apis, resources, owners] — this is the order
// `catalogHash` consumes per identity-and-keys.md §9.
//
// sourceSnapshotKey and catalogSnapshotKey are stamped on every graph.
// Pass empty strings if the catalogSnapshotKey is not yet known (caller
// back-fills after computing catalogHash).
func buildGraphs(manifests []*catalogmodel.ComponentManifest, sourceSnapshotKey, catalogSnapshotKey string) []*catalogmodel.CatalogGraph {
	deps := newGraph(sourceSnapshotKey, catalogSnapshotKey)
	sys := newGraph(sourceSnapshotKey, catalogSnapshotKey)
	apis := newGraph(sourceSnapshotKey, catalogSnapshotKey)
	res := newGraph(sourceSnapshotKey, catalogSnapshotKey)
	own := newGraph(sourceSnapshotKey, catalogSnapshotKey)

	// Track distinct nodes per graph. The component itself is a node in
	// every graph that names it on either side of an edge.
	depNodes := map[string]catalogmodel.GraphNode{}
	sysNodes := map[string]catalogmodel.GraphNode{}
	apiNodes := map[string]catalogmodel.GraphNode{}
	resNodes := map[string]catalogmodel.GraphNode{}
	ownNodes := map[string]catalogmodel.GraphNode{}

	for _, m := range manifests {
		ckey := m.Identity.ComponentKey
		cnode := catalogmodel.GraphNode{Key: ckey, Kind: graphKindComponent, Name: m.Identity.Name}

		// Dependencies graph — every component is a node; edges are the
		// resolved dependency entries (relationship verbatim).
		depNodes[ckey] = cnode
		for _, d := range m.Spec.Dependencies.Components {
			// Dest may be unresolved (left as authored short ref or fq form);
			// we still add it as a node and edge so the graph is complete.
			depNodes[d.Key] = catalogmodel.GraphNode{Key: d.Key, Kind: graphKindComponent, Name: d.Name}
			deps.Edges = append(deps.Edges, catalogmodel.GraphEdge{
				From: ckey, To: d.Key, Type: d.Relationship, Optional: d.Optional,
			})
		}

		// Systems graph — node for the component plus a node for the
		// system; edge component → system with type=part-of. Components
		// with no system membership are still nodes (isolated).
		sysNodes[ckey] = cnode
		if s := m.Spec.System; s != "" {
			sysNodes[s] = catalogmodel.GraphNode{Key: s, Kind: graphKindSystem, Name: s}
			sys.Edges = append(sys.Edges, catalogmodel.GraphEdge{
				From: ckey, To: s, Type: graphEdgePartOf,
			})
		}

		// APIs graph — node for the component plus a node per API; edges
		// component → api with type ∈ {provides, consumes}.
		apiNodes[ckey] = cnode
		for _, api := range m.Spec.Dependencies.APIs.Provides {
			apiNodes[api] = catalogmodel.GraphNode{Key: api, Kind: graphKindAPI, Name: api}
			apis.Edges = append(apis.Edges, catalogmodel.GraphEdge{
				From: ckey, To: api, Type: graphEdgeProvides,
			})
		}
		for _, api := range m.Spec.Dependencies.APIs.Consumes {
			apiNodes[api] = catalogmodel.GraphNode{Key: api, Kind: graphKindAPI, Name: api}
			apis.Edges = append(apis.Edges, catalogmodel.GraphEdge{
				From: ckey, To: api, Type: graphEdgeConsumes,
			})
		}

		// Resources graph — node for the component plus a node per
		// resource; edge component → resource with type=uses.
		resNodes[ckey] = cnode
		for _, r := range m.Spec.Dependencies.Resources.Uses {
			resNodes[r] = catalogmodel.GraphNode{Key: r, Kind: graphKindResource, Name: r}
			res.Edges = append(res.Edges, catalogmodel.GraphEdge{
				From: ckey, To: r, Type: graphEdgeUses,
			})
		}

		// Owners graph — node for each owner plus the components they
		// own; edge owner → component with type=owns. Per the task
		// prompt, the edge direction is `metadata.owner → component`.
		ownNodes[ckey] = cnode
		if o := m.Metadata.Owner; o != "" {
			ownNodes[o] = catalogmodel.GraphNode{Key: o, Kind: graphKindOwner, Name: o}
			own.Edges = append(own.Edges, catalogmodel.GraphEdge{
				From: o, To: ckey, Type: graphEdgeOwns,
			})
		}
	}

	finalize(deps, depNodes)
	finalize(sys, sysNodes)
	finalize(apis, apiNodes)
	finalize(res, resNodes)
	finalize(own, ownNodes)

	return []*catalogmodel.CatalogGraph{deps, sys, apis, res, own}
}

func newGraph(sourceSnapshotKey, catalogSnapshotKey string) *catalogmodel.CatalogGraph {
	return &catalogmodel.CatalogGraph{
		APIVersion:         "orun.io/v1alpha1",
		Kind:               "CatalogGraph",
		SourceSnapshotKey:  sourceSnapshotKey,
		CatalogSnapshotKey: catalogSnapshotKey,
		Nodes:              []catalogmodel.GraphNode{},
		Edges:              []catalogmodel.GraphEdge{},
	}
}

// finalize flattens the node-set map into a sorted slice and applies the
// stable edge ordering rule: (from, to, type, optional).
func finalize(g *catalogmodel.CatalogGraph, nodes map[string]catalogmodel.GraphNode) {
	if len(nodes) > 0 {
		g.Nodes = make([]catalogmodel.GraphNode, 0, len(nodes))
		for _, n := range nodes {
			g.Nodes = append(g.Nodes, n)
		}
		sort.SliceStable(g.Nodes, func(a, b int) bool {
			return g.Nodes[a].Key < g.Nodes[b].Key
		})
	}
	if len(g.Edges) > 0 {
		sort.SliceStable(g.Edges, func(a, b int) bool {
			ea, eb := g.Edges[a], g.Edges[b]
			if ea.From != eb.From {
				return ea.From < eb.From
			}
			if ea.To != eb.To {
				return ea.To < eb.To
			}
			if ea.Type != eb.Type {
				return ea.Type < eb.Type
			}
			// false sorts before true (deterministic boolean tiebreak)
			return !ea.Optional && eb.Optional
		})
	}
}

// stampCatalogSnapshotKey back-fills the catalogSnapshotKey on every graph
// after the key has been derived from catalogHash. Idempotent.
func stampCatalogSnapshotKey(graphs []*catalogmodel.CatalogGraph, sourceSnapshotKey, catalogSnapshotKey string) {
	for _, g := range graphs {
		g.SourceSnapshotKey = sourceSnapshotKey
		g.CatalogSnapshotKey = catalogSnapshotKey
	}
}
