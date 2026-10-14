package statebackend

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/remotestate"
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
//
// Secret references join as "KEY=secret://…" entries: the reference (and any
// @version pin) is content and belongs in the input set, while the resolved
// VALUE never does — bare env keys never contain '=', so the two entry shapes
// cannot collide.
func jobInputHashFor(job model.PlanJob) string {
	envKeys := make([]string, 0, len(job.Env)+len(job.SecretRefs))
	for k := range job.Env {
		envKeys = append(envKeys, k)
	}
	for _, sr := range job.SecretRefs {
		envKeys = append(envKeys, sr.AsEnv+"="+sr.Ref)
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
// full client rewrite — it is the default backend (the legacy OP2 relational
// path is decommissioned; ORUN_COORDINATION=legacy is a temporary escape hatch).
// The lease epoch that :claim returns is the conditional-append key for
// :heartbeat/:complete, so it is threaded across calls — kept per job for the
// single run this backend drives.
type CoordBackend struct {
	coord    *CoordClient
	inner    *RemoteStateBackend
	runnerID string

	mu     sync.Mutex
	leases map[string]int    // jobID -> leaseEpoch
	hashes map[string]string // jobID -> jobInputHash (set when a hermetic job is claimed)
	// retryJobs marks jobs whose claim should carry retry=true (the --retry
	// re-open of a failed job when resuming one execution across CI reruns).
	retryJobs map[string]bool

	viewMu sync.Mutex
	views  map[string]*runView // wire run id -> what this process knows of the run
}

// runView is what this process knows of a run between reads: the folded
// state, the plan's dependency graph, and the timestamps the presenter shows,
// advanced by the events each long-poll or delta read returns. A waiting job
// used to re-read the whole event log and re-download the plan object on
// every wake, discarding the very events its long-poll had just returned;
// with the view a wake costs the long-poll alone and a read made just after
// it costs nothing (specs/orun-sync-efficiency P1).
type runView struct {
	plan       CoordinationPlan
	havePlan   bool
	fold       RunFoldState
	started    bool // fold holds the run's state (its events have been folded)
	times      map[string]*jobTimes
	runStarted string
	runFinish  string
	user       string
	syncedAt   time.Time
	// noStateRead remembers a server with no state read for this run (an
	// older server, or a run its shard does not back): the first sync reads
	// the log instead, and is not asked again.
	noStateRead bool
}

type jobTimes struct{ started, finished string }

// viewFreshFor is how long after a read or a long-poll the view answers a
// state or frontier read without asking the server again: long enough to
// cover the reads a wake makes in a row, short enough that a caller polling
// on a timer still sees the server within its interval.
var viewFreshFor = 2 * time.Second // a variable so tests can age a view

var _ Backend = (*CoordBackend)(nil)

// MarkRetry flags a job so its next claim carries retry=true — the explicit
// re-open of a terminally-failed job when a CI rerun resumes one execution.
func (b *CoordBackend) MarkRetry(jobID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.retryJobs[jobID] = true
}

// NewCoordBackend wires the native coordination client over an inner backend
// (used for InitRun, logs, and read-model loads).
func NewCoordBackend(coord *CoordClient, inner *RemoteStateBackend, runnerID string) *CoordBackend {
	return &CoordBackend{coord: coord, inner: inner, runnerID: runnerID, leases: map[string]int{}, hashes: map[string]string{}, retryJobs: map[string]bool{}, views: map[string]*runView{}}
}

// InitRun creates/joins the run via the inner backend. When the server runs the
// DO coordination backend, that create seeds the per-run shard, so the native
// verbs below operate on a DO-backed run.
func (b *CoordBackend) InitRun(ctx context.Context, plan *model.Plan, opts InitRunOptions) (*RunHandle, error) {
	h, err := b.inner.InitRun(ctx, plan, opts)
	if err != nil {
		return nil, err
	}
	// The plan this process holds seeds the run's view, so its reads never
	// need the plan object.
	if plan != nil {
		cp := CoordinationPlan{Jobs: make(map[string]PlanNode, len(plan.Jobs))}
		for _, j := range plan.Jobs {
			cp.Jobs[j.ID] = PlanNode{Deps: append([]string(nil), j.DependsOn...)}
		}
		b.viewMu.Lock()
		v := b.viewLocked(wireRunID(h.RunID))
		if !v.havePlan {
			v.plan, v.havePlan = cp, true
		}
		b.viewMu.Unlock()
	}
	return h, nil
}

// viewLocked returns the run's view, creating an empty one. viewMu held.
func (b *CoordBackend) viewLocked(wire string) *runView {
	v := b.views[wire]
	if v == nil {
		v = &runView{plan: CoordinationPlan{Jobs: map[string]PlanNode{}}, times: map[string]*jobTimes{}}
		b.views[wire] = v
	}
	return v
}

// absorbLocked folds events onto the view (those past its last seq) and
// records the timestamps the presenter shows. viewMu held.
func (v *runView) absorbLocked(events []CoordinationEvent) {
	if v.started {
		v.fold = FoldFrom(&v.fold, events, v.plan)
	} else {
		v.fold = Fold(events, v.plan)
		v.started = true
	}
	for _, e := range events {
		switch e.Kind {
		case EventRunCreated:
			v.runStarted, v.user = e.At, e.Actor.ID
		case EventRunCompleted, EventRunFailed, EventRunCanceled:
			v.runFinish = e.At
		case EventJobClaimed:
			if t := v.at(e.JobID); t.started == "" {
				t.started = e.At
			}
		case EventJobSucceeded, EventJobFailed, EventJobMemoized:
			v.at(e.JobID).finished = e.At
		}
	}
	v.syncedAt = time.Now()
}

func (v *runView) at(jobID string) *jobTimes {
	t := v.times[jobID]
	if t == nil {
		t = &jobTimes{}
		v.times[jobID] = t
	}
	return t
}

// planDigestOf is the plan digest the run's created event names, "" when
// the events do not include it.
func planDigestOf(events []CoordinationEvent) string {
	for _, e := range events {
		if e.Kind == EventRunCreated && e.PlanDigest != "" {
			return e.PlanDigest
		}
	}
	return ""
}

// ensurePlanLocked fetches the plan object the run was created from when
// this process did not seed the view from its own plan: once per run, not
// once per read. viewMu held.
func (b *CoordBackend) ensurePlanLocked(ctx context.Context, v *runView, planDigest string) bool {
	if v.havePlan {
		return true
	}
	if b.inner == nil || planDigest == "" {
		return false
	}
	blob, err := b.inner.GetObject(ctx, planDigest)
	if err != nil || blob == nil {
		return false
	}
	var bp remotestate.BackendPlan
	if json.Unmarshal(blob, &bp) != nil {
		return false
	}
	cp := CoordinationPlan{Jobs: make(map[string]PlanNode, len(bp.Jobs))}
	for _, j := range bp.Jobs {
		cp.Jobs[j.JobID] = PlanNode{Deps: j.Deps}
	}
	v.plan, v.havePlan = cp, true
	return true
}

// adoptLocked makes the server's fold the view's state: the first read of a
// run is the one state read instead of its whole log. The timings beside the
// fold are the presenter's timestamps; a fold the server built before it
// recorded them leaves them unset. viewMu held.
func (v *runView) adoptLocked(st *RunStateRead) {
	v.fold = st.Fold
	if v.fold.Jobs == nil {
		v.fold.Jobs = map[string]*JobFoldState{}
	}
	v.started = true
	if st.Timings != nil {
		v.runStarted, v.runFinish = st.Timings.StartedAt, st.Timings.FinishedAt
		for id, jt := range st.Timings.Jobs {
			t := v.at(id)
			t.started, t.finished = jt.StartedAt, jt.FinishedAt
		}
	}
	v.syncedAt = time.Now()
}

// primeLocked gives a view with nothing yet the run's state: the server's
// fold in one read when it has one for the run (and the plan object once,
// when this process did not seed the plan), else the log it was given. It
// reports whether the view holds the run's state afterwards. viewMu held.
func (b *CoordBackend) primeLocked(ctx context.Context, wire string, v *runView, log func() ([]CoordinationEvent, error)) bool {
	if !v.noStateRead {
		st, err := b.coord.ReadState(ctx, wire)
		switch {
		case err == nil:
			if !b.ensurePlanLocked(ctx, v, st.Fold.PlanDigest) {
				return false
			}
			v.adoptLocked(st)
			return true
		case errors.Is(err, ErrStateReadUnsupported):
			v.noStateRead = true
		}
	}
	if log == nil {
		return false
	}
	events, err := log()
	if err != nil || len(events) == 0 || !b.ensurePlanLocked(ctx, v, planDigestOf(events)) {
		return false
	}
	v.absorbLocked(events)
	return true
}

// syncView brings the run's view up to the server: the server's fold in one
// read when the view has nothing yet (the whole log on a server without the
// state read), else only the events past its last seq, and nothing at all
// within viewFreshFor of the last sync. It reports whether the view holds
// the run's state afterwards.
func (b *CoordBackend) syncView(ctx context.Context, wire string) (*runView, bool) {
	b.viewMu.Lock()
	defer b.viewMu.Unlock()
	v := b.viewLocked(wire)
	if v.started && time.Since(v.syncedAt) < viewFreshFor {
		return v, true
	}
	if !v.started {
		return v, b.primeLocked(ctx, wire, v, func() ([]CoordinationEvent, error) { return b.coord.ReadLog(ctx, wire, 0, 0) })
	}
	events, err := b.coord.ReadLog(ctx, wire, v.fold.LastSeq, 0)
	if err != nil {
		return v, true
	}
	v.absorbLocked(events)
	return v, true
}

// ClaimJob posts a §3 :claim and maps the native outcome onto the ClaimResult
// fields the runner branches on, stashing the lease epoch for later verbs.
func (b *CoordBackend) ClaimJob(ctx context.Context, runID string, job model.PlanJob, runnerID string) (*ClaimResult, error) {
	if runnerID == "" {
		runnerID = b.runnerID
	}
	b.mu.Lock()
	retry := b.retryJobs[job.ID]
	b.mu.Unlock()
	req := ClaimRequest{RunnerID: runnerID, Retry: retry}
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
		// exactly like a job finished elsewhere and does not execute it. Cached +
		// ResultDigest let the caller surface the hit (it is otherwise silent).
		res.Cached = true
		res.ResultDigest = out.ResultDigest
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

// RunnableJobs is the run's frontier: from the view when this process holds
// the run's state (synced first unless it just was), else the server's
// projection.
func (b *CoordBackend) RunnableJobs(ctx context.Context, runID string) ([]string, error) {
	wire := wireRunID(runID)
	if v, ok := b.syncView(ctx, wire); ok {
		b.viewMu.Lock()
		defer b.viewMu.Unlock()
		return append([]string(nil), v.fold.Frontier...), nil
	}
	return b.coord.Frontier(ctx, wire)
}

// WaitForRunEvents long-polls the run's event stream for any event past
// sinceSeq, returning the new head seq (or sinceSeq unchanged when the wait
// lapses with no new event), and folds what arrives onto the view, so the
// state and frontier reads that follow a wake cost nothing. It powers the
// event-driven waits and `status --watch`. A sinceSeq of 0 with no wait is
// the head read; when the view holds the run already it starts from the
// view's last seq instead of re-reading the whole log.
func (b *CoordBackend) WaitForRunEvents(ctx context.Context, runID string, sinceSeq, waitSeconds int) (int, error) {
	wire := wireRunID(runID)
	b.viewMu.Lock()
	v := b.viewLocked(wire)
	if sinceSeq == 0 && v.started {
		sinceSeq = v.fold.LastSeq
	}
	b.viewMu.Unlock()

	events, err := b.coord.ReadLog(ctx, wire, sinceSeq, waitSeconds)
	if err != nil {
		return sinceSeq, err
	}
	head := sinceSeq
	for _, e := range events {
		if e.Seq > head {
			head = e.Seq
		}
	}

	b.viewMu.Lock()
	defer b.viewMu.Unlock()
	switch {
	case v.started && sinceSeq <= v.fold.LastSeq:
		// Contiguous with, or overlapping, what the view holds: it advances
		// with the long-poll (FoldFrom ignores the events it already folded).
		v.absorbLocked(events)
	case !v.started && sinceSeq == 0 && len(events) > 0:
		_ = b.primeLocked(ctx, wire, v, func() ([]CoordinationEvent, error) { return events, nil })
	}
	// A page the view has not seen the start of leaves it alone; the next
	// state read continues from the view's own seq.
	return head, nil
}

// WaitForRunProgress is WaitForRunEvents for a waiter that cares about
// progress alone: it asks the server to hold through lease renewals
// (`wake=progress`), folds everything the page carries onto the view, and
// reports the stream head with whether any event past sinceSeq was more than
// a renewal. A runner waiting on its upstreams is woken when one of them
// changes, not every 20 s per running job (specs/orun-sync-efficiency F3).
func (b *CoordBackend) WaitForRunProgress(ctx context.Context, runID string, sinceSeq, waitSeconds int) (head int, progressed bool, err error) {
	wire := wireRunID(runID)
	b.viewMu.Lock()
	v := b.viewLocked(wire)
	if sinceSeq == 0 && v.started {
		sinceSeq = v.fold.LastSeq
	}
	b.viewMu.Unlock()

	events, _, err := b.coord.ReadLogWait(ctx, wire, sinceSeq, waitSeconds, true)
	if err != nil {
		return sinceSeq, false, err
	}
	head = sinceSeq
	for _, e := range events {
		if e.Seq > head {
			head = e.Seq
		}
		if e.Seq > sinceSeq && e.Kind != EventLeaseRenewed {
			progressed = true
		}
	}

	b.viewMu.Lock()
	defer b.viewMu.Unlock()
	switch {
	case v.started && sinceSeq <= v.fold.LastSeq:
		v.absorbLocked(events)
	case !v.started && sinceSeq == 0 && len(events) > 0:
		_ = b.primeLocked(ctx, wire, v, func() ([]CoordinationEvent, error) { return events, nil })
	}
	return head, progressed, nil
}

// ── Delegated to the inner backend (native surface does not yet own these) ──

func (b *CoordBackend) AppendStepLog(ctx context.Context, runID, jobID, content string, step *LogStep) error {
	return b.inner.AppendStepLog(ctx, runID, jobID, content, step)
}

// LoadRunState is the run's state from the view, synced to the server first
// unless it just was: the whole log on the first read, only the events past
// the view's last seq after that, and the plan object once per run (never
// when this process seeded the view from its own plan). A run with no
// native events (created before the coordination plane) falls back to the
// inner backend's read model.
func (b *CoordBackend) LoadRunState(ctx context.Context, runID string) (*execmodel.ExecState, *execmodel.ExecMetadata, error) {
	v, ok := b.syncView(ctx, wireRunID(runID))
	if !ok {
		return b.inner.LoadRunState(ctx, runID)
	}
	b.viewMu.Lock()
	defer b.viewMu.Unlock()
	st, meta := v.toExec(runID)
	return st, meta, nil
}

// foldToExec maps an authoritative fold and its event stream into the
// presenter's ExecState/ExecMetadata.
func foldToExec(runID string, fold RunFoldState, events []CoordinationEvent) (*execmodel.ExecState, *execmodel.ExecMetadata) {
	v := &runView{fold: fold, started: true, times: map[string]*jobTimes{}}
	for _, e := range events {
		switch e.Kind {
		case EventRunCreated:
			v.runStarted, v.user = e.At, e.Actor.ID
		case EventRunCompleted, EventRunFailed, EventRunCanceled:
			v.runFinish = e.At
		case EventJobClaimed:
			if t := v.at(e.JobID); t.started == "" {
				t.started = e.At
			}
		case EventJobSucceeded, EventJobFailed, EventJobMemoized:
			v.at(e.JobID).finished = e.At
		}
	}
	return v.toExec(runID)
}

// toExec maps the view into the presenter's ExecState/ExecMetadata.
func (v *runView) toExec(runID string) (*execmodel.ExecState, *execmodel.ExecMetadata) {
	fold := v.fold
	st := &execmodel.ExecState{ExecID: runID, PlanChecksum: fold.PlanDigest, Jobs: make(map[string]*execmodel.JobState, len(fold.Jobs))}
	done, failed := 0, 0
	for id, j := range fold.Jobs {
		phase := j.Phase
		if phase == "memoized" {
			phase = "succeeded" // a memoized job reads as completed
		}
		js := &execmodel.JobState{Status: remotestate.BackendJobStatusToLocal(phase), Steps: map[string]string{}, LastError: j.ErrorText}
		if t := v.times[id]; t != nil {
			js.StartedAt, js.FinishedAt = t.started, t.finished
		}
		js.HeartbeatAt = j.LeaseExpiresAt
		st.Jobs[id] = js
		switch j.Phase {
		case "succeeded", "memoized":
			done++
		case "failed", "timed_out", "canceled":
			failed++
		}
	}

	meta := &execmodel.ExecMetadata{
		ExecID:     runID,
		PlanID:     fold.PlanDigest,
		StartedAt:  v.runStarted,
		FinishedAt: v.runFinish,
		Status:     localRunStatus(fold.Phase),
		User:       v.user,
		JobTotal:   len(fold.Jobs),
		JobDone:    done,
		JobFailed:  failed,
	}
	return st, meta
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

// LeaseEpoch returns the lease epoch this backend holds for a claimed job —
// the conditional key the lease-bound secret resolve carries (contract §4 v3),
// same discipline as :heartbeat/:complete. Zero when the job was not claimed
// through this backend.
func (b *CoordBackend) LeaseEpoch(jobID string) int { return b.lease(jobID) }

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
