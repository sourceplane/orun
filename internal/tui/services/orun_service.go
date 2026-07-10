// Package services defines the OrunService boundary between the TUI and
// Orun internal packages, plus all request/response and tea.Msg types.
//
// All implementations must avoid shelling out to the `orun` binary; they
// invoke the same internal packages the CLI commands use.
package services

import (
	"context"
	"time"

	"github.com/sourceplane/orun/internal/model"
)

// OrunService is the single boundary between TUI views and Orun internals.
// All methods are safe to call from a tea.Cmd goroutine.
type OrunService interface {
	// LoadWorkspace discovers the intent root and returns a snapshot of all
	// components, environments, and saved plans. Called on startup and on
	// ctrl+r.
	LoadWorkspace(ctx context.Context, req WorkspaceRequest) (*WorkspaceSnapshot, error)

	// GeneratePlan compiles a plan from the current intent using the planner.
	// Never shells out; calls internal/planner directly.
	GeneratePlan(ctx context.Context, req PlanRequest) (*PlanResult, error)

	// RunPlan executes a compiled plan and streams RunEvents on the returned
	// channel. The channel is closed when the run completes or the context is
	// cancelled.
	RunPlan(ctx context.Context, req RunRequest) (<-chan RunEvent, error)

	// ListRuns returns execution summaries from the local state store (or
	// remote backend when RemoteState is set).
	ListRuns(ctx context.Context, req ListRunsRequest) ([]RunSummary, error)

	// GetRunDetail loads the compiled plan and per-job statuses for a single
	// execution so the Activity drilldown can enumerate steps and tail logs for
	// completed/historical runs (whose plan is not carried by ListRuns). It is
	// best-effort: a run whose plan blob is gone yields a RunDetail with a nil
	// Plan rather than an error.
	GetRunDetail(ctx context.Context, req RunDetailRequest) (RunDetail, error)

	// Describe returns structured detail for any resource ref (component,
	// job, plan, run).
	Describe(ctx context.Context, ref ResourceRef) (*ResourceDescription, error)

	// TailLogs streams log lines for a job/step. The channel is closed when
	// the log file ends (or the context is cancelled for live tailing).
	TailLogs(ctx context.Context, req LogRequest) (<-chan LogEvent, error)

	// RefreshCatalog resolves the workspace into the object-model catalog when
	// it is stale for the current tree (or force=true), under a source-hash
	// staleness gate + a non-blocking try-lock. Best-effort: a missing
	// workspace / object-model root is a no-op (zero result, no error).
	RefreshCatalog(ctx context.Context, force bool) (CatalogRefreshResult, error)

	// CatalogStale reports whether the object-model catalog was resolved against
	// a different source than the workspace currently has (so a refresh would
	// change it). Read-only and cheap (one source probe, no resolve). A missing
	// object-model root reports not-stale.
	CatalogStale(ctx context.Context) (bool, error)

	// LoadCatalog reads the multi-kind entity view of the object-model catalog
	// at catalogs/current (components, derived entities, typed relations,
	// per-kind counts) for the Catalog surface. Best-effort: an absent or
	// unreadable object model returns (nil, nil) so the surface renders its
	// empty state rather than an error.
	LoadCatalog(ctx context.Context) (*CatalogSnapshot, error)

	// LoadAgentTypes reads the workspace's AgentType catalog entities
	// (orun-agents AG1) for the Agent surface. Best-effort: an absent object
	// model or catalog returns (nil, nil).
	LoadAgentTypes(ctx context.Context) ([]AgentTypeRow, error)

	// LiveSessions lists the live session bodies on this machine
	// (orun-agents-live AL3) for the Agent surface's sessions sidebar.
	// Best-effort: an absent registry returns (nil, nil).
	LiveSessions() ([]LiveSessionRow, error)
}

// --- Request types ---

// WorkspaceRequest configures workspace discovery.
type WorkspaceRequest struct {
	IntentFile string // auto-discovered if empty
	ConfigDir  string
	All        bool // disable CWD scoping
}

// PlanRequest is the scope for plan generation.
type PlanRequest struct {
	IntentFile  string
	ConfigDir   string
	Components  []string
	Environment string
	ChangedOnly bool
	BaseBranch  string
	HeadRef     string
	TriggerName string
	NamedPlan   string
}

// RunRequest is the scope for a plan execution.
type RunRequest struct {
	Plan        *model.Plan
	ExecID      string
	DryRun      bool
	JobID       string // empty = run all
	Components  []string
	Concurrency int
	WorkDir     string
	RemoteState bool
	BackendURL  string
}

// ListRunsRequest configures execution history retrieval.
type ListRunsRequest struct {
	Limit       int
	RemoteState bool
	BackendURL  string
}

