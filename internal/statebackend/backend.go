package statebackend

import (
	"context"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/state"
)

// JobStatus mirrors the backend API terminal status values.
type JobStatus string

const (
	JobStatusSuccess JobStatus = "success"
	JobStatusFailed  JobStatus = "failed"
)

// InitRunOptions carries options for InitRun.
type InitRunOptions struct {
	RunID        string
	NamespaceID  string
	RepoFullName string
	DryRun       bool
	Actor        string
	TriggerType  string
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
}

// HeartbeatResult is returned by Heartbeat.
type HeartbeatResult struct {
	OK bool
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
	LoadRunState(ctx context.Context, runID string) (*state.ExecState, *state.ExecMetadata, error)

	// ReadJobLog retrieves the combined log for a job.
	ReadJobLog(ctx context.Context, runID string, jobID string) (string, error)

	// Close releases any resources held by the backend.
	Close(ctx context.Context) error
}
