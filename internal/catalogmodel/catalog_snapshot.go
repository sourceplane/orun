package catalogmodel

// CatalogSnapshot is the persisted record summarizing a resolved catalog
// (the set of ComponentManifests + graphs) for a given SourceSnapshot. See
// data-model.md §2.
//
// Stored at:
//
//	sources/<sourceSnapshotKey>/catalogs/<catalogSnapshotKey>/catalog.json
//
// Immutable after first write.
type CatalogSnapshot struct {
	APIVersion         string                  `json:"apiVersion"`
	Kind               string                  `json:"kind"`
	CatalogSnapshotKey string                  `json:"catalogSnapshotKey"`
	CatalogSnapshotID  string                  `json:"catalogSnapshotId"`
	SourceSnapshotKey  string                  `json:"sourceSnapshotKey"`
	Repo               string                  `json:"repo"`
	SourceScope        string                  `json:"sourceScope"`
	HeadRevision       string                  `json:"headRevision"`
	TreeHash           string                  `json:"treeHash"`
	WorkingTree        string                  `json:"workingTree"`
	Authoritative      bool                    `json:"authoritative"`
	Preview            bool                    `json:"preview"`
	Resolver           CatalogResolver         `json:"resolver"`
	CatalogHash        string                  `json:"catalogHash"`
	Summary            CatalogSummary          `json:"summary"`
	Objects            CatalogObjects          `json:"objects"`
	CreatedAt          string                  `json:"createdAt"`
}

// CatalogResolver records the resolver provenance — the tool versions and
// stack composition sources that produced this catalog. Used by T-IDK-1 to
// keep `catalogHash` reproducible across runs.
type CatalogResolver struct {
	OrunVersion     string   `json:"orunVersion"`
	SchemaVersion   string   `json:"schemaVersion"`
	ResolverVersion int      `json:"resolverVersion"`
	StackSources    []string `json:"stackSources"`
}

// CatalogSummary is the headline counts surfaced to `orun catalog list` and
// the cockpit. Pure denormalization — recomputable from the manifests.
type CatalogSummary struct {
	Components int `json:"components"`
	Systems    int `json:"systems"`
	APIs       int `json:"apis"`
	Resources  int `json:"resources"`
	Owners     int `json:"owners"`
	Domains    int `json:"domains"`
}

// CatalogObjects is the inventory of objects that compose this catalog. The
// resolver writes one ManifestRef per ComponentManifest it persisted.
type CatalogObjects struct {
	Components []ManifestRef `json:"components"`
}

// ManifestRef is the `objects.components[i]` shape — name, on-disk relative
// path, and the manifest's content hash for tamper detection.
type ManifestRef struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	ManifestHash string `json:"manifestHash"`
}
