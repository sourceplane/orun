package catalogmodel

// CatalogGraph is one of the relationship-graph siblings the resolver writes
// alongside a CatalogSnapshot (dependencies, systems, apis, resources,
// owners). See data-model.md §4.
//
// Stored at:
//
//	sources/<sourceSnapshotKey>/catalogs/<catalogSnapshotKey>/graph/<name>.json
//
// Immutable after first write.
type CatalogGraph struct {
	APIVersion         string      `json:"apiVersion"`
	Kind               string      `json:"kind"`
	SourceSnapshotKey  string      `json:"sourceSnapshotKey"`
	CatalogSnapshotKey string      `json:"catalogSnapshotKey"`
	Nodes              []GraphNode `json:"nodes"`
	Edges              []GraphEdge `json:"edges"`
}

// GraphNode is a single node in a CatalogGraph. `Kind` is one of {Component,
// System, API, Resource, Owner}.
type GraphNode struct {
	Key  string `json:"key"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

// GraphEdge is a directed edge between two GraphNodes. `Type` vocabulary is
// graph-specific (e.g. {calls, depends-on, deploy-after, links-to} for the
// dependency graph; {provides, consumes} for the API graph).
type GraphEdge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Type     string `json:"type"`
	Optional bool   `json:"optional"`
	// Include carries the change-detection plan-selection mode on dependency
	// edges: "always" when the edge pulls its target into a --changed plan;
	// omitted otherwise (the "if-selected" default). (orun-catalog-state CS5.)
	Include string `json:"include,omitempty"`
	// Input marks a build-input dependency edge: change detection rescopes
	// the From component when To changes.
	Input bool `json:"input,omitempty"`
}

// Graph kind names per data-model.md §4 (separate file per relationship).
const (
	GraphFileDependencies = "dependencies.json"
	GraphFileSystems      = "systems.json"
	GraphFileAPIs         = "apis.json"
	GraphFileResources    = "resources.json"
	GraphFileOwners       = "owners.json"
)
