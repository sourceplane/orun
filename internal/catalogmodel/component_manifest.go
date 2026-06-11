package catalogmodel

// ComponentManifest is the resolved per-component record persisted under a
// CatalogSnapshot. See data-model.md §3.
//
// Stored at:
//
//	sources/<sourceSnapshotKey>/catalogs/<catalogSnapshotKey>/components/<name>/manifest.json
//
// Immutable after first write. Status mutations live in the per-catalog
// component-execution-index, not here.
type ComponentManifest struct {
	APIVersion string              `json:"apiVersion"`
	Kind       string              `json:"kind"`
	Identity   ComponentIdentity   `json:"identity"`
	Source     ComponentSource     `json:"source"`
	Metadata   ComponentMetadata   `json:"metadata"`
	Spec       ComponentSpec       `json:"spec"`
	Runtime    ComponentRuntime    `json:"runtime"`
	Resolution ComponentResolution `json:"resolution"`
	// Integrations / Links / Docs / Extensions are the catalog-hub blocks
	// (orun-service-catalog SC6): typed join keys, external links, docs
	// pointers, and namespaced x-<vendor> extensions preserved verbatim.
	Integrations map[string]any  `json:"integrations,omitempty"`
	Links        []ComponentLink `json:"links,omitempty"`
	Docs         *ComponentDocs  `json:"docs,omitempty"`
	Extensions   map[string]any  `json:"extensions,omitempty"`
}

// ComponentLink is one external link (data-model.md §2).
type ComponentLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Icon  string `json:"icon,omitempty"`
}

// ComponentDocs points at techdocs/runbooks/ADRs (data-model.md §2).
type ComponentDocs struct {
	TechDocs string   `json:"techdocs,omitempty"`
	Runbooks []string `json:"runbooks,omitempty"`
	ADRs     []string `json:"adrs,omitempty"`
}

// ComponentIdentity is the stable cross-source identity of a component. The
// `componentKey` is the 3-segment <namespace>/<repo>/<name> string per
// identity-and-keys.md §4.
type ComponentIdentity struct {
	ComponentID  string `json:"componentId"`
	ComponentKey string `json:"componentKey"`
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Repo         string `json:"repo"`
	Path         string `json:"path"`
	SourceFile   string `json:"sourceFile"`
}

// ComponentSource ties a manifest back to the SourceSnapshot/CatalogSnapshot
// it was resolved against and records the canonical manifestHash.
type ComponentSource struct {
	SourceSnapshotKey  string `json:"sourceSnapshotKey"`
	CatalogSnapshotKey string `json:"catalogSnapshotKey"`
	Ref                string `json:"ref"`
	Branch             string `json:"branch"`
	HeadRevision       string `json:"headRevision"`
	TreeHash           string `json:"treeHash"`
	WorkingTree        string `json:"workingTree"`
	ManifestHash       string `json:"manifestHash"`
}

// ComponentMetadata mirrors the `metadata` block of the resolved manifest.
type ComponentMetadata struct {
	Title       string            `json:"title"`
	Description string            `json:"description"`
	Owner       string            `json:"owner"`
	Maintainers []string          `json:"maintainers"`
	Contacts    map[string]string `json:"contacts"`
	Labels      map[string]string `json:"labels"`
	Tags        []string          `json:"tags"`
	Annotations map[string]string `json:"annotations"`
}

// ComponentSpec mirrors the `spec` block of the resolved manifest.
type ComponentSpec struct {
	Type         string                          `json:"type"`
	Lifecycle    string                          `json:"lifecycle"`
	System       string                          `json:"system"`
	Domain       string                          `json:"domain"`
	Tier         string                          `json:"tier"`
	Composition  CompositionRef                  `json:"composition"`
	Parameters   map[string]string               `json:"parameters"`
	Environments map[string]ComponentEnvironment `json:"environments"`
	Dependencies ComponentDependencies           `json:"dependencies"`
	Change       *ComponentChange                `json:"change,omitempty"`
}

// ComponentChange carries the resolved change-detection "watch" sections (the
// intent signals this component reacts to). Optional/pointer so a component
// without watches leaves the manifest hash unchanged.
type ComponentChange struct {
	Watches []string `json:"watches,omitempty"`
}

// CompositionRef points at the stack-tectonic composition that backs this
// component's deploys, with the resolved version/digest/lifecycle of the
// golden path (orun-service-catalog SC7, data-model.md §5).
type CompositionRef struct {
	Source    string `json:"source"`
	Type      string `json:"type,omitempty"`
	Version   string `json:"version,omitempty"`
	Digest    string `json:"digest,omitempty"`
	Lifecycle string `json:"lifecycle,omitempty"`
}

// ComponentEnvironment is the per-environment spec entry: which profile fires
// and whether the env is currently active.
type ComponentEnvironment struct {
	Profile string `json:"profile"`
	Active  bool   `json:"active"`
}

// ComponentDependencies lists the cross-component, API, and resource edges
// the resolver discovered for this component.
type ComponentDependencies struct {
	Components []ComponentDependency `json:"components"`
	APIs       APIDependencies       `json:"apis"`
	Resources  ResourceDependencies  `json:"resources"`
}

// ComponentDependency is a single component → component edge. `Relationship`
// is one of {calls, depends-on, deploy-after, links-to}.
type ComponentDependency struct {
	Key          string `json:"key"`
	Name         string `json:"name"`
	Relationship string `json:"relationship"`
	Optional     bool   `json:"optional"`
	// Include is the resolved change-detection plan-selection mode ("always" or
	// the omitted "if-selected" default). (orun-catalog-state CS5.)
	Include string `json:"include,omitempty"`
}

// APIDependencies splits the API edges into provided and consumed.
type APIDependencies struct {
	Provides []string `json:"provides"`
	Consumes []string `json:"consumes"`
}

// ResourceDependencies records the typed resources this component uses.
type ResourceDependencies struct {
	Uses []string `json:"uses"`
}

// ComponentRuntime captures inferred runtime traits and the on-disk files
// the inference layer drew them from.
type ComponentRuntime struct {
	Inferred ComponentInferred `json:"inferred"`
	Files    ComponentFiles    `json:"files"`
}

// ComponentInferred is the inference-layer output. Each slice is
// deterministically sorted by the resolver before write.
type ComponentInferred struct {
	Languages       []string `json:"languages"`
	PackageManagers []string `json:"packageManagers"`
	Frameworks      []string `json:"frameworks"`
	Infra           []string `json:"infra"`
}

// ComponentFiles records the on-disk file paths the inferrer scanned. Use a
// pointer so a missing file (e.g. dockerfile = null) round-trips as a JSON
// null rather than the empty string.
type ComponentFiles struct {
	Readme     *string `json:"readme"`
	Package    *string `json:"package"`
	Dockerfile *string `json:"dockerfile"`
}

// ComponentResolution records WHERE each resolved value came from. Provenance
// is excluded from `manifestHash` per identity-and-keys.md §10 — changing
// only inheritedFrom must NOT change the manifest hash.
type ComponentResolution struct {
	InheritedFrom map[string]string   `json:"inheritedFrom"`
	InferredFrom  map[string][]string `json:"inferredFrom"`
}

// Allowed `relationship` values for ComponentDependency, per data-model.md §3.
const (
	RelCalls       = "calls"
	RelDependsOn   = "depends-on"
	RelDeployAfter = "deploy-after"
	RelLinksTo     = "links-to"
)
