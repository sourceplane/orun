package catalogmodel

// This file defines the shared entity envelope and the per-kind `spec` shapes
// that the software catalog (specs/orun-service-catalog/) resolves every
// entity into. It is the SC0 vocabulary: the Go types every later milestone
// (SC1 reshape, SC2 relations, SC3 multi-kind, …) reads and writes. The
// envelope is the L1 resolved form; `data-model.md §2` is the on-disk contract
// these types generate/serialize.
//
// SC0 only stands the types up — nothing in the resolver emits an
// EntityEnvelope yet (the reshape is SC1). The types compile, serialize
// through CanonicalEncode/PrettyEncode, and round-trip.

// APIVersionV1 is the graduated catalog schema version. v1alpha1 blobs
// up-convert to it lazily on read (entity_convert.go, migration.md §2).
const APIVersionV1 = "orun.io/v1"

// EntityEnvelope is the single shape shared by every catalog kind. Only `spec`
// differs per kind (§4). `provenance` is EXCLUDED from manifestHash (§2, §10);
// every other block is included. Optional blocks serialize as `null`/absent and
// readers MUST treat the two identically.
type EntityEnvelope struct {
	APIVersion   string           `json:"apiVersion"`
	Kind         string           `json:"kind"`
	Identity     EntityIdentity   `json:"identity"`
	Metadata     EntityMetadata   `json:"metadata"`
	Ownership    EntityOwnership  `json:"ownership"`
	Lifecycle    EntityLifecycle  `json:"lifecycle"`
	Spec         any              `json:"spec"`
	Relations    []EntityRelation `json:"relations"`
	Contracts    *EntityContracts `json:"contracts"`
	Integrations map[string]any   `json:"integrations"`
	Docs         *EntityDocs      `json:"docs"`
	Links        []EntityLink     `json:"links"`
	Provenance   EntityProvenance `json:"provenance"`
	Extensions   map[string]any   `json:"extensions"`
}

// EntityIdentity is the stable, cross-source identity of an entity. The
// `entityKey` is the 3-segment `<namespace>/<repo>/<name>` string paired with
// `kind` so the same name under two kinds is distinct (§1). `tenant` is
// reserved for SC12 multi-tenant federation (S-8) and is never emitted in v1.
type EntityIdentity struct {
	EntityKey string `json:"entityKey"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Repo      string `json:"repo"`
	Tenant    string `json:"tenant,omitempty"`
	Path      string `json:"path,omitempty"`
}

// EntityMetadata is the queryable, faceted descriptive block. `annotations` is
// the untyped escape hatch of last resort (§8).
type EntityMetadata struct {
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Tags        []string          `json:"tags,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// EntityOwnership records the (single primary) owner, additional owners,
// contacts, and — critically — the *source* of the ownership claim (S-2).
type EntityOwnership struct {
	Owner            string          `json:"owner"`
	AdditionalOwners []string        `json:"additionalOwners,omitempty"`
	Contacts         []EntityContact `json:"contacts,omitempty"`
	Escalation       string          `json:"escalation,omitempty"`
	Source           string          `json:"source"`
}

// EntityContact is one typed contact channel (slack/pagerduty/email/…).
type EntityContact struct {
	Type    string `json:"type"`
	Value   string `json:"value"`
	Primary bool   `json:"primary,omitempty"`
}

// Ownership source values (data-model.md §2 / S-2). Precedence on resolve is
// authored > CODEOWNERS > inherited; `unknown` only when no claim exists.
const (
	OwnershipSourceAuthored   = "authored"
	OwnershipSourceCODEOWNERS = "CODEOWNERS"
	OwnershipSourceInherited  = "inherited"
	OwnershipSourceUnknown    = "unknown"
)

// EntityLifecycle carries stage/criticality and the denormalized maturity. The
// maturity value is recomputed from L2 every resolve and is `null` until the
// v2 scorecard engine (`specs/orun-scorecards/`) lands — never authored (CR-1).
type EntityLifecycle struct {
	Stage    string  `json:"stage,omitempty"`
	Tier     string  `json:"tier,omitempty"`
	Maturity *string `json:"maturity"`
}

// Lifecycle stage values (data-model.md §2).
const (
	LifecycleStageExperimental = "experimental"
	LifecycleStageProduction   = "production"
	LifecycleStageDeprecated   = "deprecated"
	LifecycleStageRetired      = "retired"
)

// EntityRelation is one typed forward edge owned by this entity. Inverses are
// materialized by the reader (objcatalog), never stored (§3). `optional` and
// `include` carry change-detection semantics and MUST be preserved through
// resolve → internal/affected (CV-1).
type EntityRelation struct {
	Type     string `json:"type"`
	To       string `json:"to"`
	ToKind   string `json:"toKind"`
	Optional bool   `json:"optional,omitempty"`
	Include  string `json:"include,omitempty"`
}

// EntityContracts groups the APIs an entity provides/consumes (§2). APIs are
// first-class entities; these are the typed edges to them with definition refs.
type EntityContracts struct {
	Provides []APIContract `json:"provides,omitempty"`
	Consumes []APIContract `json:"consumes,omitempty"`
}

// APIContract is one provided/consumed API reference, optionally carrying the
// definition pointer + visibility/stability for a provided API.
type APIContract struct {
	API        string `json:"api"`
	Definition string `json:"definition,omitempty"`
	Ref        string `json:"ref,omitempty"`
	Visibility string `json:"visibility,omitempty"`
	Stability  string `json:"stability,omitempty"`
}

// EntityDocs points at overview/techdocs/runbooks/ADRs (§2). `overview` is the
// reserved front-page md pointer, shared by every kind (saas-workspace-overview
// WO3); `pages` is the ordered multi-doc set (saas-catalog-docs CD1).
type EntityDocs struct {
	Overview string    `json:"overview,omitempty"`
	Pages    []DocPage `json:"pages,omitempty"`
	TechDocs string    `json:"techdocs,omitempty"`
	Runbooks []string  `json:"runbooks,omitempty"`
	ADRs     []string  `json:"adrs,omitempty"`
}

// EntityLink is one external link surfaced in the portal/CLI (§2).
type EntityLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Icon  string `json:"icon,omitempty"`
}

