package nodes

import "time"

// SourceSnapshot is the root content node: an exact git/worktree state. Its id
// is the blob id of this record, which is input-addressed by construction
// because every field is derived deterministically from git state. Per
// decision D-6, no resolvedAt/observation time is stored here (it would defeat
// source dedup); the observation time lives on the ref/trigger that recorded it.
type SourceSnapshot struct {
	Kind         string `json:"kind"`
	HumanKey     string `json:"humanKey,omitempty"`
	Scope        string `json:"scope"` // main|branch|pr|local-nogit
	Repo         string `json:"repo,omitempty"`
	HeadRevision string `json:"headRevision,omitempty"`
	TreeHash     string `json:"treeHash,omitempty"`
	Branch       string `json:"branch,omitempty"`
	PR           string `json:"pr,omitempty"`
	WorkingTree  string `json:"workingTree,omitempty"` // clean|dirty
	DirtyHash    string `json:"dirtyHash,omitempty"`
}

// CatalogComponentRef indexes a component manifest inside a catalog. The actual
// manifest blob lives in the catalog tree's components/ subtree.
type CatalogComponentRef struct {
	ComponentKey string `json:"componentKey"`
	Name         string `json:"name"`
	ManifestID   string `json:"manifestId"`
}

// CatalogIssue is a non-fatal resolver finding (the tolerant part of
// tolerant-strict).
type CatalogIssue struct {
	Severity  string `json:"severity"` // warning|error
	Component string `json:"component,omitempty"`
	Message   string `json:"message"`
}

// CatalogSnapshot is the resolved catalog for a source. Its id is the Merkle
// root of the catalog tree (catalog.json + components/ + graph/), NOT this
// record's blob id, so it must not embed catalogId. It may embed sourceId (an
// edge) and the manifest/graph ids (edges to its children).
type CatalogSnapshot struct {
	Kind            string                `json:"kind"`
	HumanKey        string                `json:"humanKey,omitempty"`
	SourceID        string                `json:"sourceId"`
	ResolverVersion int                   `json:"resolverVersion"`
	ComponentCount  int                   `json:"componentCount"`
	CountsByKind    map[string]int        `json:"countsByKind,omitempty"`
	Components      []CatalogComponentRef `json:"components"`
	GraphIDs        map[string]string     `json:"graphIds,omitempty"`
	Issues          []CatalogIssue        `json:"issues,omitempty"`

	// DeclaredEntities carries entities the resolver declares directly (not
	// derived from component relations) — e.g. the single Repo self-describing
	// the repo (saas-workspace-overview WO3). It is a transient assembly input,
	// NOT part of the serialized catalog record (json:"-"), so it never changes
	// the catalog.json blob id; AssembleCatalog folds it into entities/<Kind>/.
	DeclaredEntities []Entity `json:"-"`

	// EntityEnrichments carries the resolver's catalog.entities entries
	// (saas-catalog-docs CD2), applied to matching DERIVED entities during
	// assembly (fill-empty metadata + the doc set). Enrich, never create: an
	// entry with no matching derived entity is ignored here (the resolver
	// already warned). Transient — not part of the serialized snapshot.
	EntityEnrichments []EntityEnrichment `json:"-"`
}

// ComponentIdentity is the identifying tuple of a component. Environment is
// never part of identity.
type ComponentIdentity struct {
	ComponentKey string `json:"componentKey"` // <namespace>/<repo>/<componentName>
	Name         string `json:"name"`
	Namespace    string `json:"namespace"`
	Repo         string `json:"repo"`
	// Path is the workspace-relative component.yaml location (the resolver's
	// Identity.Path). Required when known; omitted for a synthetic root
	// component. Adding it changes the manifest blob hash → catalog Merkle id
	// on the next resolve (content-addressing absorbs the one-time change).
	Path string `json:"path,omitempty"`
}

