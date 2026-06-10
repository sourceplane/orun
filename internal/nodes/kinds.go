// Package nodes defines the L1 typed-node layer of the orun-object-model store
// (specs/orun-object-model/data-model.md). Each node is a canonical JSON record
// (a blob) optionally bundled by a Merkle tree with its children. This package
// owns the record schemas, their canonical encoding, validation, and the
// tree-assembly + identity helpers that turn records into content-addressed
// objects via internal/objectstore.
//
// Identity rules (identity-and-keys.md):
//   - SourceSnapshot, ComponentManifest, CatalogGraph, TriggerOccurrence: blob
//     id of the record.
//   - CatalogSnapshot, PlanRevision, sealed ExecutionRun: Merkle root of the
//     node's tree.
//   - Content nodes never embed their own id, and PlanRevision additionally
//     excludes the trigger and any timestamp so that two triggers producing an
//     identical plan share one revision (the dedup-across-triggers property).
package nodes

// Kind discriminators for every persisted record.
const (
	KindSourceSnapshot        = "SourceSnapshot"
	KindCatalogSnapshot       = "CatalogSnapshot"
	KindComponentManifest     = "ComponentManifest"
	KindCatalogGraph          = "CatalogGraph"
	KindPlanRevision          = "PlanRevision"
	KindTriggerOccurrence     = "TriggerOccurrence"
	KindExecutionRun          = "ExecutionRun"
	KindJobRun                = "JobRun"
	KindJobAttempt            = "JobAttempt"
	KindStepAttempt           = "StepAttempt"
	KindExecutionEvent        = "ExecutionEvent"
	KindComponentHistoryIndex = "ComponentHistoryIndex"
	KindStoreVersion          = "StoreVersion"
	KindImpactOwnership       = "ImpactOwnership"
	KindComponentFingerprint  = "ComponentFingerprint"
	// KindRelationGraph is the single typed relation-graph blob that replaces
	// the per-edge-kind CatalogGraph slices (orun-service-catalog/data-model.md
	// §3). Introduced in SC0; emitted by the resolver in SC2.
	KindRelationGraph = "RelationGraph"
)

// Source scope values (identity-and-keys.md §3).
const (
	ScopeMain       = "main"
	ScopeBranch     = "branch"
	ScopePR         = "pr"
	ScopeLocalNoGit = "local-nogit"
)

// Execution / job / step / attempt status values. Only terminal statuses are
// sealed (runner-integration.md §1).
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusSucceeded = "succeeded"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// IsTerminalStatus reports whether s is a terminal execution/job status — the
// gate the runner uses before sealing.
func IsTerminalStatus(s string) bool {
	switch s {
	case StatusSucceeded, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

// validStatus reports whether s is any known status (terminal or not).
func validStatus(s string) bool {
	switch s {
	case StatusPending, StatusRunning, StatusSucceeded, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}

// Canonical tree entry filenames (object-store.md / data-model.md).
const (
	fileSource    = "source.json"
	fileCatalog   = "catalog.json"
	fileRevision  = "revision.json"
	filePlan      = "plan.json"
	fileExecution = "execution.json"
	fileJobRun    = "job-run.json"
	fileAttempt   = "attempt.json"
	dirComponents = "components"
	dirGraph      = "graph"
	dirImpact       = "impact"
	fileOwnership   = "ownership.json"
	dirFingerprints = "fingerprints"
	fileRelations   = "relations.json"
	dirEntities     = "entities"
	dirExecutions = "executions"
	dirJobs       = "jobs"
	dirAttempts   = "attempts"
	dirSteps      = "steps"
	dirEvents     = "events"
	dirLogs       = "logs"
	dirArtifacts  = "artifacts"
)
