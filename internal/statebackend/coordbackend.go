package statebackend

import (
	"context"
	"encoding/json"
	"sort"
	"sync"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
)

// HermeticLabel opts a job into memoization (coordination-api.md §1: hermetic is
// opt-in, never required for correctness). A job carrying this label asserts its
// result is a pure function of its declared inputs, so a later run with the same
// jobInputHash may adopt the prior result and skip execution.
const HermeticLabel = "orun.dev/hermetic"

// objectKindJobResult is the CAS kind for a completed job's result object.
const objectKindJobResult = "job-result"

func isHermetic(job model.PlanJob) bool { return job.Labels[HermeticLabel] == "true" }

// jobInputHashFor derives the memo key for a job from the inputs the contract
// admits (C5): the resolved steps and the declared env-var KEYS (never values).
// It excludes wall-clock, secrets, and runner identity, so it is stable across
// runs of the same job. (Input-artifact digests and the composition-lock digest
// are not yet threaded through the plan; this is a deterministic subset, and the
// server treats the hash as an opaque key.)
func jobInputHashFor(job model.PlanJob) string {
	envKeys := make([]string, 0, len(job.Env))
	for k := range job.Env {
		envKeys = append(envKeys, k)
	}
	sort.Strings(envKeys)
	return JobInputHash(JobInputHashInput{Steps: job.Steps, EnvKeys: envKeys})
}

// CoordBackend implements Backend over the native v2 coordination wire
// (coordination-api.md §2/§3). The coordination cycle — claim, heartbeat,
// complete, and the runnable frontier — goes through the event-sourced
// CoordClient (the colon-verbs + GET …/frontier the state-worker now exposes).
// Run creation, log append/read, and read-model loads delegate to an inner
// RemoteStateBackend, because the native surface does not yet own those
// (tracked as the §5 /events primitive, log sealing, and a §2-native create).
//
// This is how `orun run` adopts the event-sourced coordination plane without a
// full client rewrite, selected with ORUN_COORDINATION=v2. The lease epoch that
// :claim returns is the conditional-append key for :heartbeat/:complete, so it
// is threaded across calls — kept per job for the single run this backend drives.
type CoordBackend struct {
	coord    *CoordClient
	inner    *RemoteStateBackend
	runnerID string

	mu     sync.Mutex
	leases map[string]int    // jobID -> leaseEpoch
	hashes map[string]string // jobID -> jobInputHash (set when a hermetic job is claimed)
}

var _ Backend = (*CoordBackend)(nil)

// NewCoordBackend wires the native coordination client over an inner backend
// (used for InitRun, logs, and read-model loads).
func NewCoordBackend(coord *CoordClient, inner *RemoteStateBackend, runnerID string) *CoordBackend {
	return &CoordBackend{coord: coord, inner: inner, runnerID: runnerID, leases: map[string]int{}, hashes: map[string]string{}}
}

// InitRun creates/joins the run via the inner backend. When the server runs the
// DO coordination backend, that create seeds the per-run shard, so the native
// verbs below operate on a DO-backed run.
func (b *CoordBackend) InitRun(ctx context.Context, plan *model.Plan, opts InitRunOptions) (*RunHandle, error) {
	return b.inner.InitRun(ctx, plan, opts)
}

// ClaimJob posts a §3 :claim and maps the native outcome onto the ClaimResult
// fields the runner branches on, stashing the lease epoch for later verbs.
func (b *CoordBackend) ClaimJob(ctx context.Context, runID string, job model.PlanJob, runnerID string) (*ClaimResult, error) {
	if runnerID == "" {
		runnerID = b.runnerID
	}
	req := ClaimRequest{RunnerID: runnerID}
	if isHermetic(job) {
		// Memoizable: send the input-hash KEY so the server can resolve a prior
		// result (the digest is the server's to choose, never the client's), and
		// remember it for the result push at completion.
		h := jobInputHashFor(job)
		b.setHash(job.ID, h)
		req.Hermetic = true
		req.JobInputHash = h
	}
	out, err := b.coord.Claim(ctx, wireRunID(runID), job.ID, req)
	if err != nil {
		return nil, err
	}
	res := &ClaimResult{}
	switch out.Kind {
	case OutcomeClaimed:
		b.setLease(job.ID, out.LeaseEpoch)
		res.Claimed = true
		res.Takeover = out.Attempt > 1
		res.LeaseExpiresAt = out.LeaseExpiresAt
		res.LeaseSeconds = out.LeaseSeconds
		res.HeartbeatIntervalSeconds = out.HeartbeatIntervalSeconds
	case OutcomeCached:
		// Memoized hit: the server resolved a prior result for this job's inputs.
		// Adopt-by-skip — report it as already-complete so the run loop treats it
		// exactly like a job finished elsewhere and does not execute it.
		res.CurrentStatus = "success"
	case OutcomeRejected:
		switch out.Reason {
		case "deps_not_ready":
			res.DepsWaiting = true
		case "job_held":
			// Held by another runner: report it as running so the caller waits/skips.
			res.CurrentStatus = "running"
		case "run_terminal", "terminal":
			res.CurrentStatus = b.inner.ClassifyTerminal(ctx, runID, job.ID)
		}
	}
	return res, nil
}