// ComponentManifest is the fully-resolved component definition (a content
// blob). It is the Component-kind specialization of the shared entity envelope
// (orun-service-catalog/data-model.md §2): the flat metadata block splits into
// metadata/ownership/lifecycle, dependencies promote to relations/contracts,
// and the resolver/inference provenance stays hash-excluded by convention. The
// deep blocks are carried as generic maps here; the resolver (catalogresolve)
// and the objplan mapper (mapEntity) own their detailed shape.
//
// SC0 added the generic nodes.Entity mirror for the other kinds (API/Resource/
// …); SC3 generalizes the storage to entities/<Kind>/. Until then Component
// remains the only resolved kind and ComponentManifest is its envelope.
type ComponentManifest struct {
	APIVersion   string            `json:"apiVersion,omitempty"`
	Kind         string            `json:"kind"`
	Identity     ComponentIdentity `json:"identity"`
	Type         string            `json:"type,omitempty"`
	Metadata     map[string]any    `json:"metadata,omitempty"`
	Ownership    map[string]any    `json:"ownership,omitempty"`
	Lifecycle    map[string]any    `json:"lifecycle,omitempty"`
	Spec         map[string]any    `json:"spec,omitempty"`
	Relations    []EntityRelation  `json:"relations,omitempty"`
	Contracts    map[string]any    `json:"contracts,omitempty"`
	Integrations map[string]any    `json:"integrations,omitempty"`
	Docs         map[string]any    `json:"docs,omitempty"`
	Links        []map[string]any  `json:"links,omitempty"`
	Provenance   map[string]any    `json:"provenance,omitempty"`
	Extensions   map[string]any    `json:"extensions,omitempty"`

	// PendingDocs is a transient assembly input (NOT serialized): doc-key →
	// raw bytes to be written as content-addressed blobs into the catalog's
	// docs/ subtree (saas-catalog-docs CD1 — components carry doc sets the
	// same way entities do). AssembleCatalog uploads each and stamps the
	// matching Docs entry's digest via stampPendingDocs.
	PendingDocs map[string][]byte `json:"-"`
}

// EntityIdentity is the generalized, multi-kind identity tuple for a catalog
// entity (orun-service-catalog/data-model.md §2). It is the kind-agnostic
// successor to ComponentIdentity; `kind` distinguishes two entities that share
// a name. `tenant` is reserved for SC12 (S-8) and omitted in v1.
type EntityIdentity struct {
	EntityKey string `json:"entityKey"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace"`
	Repo      string `json:"repo"`
	Tenant    string `json:"tenant,omitempty"`
	Path      string `json:"path,omitempty"`
}

// Entity is the L1 node mirror of catalogmodel.EntityEnvelope — the resolved,
// content-addressed blob for any catalog kind (Component/API/Resource/System/
// …). The deep metadata/ownership/lifecycle/spec/contracts/… blocks are carried
// as generic values here exactly as ComponentManifest carries metadata/spec/
// provenance; the resolver (catalogresolve) owns their detailed shape. SC0
// introduces the type; the resolver begins emitting it in SC1.
type Entity struct {
	APIVersion   string           `json:"apiVersion"`
	Kind         string           `json:"kind"`
	Identity     EntityIdentity   `json:"identity"`
	Metadata     map[string]any   `json:"metadata,omitempty"`
	Ownership    map[string]any   `json:"ownership,omitempty"`
	Lifecycle    map[string]any   `json:"lifecycle,omitempty"`
	Spec         map[string]any   `json:"spec,omitempty"`
	Relations    []EntityRelation `json:"relations,omitempty"`
	Contracts    map[string]any   `json:"contracts,omitempty"`
	Integrations map[string]any   `json:"integrations,omitempty"`
	Docs         map[string]any   `json:"docs,omitempty"`
	Links        []map[string]any `json:"links,omitempty"`
	Provenance   map[string]any   `json:"provenance,omitempty"`
	Extensions   map[string]any   `json:"extensions,omitempty"`

	// PendingDocs is a transient assembly input (NOT serialized): doc-key →
	// raw bytes to be written as content-addressed blobs into the catalog's
	// docs/ subtree (saas-workspace-overview WO3b). AssembleCatalog uploads
	// each, then fills the matching Docs[key].digest with the blob id so the
	// entity references the doc by its content address.
	PendingDocs map[string][]byte `json:"-"`
}

// EntityRelation is one typed forward edge owned by an Entity (data-model.md
// §3). Inverses are materialized by the reader, never stored.
type EntityRelation struct {
	Type     string `json:"type"`
	To       string `json:"to"`
	ToKind   string `json:"toKind"`
	Optional bool   `json:"optional,omitempty"`
	Include  string `json:"include,omitempty"`
	// Input marks a dependsOn edge whose target's sources are build inputs of
	// this entity (change detection rescopes the dependent on a target change).
	Input bool `json:"input,omitempty"`
}

// ImpactOwnership is the change-detection ownership map (data-model.md §2). It
// lives inside the catalog tree at impact/ownership.json and folds into the
// catalog Merkle root; it is a deterministic function of the resolved catalog +
// discovery and carries no timestamps. The change-detection engine
// (internal/affected, CS4) uses it to map a changed workspace path to the
// component that owns it.
type ImpactOwnership struct {
	Kind                string            `json:"kind"`
	SchemaVersion       int               `json:"schemaVersion"`
	Components          map[string]string `json:"components"`          // workspace-relative dir → componentKey
	GlobalPaths         []string          `json:"globalPaths"`         // files whose change is global (e.g. intent.yaml)
	GlobalBlocks        []string          `json:"globalBlocks"`        // catalog-relevant intent.yaml blocks
	StructuralFilenames []string          `json:"structuralFilenames"` // basenames whose add/remove/edit is structural
	IgnoreDirs          []string          `json:"ignoreDirs"`          // directory basenames discovery prunes
}

