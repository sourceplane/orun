// Package executionstate owns the ExecutionRun domain model, the writer
// helpers (NextExecutionKey, SanitizeExecID, CreateExecution,
// UpdateSnapshot, MarkTerminal) that persist executions under a revision,
// and the seven-branch ResolveExecution resolver with legacy
// `.orun/executions/` fallback.
//
// Phase 1 / M4 PR-A scope (this package, this PR):
//
//   - model.go — ExecutionRun, RunnerProfile, ExecSummary per
//     data-model.md §5 (byte-stable JSON: stable key order, trailing
//     newline, Kind="ExecutionRun").
//   - writer.go — NextExecutionKey monotonic generator, SanitizeExecID
//     alphabet projection, CreateExecution/UpdateSnapshot/MarkTerminal
//     writers. CreateExecution wires the first real caller of
//     revision.UpdateLatestExecutionSummary, emits an execution-created
//     event (data-model.md §9), and writes refs/latest-execution.json.
//   - resolver.go — ResolveExecution(ctx, store, arg, revHint) with the
//     branch ladder from cli-surface.md §1.4 and compat §3, plus the
//     legacy `.orun/executions/<legacyExecID>/` fallback from compat §4.
//
// The legacy runner-output mirror (bridge.go) was retired at the M12 cutover:
// the runner no longer writes a legacy `.orun/executions/<id>/` file store, so
// there is nothing to mirror. The live run is recorded into the object graph by
// internal/objrun instead.
//
// Dependencies are kept minimal to satisfy the M4 leaf-clean constraint:
// only internal/statestore, internal/triggerctx, internal/revision, the
// stdlib, and the project-pinned oklog/ulid/v2 + pgregory.net/rapid deps
// are imported. `go list -deps ./internal/executionstate` proves the
// invariant.
package executionstate

import "time"

// API constants written verbatim into every persisted ExecutionRun. They
// must remain in lockstep with data-model.md §5.
const (
	APIVersion = "orun.io/v1alpha1"
	KindName   = "ExecutionRun"

	// idPrefixExecution is concatenated with the underlying ULID to form
	// an ExecutionRun.ExecutionID. Mirrors triggerctx's "trg_" /
	// revision's "rev_" conventions.
	idPrefixExecution = "exec_"
)

// Reason values written into ExecutionRun.Reason (data-model.md §5).
const (
	ReasonDirectRun = "direct-run"
	ReasonRerun     = "rerun"
	ReasonRetry     = "retry"
	ReasonMigration = "migration"
)

// Status values written into ExecutionRun.Status (data-model.md §5).
const (
	StatusPending   = "pending"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCancelled = "cancelled"
)

// ExecutionRun is the persisted shape of revisions/<revKey>/executions/<execKey>/
// execution.json (data-model.md §5). JSON tag order MUST match the spec
// byte-for-byte; downstream readers (status command, manifest summary,
// resolver) discriminate on these field names.
//
// StartedAt and FinishedAt are pointer-typed because data-model.md §5 marks
// them omitempty — a zero time.Time would still serialize when the field
// is non-pointer.
type ExecutionRun struct {
	APIVersion         string        `json:"apiVersion"`
	Kind               string        `json:"kind"`
	ExecutionID        string        `json:"executionId"`
	ExecutionKey       string        `json:"executionKey"`
	OriginalKey        string        `json:"originalKey,omitempty"`
	RevisionID         string        `json:"revisionId"`
	RevisionKey        string        `json:"revisionKey"`
	TriggerID          string        `json:"triggerId"`
	TriggerKey         string        `json:"triggerKey"`
	Reason             string        `json:"reason"`
	Status             string        `json:"status"`
	Attempt            int           `json:"attempt"`
	Runner             RunnerProfile `json:"runner"`
	Summary            ExecSummary   `json:"summary"`
	SourceSnapshotKey  string        `json:"sourceSnapshotKey,omitempty"`
	CatalogSnapshotKey string        `json:"catalogSnapshotKey,omitempty"`
	CreatedAt          time.Time     `json:"createdAt"`
	StartedAt          *time.Time    `json:"startedAt,omitempty"`
	FinishedAt         *time.Time    `json:"finishedAt,omitempty"`
}

// RunnerProfile records the runner attached to an execution (data-model.md
// §5). All three fields are required.
type RunnerProfile struct {
	Mode     string `json:"mode"`
	Backend  string `json:"backend"`
	Platform string `json:"platform"`
}

// ExecSummary captures the running tally of jobs across an execution
// (data-model.md §5). Counts are always non-negative; CreateExecution
// stamps the initial value (typically Total=N, Pending=N).
type ExecSummary struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Running   int `json:"running"`
	Pending   int `json:"pending"`
}

// IsTerminal reports whether status is one of the terminal values per
// data-model.md §5. Used by MarkTerminal and the resolver's status
// projections.
func IsTerminal(status string) bool {
	switch status {
	case StatusCompleted, StatusFailed, StatusCancelled:
		return true
	default:
		return false
	}
}