// Heartbeat renews the lease via :heartbeat, using the epoch stashed at claim.
func (b *CoordBackend) Heartbeat(ctx context.Context, runID, jobID, runnerID string) (*HeartbeatResult, error) {
	if runnerID == "" {
		runnerID = b.runnerID
	}
	leaseLost, err := b.coord.Heartbeat(ctx, wireRunID(runID), jobID, runnerID, b.lease(jobID))
	if err != nil {
		return nil, err
	}
	return &HeartbeatResult{OK: !leaseLost, LeaseLost: leaseLost}, nil
}

// UpdateJob reports the terminal transition via :complete. A lost lease means
// another runner already drove the job terminal — the append is superseded, not
// an error (at-least-once execution; steps are idempotent).
func (b *CoordBackend) UpdateJob(ctx context.Context, runID, jobID, runnerID string, status JobStatus, errText string) error {
	if runnerID == "" {
		runnerID = b.runnerID
	}
	outcome := "succeeded"
	if status == JobStatusFailed {
		outcome = "failed"
	}
	req := CompleteRequest{
		RunnerID:   runnerID,
		LeaseEpoch: b.lease(jobID),
		Outcome:    outcome,
		ErrorText:  errText,
	}
	// For a hermetic success, push a `job-result` object and report its digest +
	// the input-hash key so the server indexes jobInputHash → resultDigest and a
	// later run with the same inputs is served from cache.
	if hash := b.hash(jobID); hash != "" && status == JobStatusSuccess {
		result := JobResult{JobInputHash: hash, Outputs: []string{}, Exit: 0}
		if blob, mErr := json.Marshal(result); mErr == nil {
			if digest, oErr := b.inner.EnsureObject(ctx, objectKindJobResult, blob); oErr == nil {
				req.JobInputHash = hash
				req.ResultDigest = digest
			}
			// A push failure is non-fatal: the completion still records; the job
			// simply won't be memoized for the next run.
		}
	}
	if _, err := b.coord.Complete(ctx, wireRunID(runID), jobID, req); err != nil {
		return err
	}
	return nil
}

// RunnableJobs reads the run's runnable frontier from the event-sourced fold.
func (b *CoordBackend) RunnableJobs(ctx context.Context, runID string) ([]string, error) {
	return b.coord.Frontier(ctx, wireRunID(runID))
}

// ── Delegated to the inner backend (native surface does not yet own these) ──

func (b *CoordBackend) AppendStepLog(ctx context.Context, runID, jobID, content string) error {
	return b.inner.AppendStepLog(ctx, runID, jobID, content)
}

func (b *CoordBackend) LoadRunState(ctx context.Context, runID string) (*execmodel.ExecState, *execmodel.ExecMetadata, error) {
	return b.inner.LoadRunState(ctx, runID)
}

func (b *CoordBackend) ReadJobLog(ctx context.Context, runID, jobID string) (string, error) {
	return b.inner.ReadJobLog(ctx, runID, jobID)
}

func (b *CoordBackend) TailJobLog(ctx context.Context, runID, jobID string, fromSeq int) (string, int, bool, error) {
	return b.inner.TailJobLog(ctx, runID, jobID, fromSeq)
}

func (b *CoordBackend) Close(ctx context.Context) error { return b.inner.Close(ctx) }

func (b *CoordBackend) setLease(jobID string, epoch int) {
	b.mu.Lock()
	b.leases[jobID] = epoch
	b.mu.Unlock()
}

func (b *CoordBackend) lease(jobID string) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.leases[jobID]
}

func (b *CoordBackend) setHash(jobID, hash string) {
	b.mu.Lock()
	b.hashes[jobID] = hash
	b.mu.Unlock()
}

func (b *CoordBackend) hash(jobID string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.hashes[jobID]
}