// RunDetailRequest identifies the execution whose plan + statuses to load.
type RunDetailRequest struct {
	ExecID      string
	RemoteState bool
	BackendURL  string
}

// RunDetail carries the per-run data the Activity drilldown needs beyond the
// History summary: the compiled plan (for job/step enumeration), the resolved
// per-job status map, and per-step execution records (status + timing). Plan is
// nil when the plan blob is unavailable.
type RunDetail struct {
	ExecID   string
	Plan     *model.Plan
	Statuses map[string]string   // jobID -> legacy status
	Steps    map[string]StepInfo // "<jobID>\x00<stepID>" -> execution record
}

// StepInfo is one step's execution record, projected from the object graph.
// Duration is zero when the step has not finished (or timing is unavailable).
type StepInfo struct {
	Status   string // legacy: completed | failed | running | pending
	Duration time.Duration
	ExitCode int
}

// StepDetailKey is the canonical key for RunDetail.Steps / ActivityRun step
// lookups. The NUL separator avoids collisions between job and step ids.
func StepDetailKey(jobID, stepID string) string {
	return jobID + "\x00" + stepID
}

// LogRequest configures a log tail.
type LogRequest struct {
	ExecID      string
	JobID       string
	StepID      string // empty = all steps for the job
	Follow      bool   // tail live logs
	RemoteState bool
	BackendURL  string
}

// ResourceRef identifies any resource the TUI can describe.
type ResourceRef struct {
	Kind string // "component" | "job" | "plan" | "run"
	Name string
	Env  string
}

// --- Response / data types ---

// WorkspaceSnapshot is the read-only view of the current intent root.
type WorkspaceSnapshot struct {
	IntentRoot   string
	IntentName   string
	IntentFile   string
	Components   []ComponentSummary
	Environments []string
	Plans        []PlanSummary
	Source       SourceInfo
	LoadedAt     time.Time
}

// SourceInfo summarizes the workspace's VCS context for the cockpit header
// (branch, short head revision, tree cleanliness). Zero value when the
// workspace has no git context.
type SourceInfo struct {
	Repo   string
	Branch string
	Head   string // short revision at HEAD
	Dirty  bool   // uncommitted catalog-relevant changes
}

// ComponentSummary is the row-level view of a component for the Browse view.
type ComponentSummary struct {
	Name      string
	Type      string
	Domain    string
	Path      string
	Envs      []string // subscribed environments
	Profile   string   // default profile
	DependsOn []string
	Watches   []string // spec.change.watches — the intent sections this component tracks
	Changed   bool
	// ChangeKind is the Q2 affected-overlay classification for this component:
	// "changed" (its own inputs changed), "affected" (a transitive dependency
	// changed), or "" (unaffected). Changed == (ChangeKind != "").
	ChangeKind    string
	LastRunStatus string // "success" | "failed" | "running" | ""

	// Envelope enrichment (orun-service-catalog SC1/SC1b) — populated when the
	// component list is served from the object-model catalog; empty on the live
	// intent-loader path (the parity guard compares the intent-derived fields
	// above only).
	Owner       string // ownership.owner entity ref (group:x / user:y)
	OwnerSource string // "authored" | "codeowners" | "inherited" | ""
	System      string // partOf System name
	Stage       string // lifecycle.stage
	Tier        string // lifecycle.tier
}

// PlanSummary is the row-level view of a saved plan.
type PlanSummary struct {
	Checksum    string
	Name        string
	GeneratedAt time.Time
	JobCount    int
	Components  []string
}

// PlanResult is returned by GeneratePlan.
type PlanResult struct {
	Plan        *model.Plan
	Checksum    string
	JobCount    int
	Components  []string
	Warnings    []string
	GeneratedAt time.Time
}

// RunEventKind enumerates streaming run event types.
type RunEventKind string

const (
	RunEventJobStarted    RunEventKind = "job_started"
	RunEventJobCompleted  RunEventKind = "job_completed"
	RunEventJobFailed     RunEventKind = "job_failed"
	RunEventStepStarted   RunEventKind = "step_started"
	RunEventStepCompleted RunEventKind = "step_completed"
	RunEventRunDone       RunEventKind = "run_done"
)

// RunEvent is a single event from a streaming run.
type RunEvent struct {
	Kind RunEventKind
	// ExecID is the resolved execution ID for this run. It is stamped on
	// every event the service emits so the TUI can scope live log tailing
	// to the in-flight run without a separate round-trip. Empty only for
	// the synthetic terminal event produced when the run channel is dropped.
	ExecID    string
	JobID     string
	StepID    string
	Component string
	Env       string
	Status    string
	Error     string
	Timestamp time.Time
}

