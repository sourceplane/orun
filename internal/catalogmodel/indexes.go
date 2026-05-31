package catalogmodel

// ComponentGlobalIndex is the cross-source view of a single component. See
// data-model.md §9.1.
//
// Stored at: indexes/components/<sanitizedComponentKey>.json (slashes in the
// 3-segment componentKey become single dashes via SanitizeComponentKey).
type ComponentGlobalIndex struct {
	APIVersion   string                  `json:"apiVersion"`
	Kind         string                  `json:"kind"`
	ComponentKey string                  `json:"componentKey"`
	Name         string                  `json:"name"`
	Repo         string                  `json:"repo"`
	Latest       ComponentIndexLocation  `json:"latest"`
	Main         ComponentIndexLocation  `json:"main"`
	Previews     []ComponentIndexPreview `json:"previews"`
}

// ComponentIndexLocation locates a manifest under the source/catalog
// directory tree. `ManifestPath` is optional (set on `main` for fast lookup,
// omitted on `latest`).
type ComponentIndexLocation struct {
	SourceSnapshotKey  string `json:"sourceSnapshotKey"`
	CatalogSnapshotKey string `json:"catalogSnapshotKey"`
	ManifestPath       string `json:"manifestPath,omitempty"`
}

// ComponentIndexPreview adds the source scope tag (e.g. pr-139) to a
// per-source location so previews can be enumerated by name.
type ComponentIndexPreview struct {
	SourceScope        string `json:"sourceScope"`
	SourceSnapshotKey  string `json:"sourceSnapshotKey"`
	CatalogSnapshotKey string `json:"catalogSnapshotKey"`
}

// ComponentExecutionIndex is the catalog-local execution-history index for a
// component. See data-model.md §9.2.
//
// Stored at:
//
//	sources/<sourceSnapshotKey>/catalogs/<catalogSnapshotKey>/indexes/components/<componentName>.json
type ComponentExecutionIndex struct {
	APIVersion         string                   `json:"apiVersion"`
	Kind               string                   `json:"kind"`
	ComponentKey       string                   `json:"componentKey"`
	SourceSnapshotKey  string                   `json:"sourceSnapshotKey"`
	CatalogSnapshotKey string                   `json:"catalogSnapshotKey"`
	Executions         []ComponentExecutionRow  `json:"executions"`
}

// ComponentExecutionRow is one entry in a ComponentExecutionIndex —
// denormalized from the per-execution metadata for fast listing.
type ComponentExecutionRow struct {
	RevisionKey  string `json:"revisionKey"`
	ExecutionKey string `json:"executionKey"`
	TriggerName  string `json:"triggerName"`
	Profile      string `json:"profile"`
	Environment  string `json:"environment"`
	Status       string `json:"status"`
	CreatedAt    string `json:"createdAt"`
}
