package statebackend

import (
	"context"
	"errors"
	"fmt"
	"os"
	"unicode/utf8"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/remotestate"
)

// logChunkSafeBytes bounds a single log-append chunk below the server's 1 MiB
// per-chunk budget (LOG_CHUNK_MAX_BYTES), leaving headroom for the JSON envelope.
const logChunkSafeBytes = 900 * 1024

// RemoteStateBackend implements Backend over the v1 state API (Orun Cloud and
// the OSS single-tenant backend speak the same contract).
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

// wireRunID maps a CLI execution id to the contract's run ULID. Every wire call
// goes through this so the run identity is stable across invocations (the same
// execId resumes the same run).
func wireRunID(runID string) string { return remotestate.RunULID(runID) }

func (r *RemoteStateBackend) InitRun(ctx context.Context, plan *model.Plan, opts InitRunOptions) (*RunHandle, error) {
	bp := remotestate.ConvertPlan(plan)

	// The plan ships through the object plane first: serialize the plan blob,
	// upload it if the object plane is missing it (digest negotiation), then
	// reference it by digest on the run. InitRun stays cheap and idempotent.
	blob, digest, err := remotestate.PlanBlob(bp)
	if err != nil {
		return nil, fmt.Errorf("init run: %w", err)
	}
	if _, err := r.client.EnsureObject(ctx, remotestate.ObjectKindPlan, blob); err != nil {
		return nil, fmt.Errorf("init run: syncing plan object: %w", err)
	}

	jobs := make([]remotestate.PlanJobInput, 0, len(bp.Jobs))
	for _, j := range bp.Jobs {
		jobs = append(jobs, remotestate.PlanJobInput{
			JobID:     j.JobID,
			Component: j.Component,
			Deps:      j.Deps,
		})
	}

	source := opts.Source
	if source == "" {
		source = "cli"
	}
	labels := opts.Labels
	if opts.DryRun {
		if labels == nil {
			labels = map[string]string{}
		}
		labels["dryRun"] = "true"
	}

	req := remotestate.CreateRunRequest{
		RunID:       wireRunID(opts.RunID),
		PlanDigest:  digest,
		Source:      source,
		Environment: opts.Environment,
		Git: remotestate.GitProvenance{
			Commit: opts.GitCommit,
			Ref:    opts.GitRef,
			Dirty:  opts.GitDirty,
		},
		Labels: labels,
		Jobs:   jobs,
	}
	if _, err := r.client.CreateRun(ctx, req); err != nil {
		return nil, fmt.Errorf("init run: %w", err)
	}
	// Return the local execId as the handle: subsequent calls pass it back and
	// we re-derive the wire ULID, so local state/object-model pathing keyed on
	// the execId is undisturbed.
	return &RunHandle{RunID: opts.RunID}, nil
}

func (r *RemoteStateBackend) ClaimJob(ctx context.Context, runID string, job model.PlanJob, runnerID string) (*ClaimResult, error) {
	if runnerID == "" {
		runnerID = r.runnerID
	}
	ulid := wireRunID(runID)
	claim, err := r.client.ClaimJob(ctx, ulid, job.ID, runnerID)
	if err != nil {
		return nil, err
	}
	res := &ClaimResult{}
	if claim.Claimed {
		res.Claimed = true
		res.Takeover = claim.Attempt > 1
		res.LeaseExpiresAt = claim.LeaseExpiresAt
		res.LeaseSeconds = claim.LeaseSeconds
		res.HeartbeatIntervalSeconds = claim.HeartbeatIntervalSeconds
		return res, nil
	}
	// Map the structured refusal reason onto the legacy ClaimResult fields the
	// runner branches on.
	switch claim.Reason {
	case "deps_not_ready":
		res.DepsWaiting = true
	case "already_claimed":
		// Held by another runner: report it as running so the caller waits/skips.
		res.CurrentStatus = "running"
	case "terminal":
		// The job (or the whole run) is terminal. Read the job back to tell a
		// completed job (skip) from a failed/aborted one (error).
		res.CurrentStatus = r.classifyTerminal(ctx, ulid, job.ID)
	}
	return res, nil
}

