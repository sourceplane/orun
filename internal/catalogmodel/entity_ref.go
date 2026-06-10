package catalogmodel

// EntityRef is the small reference shape used inside CatalogObjects entries
// and other denormalized lookups when a full ManifestRef is overkill. Kept
// in its own file so future entity kinds (System, API, Resource, Group) can
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

// EntityKind enum values per orun-service-catalog/data-model.md §2.1. The
// catalog graduates from a single Component kind into a typed, multi-kind
// entity graph. `Owner` is renamed to `Group`; EntityKindOwner is retained as
// a read-time alias (see NormalizeEntityKind) so older blobs up-convert.
const (
	EntityKindComponent   = "Component"
	EntityKindAPI         = "API"
	EntityKindResource    = "Resource"
	EntityKindSystem      = "System"
	EntityKindDomain      = "Domain"
	EntityKindGroup       = "Group"
	EntityKindUser        = "User"
	EntityKindComposition = "Composition"
	EntityKindEnvironment = "Environment" // derived from execution (SC4)
	EntityKindDeployment  = "Deployment"  // derived from execution (SC4/SC8)

	// EntityKindOwner is the legacy spelling of EntityKindGroup. It is never
	// emitted by the v1 resolver; readers normalize it via NormalizeEntityKind.
	EntityKindOwner = "Owner"
)

// allEntityKinds is the canonical, sorted set of first-class v1 kinds (the
// legacy Owner alias is intentionally excluded — it normalizes to Group).
var allEntityKinds = []string{
	EntityKindAPI,
	EntityKindComponent,
	EntityKindComposition,
	EntityKindDeployment,
	EntityKindDomain,
	EntityKindEnvironment,
	EntityKindGroup,
	EntityKindResource,
	EntityKindSystem,
	EntityKindUser,
}

// AllEntityKinds returns the canonical v1 entity kinds in sorted order. The
// returned slice is a copy; callers may mutate it freely.
func AllEntityKinds() []string {
	return append([]string(nil), allEntityKinds...)
}

// NormalizeEntityKind maps a stored kind string onto its canonical v1 form.
// The only rewrite today is the legacy `Owner` → `Group` alias (data-model.md
// §2.1 / migration.md §3); every other kind passes through unchanged, so an
// unknown kind is returned verbatim for the caller to validate.
func NormalizeEntityKind(kind string) string {
	if kind == EntityKindOwner {
		return EntityKindGroup
	}
	return kind
}

// IsEntityKind reports whether kind is a known first-class v1 entity kind. The
// legacy `Owner` alias is accepted (it normalizes to Group).
func IsEntityKind(kind string) bool {
	if kind == EntityKindOwner {
		return true
	}
	for _, k := range allEntityKinds {
		if k == kind {
			return true
		}
	}
	return false
}