// ComponentFingerprint is one component's input fingerprint — the leaf-set of
// the change-detection virtual Merkle tree (data-model.md §2b). Derived at
// resolve time, stored at impact/fingerprints/<name>.json, deterministic and
// timestamp-free so it folds into the catalog Merkle root. The cockpit's
// content-aware change source compares a recomputed Subtree against the stored
// one (CS6): a mismatch ⇒ that component changed.
type ComponentFingerprint struct {
	Kind          string            `json:"kind"`
	SchemaVersion int               `json:"schemaVersion"`
	ComponentKey  string            `json:"componentKey"`
	Dir           string            `json:"dir"`                    // workspace-relative component dir
	Subtree       string            `json:"subtree"`                // hash over the input file set (the leaf-set root)
	Files         map[string]string `json:"files,omitempty"`        // workspace-relative path → content hash
	GlobalDigest  string            `json:"globalDigest,omitempty"` // hash of the catalog-relevant intent blocks (shared leaf)
}

// GraphNode / GraphEdge model one edge-kind slice of the catalog graph.
type GraphNode struct {
	Key  string `json:"key"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type GraphEdge struct {
	From     string `json:"from"`
	To       string `json:"to"`
	Type     string `json:"type"`
	Optional bool   `json:"optional,omitempty"` // dependency-edge optionality, carried from the resolver (omitted when false to keep the catalog hash stable for required edges)
	Include  string `json:"include,omitempty"`  // change-detection plan-selection mode ("always"; omitted = if-selected)
	Input    bool   `json:"input,omitempty"`    // build-input edge: change detection rescopes the dependent on a target change
}

// CatalogGraph is one edge-kind slice (dependencies/systems/apis/resources/
// owners), stored as a blob under graph/. Must not embed catalogId.
type CatalogGraph struct {
	Kind     string      `json:"kind"`
	EdgeKind string      `json:"edgeKind"`
	Nodes    []GraphNode `json:"nodes"`
	Edges    []GraphEdge `json:"edges"`
}

// RelationGraph is the single typed relation graph (orun-service-catalog/
// data-model.md §3) — one blob per catalog (relations.json) replacing the
// per-edge-kind CatalogGraph slices. Only the forward edge is stored; the reader
// materializes inverses. Edges are sorted by (from, fromKind, type, to) for
// determinism. Must not embed catalogId.
type RelationGraph struct {
	Kind  string         `json:"kind"`
	Edges []RelationEdge `json:"edges"`
}

// RelationEdge is one typed forward edge spanning kinds (data-model.md §3). The
// optional/include attributes carry change-detection semantics (CV-1) and must
// survive resolve → internal/affected.
type RelationEdge struct {
	From     string `json:"from"`
	FromKind string `json:"fromKind"`
	Type     string `json:"type"`
	To       string `json:"to"`
	ToKind   string `json:"toKind"`
	Optional bool   `json:"optional,omitempty"`
	Include  string `json:"include,omitempty"`
	// Input — see EntityRelation.Input.
	Input bool `json:"input,omitempty"`
}

// RevisionScope captures the planning scope (full vs changed-subset). It is
// part of the plan inputs and therefore part of revision identity.
type RevisionScope struct {
	Mode            string   `json:"mode"` // full|changed
	Components      []string `json:"components,omitempty"`
	MatchedTriggers []string `json:"matchedTriggers,omitempty"`
}

// PlanRevision is a compiled plan. Its id is the Merkle root of the revision
// tree (revision.json + plan.json). It MUST NOT embed the revisionId, the
// trigger, an executionId, or any timestamp — those would break the property
// that two triggers compiling an identical plan share one revision.
type PlanRevision struct {
	Kind           string        `json:"kind"`
	HumanKey       string        `json:"humanKey,omitempty"`
	CatalogID      string        `json:"catalogId,omitempty"`
	SourceID       string        `json:"sourceId,omitempty"`
	PlanHash       string        `json:"planHash"`
	Scope          RevisionScope `json:"scope"`
	JobCount       int           `json:"jobCount"`
	LegacyChecksum string        `json:"legacyChecksum,omitempty"`
}

// TriggerSource distinguishes declared (CI) from system triggers.
type TriggerSource struct {
	Flavor string `json:"flavor"`           // system|declared
	System string `json:"system,omitempty"` // manual|manual-changed|replay|api|migrated
}

// TriggerOccurrence is an append-only event pointing at the revision it
// produced. Its id is the blob id; uniqueness comes from the embedded ULID, so
// it never dedups (by design).
type TriggerOccurrence struct {
	Kind          string         `json:"kind"`
	TriggerID     string         `json:"triggerId"` // trg_<ULID>
	TriggerName   string         `json:"triggerName"`
	TriggerKey    string         `json:"triggerKey"`
	RevisionID    string         `json:"revisionId"`
	Source        TriggerSource  `json:"source"`
	Scope         RevisionScope  `json:"scope"`
	CreatedAt     time.Time      `json:"createdAt"`
	Actor         string         `json:"actor"` // cli|runner|tui|saas|ci
	ProviderEvent map[string]any `json:"providerEvent,omitempty"`
}

// ExecLink is an external link surfaced for an execution (CI run page, etc.).
type ExecLink struct {
	Label  string `json:"label"`
	URL    string `json:"url"`
	JobID  string `json:"jobId,omitempty"`
	StepID string `json:"stepId,omitempty"`
}

// ExecSummary holds rolled-up counts computed at seal time.
type ExecSummary struct {
	JobsTotal     int `json:"jobsTotal"`
	JobsSucceeded int `json:"jobsSucceeded"`
	JobsFailed    int `json:"jobsFailed"`
	StepsTotal    int `json:"stepsTotal"`
}

// RunnerProfile records how the execution was run.
type RunnerProfile struct {
	Concurrency int  `json:"concurrency"`
	FailFast    bool `json:"failFast"`
}

// ExecutionRun is an execution event that becomes content when sealed. Its
// sealed id is the Merkle root of the execution tree. jobIds maps the sanitized
// job folder name to that job's sealed JobRun tree id.
type ExecutionRun struct {
	Kind          string            `json:"kind"`
	ExecutionID   string            `json:"executionId"` // exec_<ULID> or gh-<run>-<attempt>-<sha>
	ExecutionKey  string            `json:"executionKey"`
	RevisionID    string            `json:"revisionId"`
	TriggerID     string            `json:"triggerId"`
	Status        string            `json:"status"`
	StartedAt     time.Time         `json:"startedAt"`
	FinishedAt    *time.Time        `json:"finishedAt,omitempty"`
	DryRun        bool              `json:"dryRun"`
	RunnerProfile RunnerProfile     `json:"runnerProfile"`
	Summary       ExecSummary       `json:"summary"`
	Links         []ExecLink        `json:"links,omitempty"`
	JobIDs        map[string]string `json:"jobIds,omitempty"`
}

// JobRun, JobAttempt, StepAttempt are the sealed lower lineage. Their ids are
// Merkle roots / blob ids within the execution tree.
type JobRun struct {
	Kind       string            `json:"kind"`
	JobID      string            `json:"jobId"`  // original (may contain @ . /)
	Folder     string            `json:"folder"` // j-<shortHash>
	Status     string            `json:"status"`
	StartedAt  *time.Time        `json:"startedAt,omitempty"`
	FinishedAt *time.Time        `json:"finishedAt,omitempty"`
	LastError  string            `json:"lastError,omitempty"`
	AttemptIDs map[string]string `json:"attemptIds,omitempty"`
}

type JobAttempt struct {
	Kind       string            `json:"kind"`
	Attempt    int               `json:"attempt"`
	Status     string            `json:"status"`
	StartedAt  *time.Time        `json:"startedAt,omitempty"`
	FinishedAt *time.Time        `json:"finishedAt,omitempty"`
	StepIDs    map[string]string `json:"stepIds,omitempty"`
}

type StepAttempt struct {
	Kind        string     `json:"kind"`
	StepID      string     `json:"stepId"`
	Status      string     `json:"status"`
	StartedAt   *time.Time `json:"startedAt,omitempty"`
	FinishedAt  *time.Time `json:"finishedAt,omitempty"`
	ExitCode    int        `json:"exitCode"`
	LogID       string     `json:"logId,omitempty"`
	HeartbeatAt *time.Time `json:"heartbeatAt,omitempty"`
}

// StoreVersion is the store-level metadata file (mutable, single — not an
// object).
type StoreVersion struct {
	Kind               string    `json:"kind"`
	ObjectModelVersion int       `json:"objectModelVersion"`
	HashAlgo           string    `json:"hashAlgo"`
	ResolverVersion    int       `json:"resolverVersion"`
	CreatedAt          time.Time `json:"createdAt"`
}

// EntityEnrichment is one catalog.entities enrichment (saas-catalog-docs CD2)
// targeted at a derived entity by (Kind, bare Name). Docs/PendingDocs follow
// the same wire + assembly contract as every other entity doc set.
type EntityEnrichment struct {
	Kind        string
	Name        string
	Description string
	Owner       string
	Links       []map[string]any
	Tags        []string
	Docs        map[string]any
	PendingDocs map[string][]byte
}