// LogEvent is a single line from a log tail.
type LogEvent struct {
	JobID     string
	StepID    string
	Line      string
	IsError   bool
	Timestamp time.Time
}

// RunSummary is one row in the History view.
type RunSummary struct {
	ExecID     string
	PlanID     string
	PlanName   string
	Status     string
	JobTotal   int
	JobDone    int
	JobFailed  int
	StartedAt  time.Time
	FinishedAt *time.Time
	Duration   time.Duration
	Trigger    string
	DryRun     bool
	// Components is the set of component names this run touched. Populated
	// best-effort by ListRuns (resolved from the saved plan). May be empty
	// for legacy runs whose plan is no longer on disk — consumers should
	// fall back to substring matching on PlanName in that case.
	Components []string
}

// CatalogSnapshot is the multi-kind entity view of the object-model catalog
// rendered by the Catalog surface (orun-service-catalog data-model.md §2–§4).
// Entities carries every catalog member — components and derived kinds — in a
// single uniform projection; Relations is the typed relation graph
// (relations.json) with forward edges only.
type CatalogSnapshot struct {
	HumanKey     string         // catalog display key (cat-…)
	CountsByKind map[string]int // per-kind entity counts
	Entities     []EntitySummary
	Relations    []RelationSummary
	LoadedAt     time.Time
}

// EntitySummary is the row-level projection of one catalog entity of any kind.
// Kind-specific fields are zero for kinds they don't apply to.
type EntitySummary struct {
	Kind      string // Component | API | Resource | System | Domain | Group | User | Composition | Environment | Deployment
	EntityKey string // <namespace>/<repo>/<name>
	Name      string
	Namespace string
	Repo      string

	// Component envelope projections.
	Type        string
	Domain      string
	System      string
	Owner       string
	OwnerSource string
	Stage       string
	Tier        string
	Envs        []string

	// Derived-entity membership (SC: spec.members).
	MemberCount int
	Members     []string // referencing component keys, sorted

	// Composition versioning.
	Version   string
	Lifecycle string
}

// RelationSummary is one forward edge of the catalog-wide typed relation graph.
type RelationSummary struct {
	From     string
	FromKind string
	Type     string
	To       string
	ToKind   string
	Optional bool
	Include  string
}

// ResourceDescription is the structured payload rendered by the Inspector.
type ResourceDescription struct {
	Kind    string
	Name    string
	Summary string
	Fields  []DescField
	Actions []string
}

// DescField is one labelled value in a ResourceDescription.
type DescField struct {
	Label string
	Value string
	Dim   bool
}

// --- tea.Msg types ---

// WorkspaceLoadedMsg is dispatched after LoadWorkspace completes.
type WorkspaceLoadedMsg struct {
	Snapshot *WorkspaceSnapshot
	Err      error
}

// PlanGeneratedMsg is dispatched after GeneratePlan completes.
type PlanGeneratedMsg struct {
	Result *PlanResult
	Err    error
}

// RunEventMsg wraps a streaming RunEvent.
type RunEventMsg struct {
	Event RunEvent
}

// LogBatchMsg carries one coalesced batch of streamed LogEvents. Stream is
// the tail's generation id (events.NextLogStream): consumers discard batches
// whose Stream doesn't match their currently attached tail, so a superseded
// pump or a cancelled stream's close-sentinel can never contaminate the live
// view. Closed reports that the channel ended (possibly alongside trailing
// events).
type LogBatchMsg struct {
	Stream int64
	Events []LogEvent
	Closed bool
}

// CatalogLoadedMsg is dispatched after LoadCatalog completes. A nil Snapshot
// with a nil Err means no catalog exists yet (the Catalog surface shows its
// empty state and prompts a refresh).
type CatalogLoadedMsg struct {
	Snapshot *CatalogSnapshot
	Err      error
}

// RunsListedMsg is dispatched after ListRuns completes.
type RunsListedMsg struct {
	Runs []RunSummary
	Err  error
}

// RunDetailLoadedMsg is dispatched after GetRunDetail completes. The Activity
// model merges Plan/Statuses into the matching run so its drilldown can show
// steps and tail logs.
type RunDetailLoadedMsg struct {
	ExecID   string
	Plan     *model.Plan
	Statuses map[string]string
	Steps    map[string]StepInfo
	Err      error
}

// DescribeResultMsg is dispatched after Describe completes.
type DescribeResultMsg struct {
	Desc *ResourceDescription
	Err  error
}

// ErrMsg propagates an out-of-band error to the root model.
type ErrMsg struct {
	Err error
}

// TickMsg drives time-based polling.
type TickMsg struct{}
