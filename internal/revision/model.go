// Package revision owns the PlanRevision domain model, the deterministic
// revision-key generator + collision resolver, and the writer that persists
// trigger.json / revision.json / plan.json plus the ref+index updates listed
// in design.md §5.1 and cli-surface.md §1.2.
//
// Phase 1 / M3 PR-A scope (this package, this PR):
//
//   - PlanRevision + RevSummary types matching data-model.md §3.
//   - RevisionKey / ValidateRevisionKey / ResolveCollision helpers in keys.go.
//   - WriteRevision in writer.go executing the seven-step ordered write list
//     against any internal/statestore.StateStore implementation.
//   - EnsureStateStoreVersion idempotency helper for .orun/version.json.
//
// Out of scope until PR-B (Task 0008) and later milestones:
//
//   - manifest.go / WriteManifest / UpdateLatestExecutionSummary.
//   - resolver.go / ResolveRevision (the seven-branch resolver).
//   - the legacy compatibility-mirror body (.orun/plans/<checksum>.json),
//     which lands during M5 CLI rewire. The CompatibilityWrites flag plumbing
//     is shipped here so M5 has nothing to add but the legacy-write body.
//
// Dependencies are kept minimal to satisfy the M3 leaf-clean constraint:
// only internal/statestore, internal/triggerctx, stdlib, and oklog/ulid/v2.
package revision

import (
	"time"

	"github.com/sourceplane/orun/internal/triggerctx"
)

// API constants. These are written verbatim into every persisted PlanRevision
// and must remain in lockstep with data-model.md §3.
const (
	APIVersion = "orun.io/v1alpha1"
	KindName   = "PlanRevision"

	// idPrefixRevision is concatenated with the underlying ULID to form a
	// PlanRevision.RevisionID. Mirrors triggerctx's "trg_" convention.
	idPrefixRevision = "rev_"

	// StateStoreVersionKind is the Kind written to .orun/version.json
	// (data-model.md §1).
	StateStoreVersionKind = "StateStoreVersion"

	// StateStoreLayoutRevisionFirst is the layout identifier for the new
	// trigger-first / revision-first state layout (data-model.md §1).
	StateStoreLayoutRevisionFirst = "revision-first"

	// StateStoreVersionCurrent is the integer version persisted alongside
	// the layout name. Bumping it requires a migration plan in
	// compatibility-and-migration.md.
	StateStoreVersionCurrent = 1
)

// PlanRevision is the persisted shape of revision.json (data-model.md §3).
// JSON tag order MUST match the spec byte-for-byte; downstream readers
// (manifest writer, CLI describe) discriminate on these field names.
type PlanRevision struct {
	APIVersion    string                    `json:"apiVersion"`
	Kind          string                    `json:"kind"`
	RevisionID    string                    `json:"revisionId"`
	RevisionKey   string                    `json:"revisionKey"`
	TriggerID     string                    `json:"triggerId"`
	TriggerKey    string                    `json:"triggerKey"`
	PlanHash      string                    `json:"planHash"`
	PlanShortHash string                    `json:"planShortHash"`
	Source        triggerctx.TriggerSource  `json:"source"`
	Summary       RevSummary                `json:"summary"`
	CreatedAt     time.Time                 `json:"createdAt"`
}

// RevSummary captures the plan-shape fields surfaced in revision.json
// (data-model.md §3). The fields are derived from the trigger's PlanScope
// at write time so resolver code does not need to re-open plan.json.
type RevSummary struct {
	JobCount           int      `json:"jobCount"`
	Scope              string   `json:"scope"`
	ActiveEnvironments []string `json:"activeEnvironments"`
	ChangedComponents  []string `json:"changedComponents,omitempty"`
}

// StateStoreVersion is the persisted shape of .orun/version.json
// (data-model.md §1). The struct is exported so callers (M5 CLI, future
// migration tooling) can read it without re-declaring the schema.
type StateStoreVersion struct {
	APIVersion string    `json:"apiVersion"`
	Kind       string    `json:"kind"`
	Layout     string    `json:"layout"`
	Version    int       `json:"version"`
	CreatedAt  time.Time `json:"createdAt"`
}