// classifyTerminal resolves a "terminal" claim refusal to "success" (the job
// completed — caller may skip) or "failed" (the job failed, or the run ended
// without it). Defaults to "failed" when ambiguous so the caller stops rather
// than hangs.
// EnsureObject uploads a CAS object (idempotent, digest-negotiated) and returns
// its content digest. Exposed for adapters that wrap this backend (CoordBackend)
// and need to push a `job-result` before reporting a memoizable completion.
func (r *RemoteStateBackend) EnsureObject(ctx context.Context, kind string, blob []byte) (string, error) {
	return r.client.EnsureObject(ctx, kind, blob)
}

// ClassifyTerminal returns "success" or "failed" for a terminal job, reading the
// job back from the read model. Exposed for adapters that wrap this backend
// (CoordBackend) and need to distinguish a completed job from a failed one when
// the native :claim reports run_terminal. Takes the CLI run id (mapped to the
// run ULID internally).
func (r *RemoteStateBackend) ClassifyTerminal(ctx context.Context, runID, jobID string) string {
	return r.classifyTerminal(ctx, wireRunID(runID), jobID)
}

func (r *RemoteStateBackend) classifyTerminal(ctx context.Context, ulid, jobID string) string {
	jobs, err := r.client.ListJobs(ctx, ulid)
	if err != nil {
		return "failed"
	}
	for _, j := range jobs {
		if j.JobID == jobID {
			if remotestate.BackendJobStatusToLocal(j.Status) == "completed" {
				return "success"
			}
			return "failed"
		}
	}
	return "failed"
}

func (r *RemoteStateBackend) Heartbeat(ctx context.Context, runID string, jobID string, runnerID string) (*HeartbeatResult, error) {
	if runnerID == "" {
		runnerID = r.runnerID
	}
	info, err := r.client.Heartbeat(ctx, wireRunID(runID), jobID, runnerID)
	if err != nil {
		var apiErr *remotestate.APIError
		if errors.As(err, &apiErr) && apiErr.IsLeaseLost() {
			return &HeartbeatResult{OK: false, LeaseLost: true}, err
		}
		return &HeartbeatResult{OK: false}, err
	}
	return &HeartbeatResult{
		OK:                       true,
		LeaseExpiresAt:           info.LeaseExpiresAt,
		LeaseSeconds:             info.LeaseSeconds,
		HeartbeatIntervalSeconds: info.HeartbeatIntervalSeconds,
	}, nil
}

func (r *RemoteStateBackend) UpdateJob(ctx context.Context, runID string, jobID string, runnerID string, status JobStatus, errText string) error {
	if runnerID == "" {
		runnerID = r.runnerID
	}
	wireStatus := "failed"
	if status == JobStatusSuccess {
		wireStatus = "succeeded"
	}
	return r.client.UpdateJob(ctx, wireRunID(runID), jobID, runnerID, wireStatus, errText)
}

func (r *RemoteStateBackend) AppendStepLog(ctx context.Context, runID string, jobID string, content string) error {
	if content == "" {
		return nil
	}
	ulid := wireRunID(runID)
	for _, chunk := range chunkUTF8(content, logChunkSafeBytes) {
		if _, err := r.client.AppendLog(ctx, ulid, jobID, r.runnerID, chunk); err != nil {
			return err
		}
	}
	return nil
}

func (r *RemoteStateBackend) RunnableJobs(ctx context.Context, runID string) ([]string, error) {
	jobs, err := r.client.ListRunnable(ctx, wireRunID(runID))
	if err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(jobs))
	for _, j := range jobs {
		ids = append(ids, j.JobID)
	}
	return ids, nil
}

