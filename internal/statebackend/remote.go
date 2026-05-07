package statebackend

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/state"
)

// RemoteStateBackend implements Backend using the orun-backend HTTP API.
type RemoteStateBackend struct {
	client   *remotestate.Client
	runnerID string
}

// NewRemoteStateBackend creates a RemoteStateBackend with the given HTTP client
// and runner identifier. runnerID should be unique per runner instance.
func NewRemoteStateBackend(client *remotestate.Client, runnerID string) *RemoteStateBackend {
	return &RemoteStateBackend{client: client, runnerID: runnerID}
}

// DeriveRunnerID builds a stable runner identifier from the environment.
// In GitHub Actions it includes the GITHUB_JOB and GITHUB_RUN_ID so that
// concurrent matrix jobs have distinct identifiers.
func DeriveRunnerID() string {
	if os.Getenv("GITHUB_ACTIONS") == "true" {
		job := os.Getenv("GITHUB_JOB")
		runID := os.Getenv("GITHUB_RUN_ID")
		attempt := os.Getenv("GITHUB_RUN_ATTEMPT")
		if attempt == "" {
			attempt = "1"
		}
		if job != "" && runID != "" {
			return fmt.Sprintf("gha-%s-%s-%s", runID, attempt, job)
		}
	}
	// Fallback to hostname + PID for uniqueness.
	host, _ := os.Hostname()
	pid := os.Getpid()
	if host == "" {
		host = "runner"
	}
	return fmt.Sprintf("%s-%d", host, pid)
}

func (r *RemoteStateBackend) InitRun(ctx context.Context, plan *model.Plan, opts InitRunOptions) (*RunHandle, error) {
	bp := remotestate.ConvertPlan(plan)
	triggerType := opts.TriggerType
	if triggerType == "" {
		triggerType = "ci"
	}
	req := remotestate.CreateRunRequest{
		Plan:         bp,
		RunID:        opts.RunID,
		NamespaceID:  opts.NamespaceID,
		RepoFullName: opts.RepoFullName,
		DryRun:       opts.DryRun,
		TriggerType:  triggerType,
		Actor:        opts.Actor,
	}
	resp, err := r.client.CreateRun(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("init run: %w", err)
	}
	return &RunHandle{RunID: resp.RunID}, nil
}

func (r *RemoteStateBackend) ClaimJob(ctx context.Context, runID string, job model.PlanJob, runnerID string) (*ClaimResult, error) {
	if runnerID == "" {
		runnerID = r.runnerID
	}
	resp, err := r.client.ClaimJob(ctx, runID, job.ID, runnerID)
	if err != nil {
		return nil, err
	}
	return &ClaimResult{
		Claimed:       resp.Claimed,
		Takeover:      resp.Takeover,
		CurrentStatus: resp.CurrentStatus,
		DepsWaiting:   resp.DepsWaiting,
		DepsBlocked:   resp.DepsBlocked,
	}, nil
}

func (r *RemoteStateBackend) Heartbeat(ctx context.Context, runID string, jobID string, runnerID string) (*HeartbeatResult, error) {
	if runnerID == "" {
		runnerID = r.runnerID
	}
	if err := r.client.Heartbeat(ctx, runID, jobID, runnerID); err != nil {
		return &HeartbeatResult{OK: false}, err
	}
	return &HeartbeatResult{OK: true}, nil
}

func (r *RemoteStateBackend) UpdateJob(ctx context.Context, runID string, jobID string, runnerID string, status JobStatus, errText string) error {
	if runnerID == "" {
		runnerID = r.runnerID
	}
	return r.client.UpdateJob(ctx, runID, jobID, runnerID, string(status), errText)
}

func (r *RemoteStateBackend) AppendStepLog(ctx context.Context, runID string, jobID string, content string) error {
	if strings.TrimSpace(content) == "" {
		return nil
	}
	return r.client.UploadLog(ctx, runID, jobID, content)
}

func (r *RemoteStateBackend) RunnableJobs(ctx context.Context, runID string) ([]string, error) {
	return r.client.GetRunnable(ctx, runID)
}

func (r *RemoteStateBackend) LoadRunState(ctx context.Context, runID string) (*state.ExecState, *state.ExecMetadata, error) {
	run, err := r.client.GetRun(ctx, runID)
	if err != nil {
		return nil, nil, fmt.Errorf("load run state: %w", err)
	}
	jobs, err := r.client.ListJobs(ctx, runID)
	if err != nil {
		return nil, nil, fmt.Errorf("list jobs for run %s: %w", runID, err)
	}

	st := &state.ExecState{
		ExecID:       runID,
		PlanChecksum: run.PlanChecksum,
		Jobs:         make(map[string]*state.JobState, len(jobs)),
	}
	for _, j := range jobs {
		localStatus := remotestate.BackendJobStatusToLocal(j.Status)
		js := &state.JobState{
			Status: localStatus,
			Steps:  map[string]string{},
		}
		if j.StartedAt != nil {
			js.StartedAt = *j.StartedAt
		}
		if j.FinishedAt != nil {
			js.FinishedAt = *j.FinishedAt
		}
		if j.LastError != nil {
			js.LastError = *j.LastError
		}
		if j.HeartbeatAt != nil {
			js.HeartbeatAt = *j.HeartbeatAt
		}
		st.Jobs[j.JobID] = js
	}

	actor := ""
	if run.Actor != nil {
		actor = *run.Actor
	}
	finishedAt := ""
	if run.FinishedAt != nil {
		finishedAt = *run.FinishedAt
	}

	// Map backend run status to local status vocabulary.
	localRunStatus := run.Status
	switch run.Status {
	case "completed", "cancelled":
		localRunStatus = "completed"
	}

	meta := &state.ExecMetadata{
		ExecID:     runID,
		PlanID:     run.PlanChecksum,
		StartedAt:  run.CreatedAt,
		FinishedAt: finishedAt,
		Status:     localRunStatus,
		Trigger:    run.TriggerType,
		User:       actor,
		DryRun:     run.DryRun,
		JobTotal:   run.JobTotal,
		JobDone:    run.JobDone,
		JobFailed:  run.JobFailed,
	}

	return st, meta, nil
}

func (r *RemoteStateBackend) ReadJobLog(ctx context.Context, runID string, jobID string) (string, error) {
	return r.client.GetLog(ctx, runID, jobID)
}

func (r *RemoteStateBackend) Close(_ context.Context) error { return nil }
