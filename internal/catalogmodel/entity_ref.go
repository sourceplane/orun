package catalogmodel

// EntityRef is the small reference shape used inside CatalogObjects entries
// and other denormalized lookups when a full ManifestRef is overkill. Kept
// in its own file so future entity kinds (System, API, Resource, Owner) can
// be added without churning component_manifest.go.
//
// data-model.md §3 references the component-flavoured shape under
// CatalogObjects.Components; this type is the generalized form callers can
// reuse.
type EntityRef struct {
	Key  string `json:"key"`
	Kind string `json:"kind"`
	Name string `json:"name"`
	Path string `json:"path,omitempty"`
}

// EntityKind enum values per data-model.md §4 (graph node kinds).
const (
	EntityKindComponent = "Component"
	EntityKindSystem    = "System"
	EntityKindAPI       = "API"
	EntityKindResource  = "Resource"
	EntityKindOwner     = "Owner"
)
