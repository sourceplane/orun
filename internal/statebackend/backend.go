package statebackend

import (
	"context"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
)

// JobStatus mirrors the backend API terminal status values.
type JobStatus string

const (
	JobStatusSuccess JobStatus = "success"
	JobStatusFailed  JobStatus = "failed"
)

// InitRunOptions carries options for InitRun.
type InitRunOptions struct {
	// RunID is the CLI execution id (an explicit --exec-id, the GitHub
	// gha_<runId>_<attempt> form, or the local fallback). The remote backend
	// maps it deterministically to a contract-valid run ULID, so the same execId
	// resumes the same run (idempotent create / crash recovery).
	RunID       string
	Source      string // "cli" | "ci"
	Environment string // optional environment slug
	GitCommit   string
	GitRef      string
	GitDirty    bool
	Labels      map[string]string
	DryRun      bool
}

// RunHandle is returned by InitRun.
type RunHandle struct {
	RunID string
}

// ClaimResult describes the outcome of a ClaimJob call.
type ClaimResult struct {
	Claimed       bool
	Takeover      bool
	CurrentStatus string // backend job status when not claimed
	DepsWaiting   bool
	DepsBlocked   bool

	// Cached is true when the claim resolved a memoized result: the job's inputs
	// matched a prior run, so the runner adopts the result and skips execution.
	// ResultDigest is the content address of the adopted result (for display).
	Cached       bool
	ResultDigest string

	// Server-supplied lease tunables, populated on a successful claim so the
	// runner never hardcodes the heartbeat cadence (contract §2.2).
	LeaseExpiresAt           string
	LeaseSeconds             int
	HeartbeatIntervalSeconds int
}

// HeartbeatResult is returned by Heartbeat.
type HeartbeatResult struct {
	OK bool
	// LeaseLost is true when the server rejected the heartbeat with 409
	// lease_lost: the lease lapsed or was reassigned and the runner should stop
	// the job. The extended lease fields are populated when OK.
	LeaseLost                bool
	LeaseExpiresAt           string
	LeaseSeconds             int
	HeartbeatIntervalSeconds int
}

// Backend is the state backend seam. Local filesystem state and remote HTTP
// state each implement this interface so the runner and commands can use either
// without embedding transport details.
type Backend interface {
	// InitRun creates or joins a run for the given plan. Returns the run handle.
	InitRun(ctx context.Context, plan *model.Plan, opts InitRunOptions) (*RunHandle, error)

	// ClaimJob attempts to claim a job for this runner. The caller must inspect
	// ClaimResult to decide whether to execute, wait, or exit.
	ClaimJob(ctx context.Context, runID string, job model.PlanJob, runnerID string) (*ClaimResult, error)

	// Heartbeat signals that the runner is still alive for the given job.
	Heartbeat(ctx context.Context, runID string, jobID string, runnerID string) (*HeartbeatResult, error)

	// UpdateJob sends the terminal status for a job.
	UpdateJob(ctx context.Context, runID string, jobID string, runnerID string, status JobStatus, errText string) error

	// AppendStepLog appends step output to the job-level log.
	AppendStepLog(ctx context.Context, runID string, jobID string, content string) error

	// RunnableJobs returns the list of job IDs that are currently runnable for the given run.
	RunnableJobs(ctx context.Context, runID string) ([]string, error)

	// LoadRunState reads run state for display (status/logs commands).
	LoadRunState(ctx context.Context, runID string) (*execmodel.ExecState, *execmodel.ExecMetadata, error)

	// ReadJobLog retrieves the combined log for a job.
	ReadJobLog(ctx context.Context, runID string, jobID string) (string, error)

	// TailJobLog reads a job's log from fromSeq onward, returning the new
	// content, the next-sequence cursor, and whether the job is complete (no more
	// chunks coming). It is the live-tail primitive behind `orun logs --follow`.
	TailJobLog(ctx context.Context, runID string, jobID string, fromSeq int) (content string, nextSeq int, complete bool, err error)

	// Close releases any resources held by the backend.
	Close(ctx context.Context) error
}
