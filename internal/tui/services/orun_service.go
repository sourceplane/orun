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

	// Describe returns structured detail for any resource ref (component,
	// job, plan, run).
	Describe(ctx context.Context, ref ResourceRef) (*ResourceDescription, error)

	// TailLogs streams log lines for a job/step. The channel is closed when
	// the log file ends (or the context is cancelled for live tailing).
	TailLogs(ctx context.Context, req LogRequest) (<-chan LogEvent, error)
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
	LoadedAt     time.Time
}

// ComponentSummary is the row-level view of a component for the Browse view.
type ComponentSummary struct {
	Name          string
	Type          string
	Domain        string
	Path          string
	Envs          []string // subscribed environments
	Profile       string   // default profile
	DependsOn     []string
	Changed       bool
	LastRunStatus string // "success" | "failed" | "running" | ""
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

// LogEventMsg wraps a streaming LogEvent.
type LogEventMsg struct {
	Event LogEvent
}

// RunsListedMsg is dispatched after ListRuns completes.
type RunsListedMsg struct {
	Runs []RunSummary
	Err  error
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