// EntityProvenance records where each resolved value came from. EXCLUDED from
// manifestHash (§2, §10) — changing only provenance must not move the hash.
type EntityProvenance struct {
	InheritedFrom map[string]string   `json:"inheritedFrom,omitempty"`
	InferredFrom  map[string][]string `json:"inferredFrom,omitempty"`
	ManifestHash  string              `json:"manifestHash,omitempty"`
	Resolver      *EntityResolver     `json:"resolver,omitempty"`
	Attestation   *EntityAttestation  `json:"attestation"`
}

// EntityResolver stamps the tool/schema versions that produced an entity.
type EntityResolver struct {
	OrunVersion     string `json:"orunVersion,omitempty"`
	ResolverVersion int    `json:"resolverVersion"`
	SchemaVersion   string `json:"schemaVersion,omitempty"`
}

// EntityAttestation is the SC12 signature block (reserved; `null` in v1).
type EntityAttestation struct {
	Signature string `json:"signature"`
	SignedBy  string `json:"signedBy"`
}

// --- Per-kind spec blocks (§4). The envelope is identical; only spec differs.

// ComponentSpecV1 is the `spec` for kind=Component: today's spec minus the
// inlined dependencies (which move to relations/contracts).
type ComponentSpecV1 struct {
	Type         string                          `json:"type,omitempty"`
	Composition  *CompositionRef                 `json:"composition,omitempty"`
	Parameters   map[string]string               `json:"parameters,omitempty"`
	Environments map[string]ComponentEnvironment `json:"environments,omitempty"`
	Runtime      *RuntimeInfo                    `json:"runtime,omitempty"`
}

// RuntimeInfo is the inferred runtime profile carried on a Component spec.
type RuntimeInfo struct {
	Languages  []string `json:"languages,omitempty"`
	Frameworks []string `json:"frameworks,omitempty"`
	Infra      []string `json:"infra,omitempty"`
}

// APISpec is the `spec` for kind=API.
type APISpec struct {
	Type          string `json:"type"`                    // openapi|asyncapi|grpc|graphql
	DefinitionRef string `json:"definitionRef,omitempty"` // path into the snapshot
	Visibility    string `json:"visibility,omitempty"`    // public|internal
	Stability     string `json:"stability,omitempty"`     // experimental|stable|deprecated
	Version       string `json:"version,omitempty"`
}

// ResourceSpec is the `spec` for kind=Resource.
type ResourceSpec struct {
	Type       string            `json:"type"` // datastore|queue|topic|bucket|cache
	Provider   string            `json:"provider,omitempty"`
	Parameters map[string]string `json:"parameters,omitempty"`
}

// SystemSpec / DomainSpec carry a derived membership count; membership itself
// lives in relations (hasPart).
type SystemSpec struct {
	Members int `json:"members"`
}

// DomainSpec mirrors SystemSpec for kind=Domain.
type DomainSpec struct {
	Members int `json:"members"`
}

// GroupSpec / UserSpec are sourced from CODEOWNERS / IdP.
type GroupSpec struct {
	Members []string `json:"members,omitempty"`
}

// UserSpec carries contact info for kind=User.
type UserSpec struct {
	Email string `json:"email,omitempty"`
}

// EnvironmentSpec is the `spec` for the derived kind=Environment (SC4).
type EnvironmentSpec struct {
	Type      string `json:"type"` // dev|staging|production|preview
	Order     int    `json:"order"`
	Protected bool   `json:"protected,omitempty"`
}

// DeploymentSpec is the `spec` for the derived kind=Deployment (SC4/SC8): the
// L1 record that a deploy happened. Live health is L2 (§6).
type DeploymentSpec struct {
	Component   string `json:"component"`
	Environment string `json:"environment"`
	Revision    string `json:"revision,omitempty"`
	ExecutionID string `json:"executionId,omitempty"`
	Status      string `json:"status,omitempty"`
	DeployedAt  string `json:"deployedAt,omitempty"`
}