func (r *RemoteStateBackend) LoadRunState(ctx context.Context, runID string) (*execmodel.ExecState, *execmodel.ExecMetadata, error) {
	ulid := wireRunID(runID)
	run, err := r.client.GetRun(ctx, ulid)
	if err != nil {
		return nil, nil, fmt.Errorf("load run state: %w", err)
	}
	jobs, err := r.client.ListJobs(ctx, ulid)
	if err != nil {
		return nil, nil, fmt.Errorf("list jobs for run %s: %w", runID, err)
	}

	st := &execmodel.ExecState{
		ExecID:       runID,
		PlanChecksum: run.PlanDigest,
		Jobs:         make(map[string]*execmodel.JobState, len(jobs)),
	}
	for _, j := range jobs {
		js := &execmodel.JobState{
			Status: remotestate.BackendJobStatusToLocal(j.Status),
			Steps:  map[string]string{},
		}
		if j.StartedAt != nil {
			js.StartedAt = *j.StartedAt
		}
		if j.FinishedAt != nil {
			js.FinishedAt = *j.FinishedAt
		}
		if j.ErrorText != nil {
			js.LastError = *j.ErrorText
		}
		// The lease expiry is the v1 liveness signal (there is no separate
		// heartbeatAt); the dep-wait logic reads it as the freshness proxy.
		if j.LeaseExpiresAt != nil {
			js.HeartbeatAt = *j.LeaseExpiresAt
		}
		st.Jobs[j.JobID] = js
	}

	finishedAt := ""
	if run.FinishedAt != nil {
		finishedAt = *run.FinishedAt
	}
	user := run.CreatedBy.DisplayName
	if user == "" {
		user = run.CreatedBy.ID
	}
	total := run.JobCounts.Queued + run.JobCounts.Running + run.JobCounts.Succeeded + run.JobCounts.Failed

	meta := &execmodel.ExecMetadata{
		ExecID:     runID,
		PlanID:     run.PlanDigest,
		StartedAt:  run.CreatedAt,
		FinishedAt: finishedAt,
		Status:     localRunStatus(run.Status),
		Trigger:    run.Source,
		User:       user,
		JobTotal:   total,
		JobDone:    run.JobCounts.Succeeded,
		JobFailed:  run.JobCounts.Failed,
	}

	return st, meta, nil
}

// localRunStatus maps a v1 run status to the local status vocabulary the
// status/cockpit viewmodels render.
func localRunStatus(s string) string {
	switch s {
	case "succeeded":
		return "completed"
	default:
		return s
	}
}

func (r *RemoteStateBackend) ReadJobLog(ctx context.Context, runID string, jobID string) (string, error) {
	res, err := r.client.ReadLog(ctx, wireRunID(runID), jobID, 0)
	if err != nil {
		return "", err
	}
	return res.Content, nil
}

func (r *RemoteStateBackend) TailJobLog(ctx context.Context, runID string, jobID string, fromSeq int) (string, int, bool, error) {
	res, err := r.client.ReadLog(ctx, wireRunID(runID), jobID, fromSeq)
	if err != nil {
		return "", fromSeq, false, err
	}
	return res.Content, res.NextSeq, res.Complete, nil
}

func (r *RemoteStateBackend) Close(_ context.Context) error { return nil }

// chunkUTF8 splits s into chunks no larger than max bytes, never splitting a
// multi-byte rune. Used to keep each log append within the server's per-chunk
// budget.
func chunkUTF8(s string, max int) []string {
	if len(s) <= max {
		return []string{s}
	}
	var chunks []string
	for len(s) > max {
		cut := max
		// Back off to a rune boundary so a chunk never ends mid-rune.
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		if cut == 0 {
			cut = max // pathological (no boundary found); force a hard split
		}
		chunks = append(chunks, s[:cut])
		s = s[cut:]
	}
	if len(s) > 0 {
		chunks = append(chunks, s)
	}
	return chunks
}
