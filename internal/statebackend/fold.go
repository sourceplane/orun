package statebackend

import "sort"

// Coordination v2 fold — the Go port of the platform's pure reduce()
// (orun-cloud `@saas/contracts/coordination`, contract coordination-api.md §8.2).
// Authoritative job/lease state is a deterministic left-fold by seq over a run's
// append-only event stream; the two implementations are pinned by the shared
// golden vectors (foldGoldenVectors in fold_test.go mirror the TS
// coordination-vectors.ts). Nullable fields use the zero value ("" / 0) as the
// "none" sentinel — epochs and attempts start at 1, so 0 unambiguously means
// none, matching the TS `null`.

// Coordination event kinds (the state.* taxonomy; coordination-api.md §8.1).
const (
	EventRunCreated   = "state.run.created"
	EventJobReady     = "state.job.ready"
	EventJobClaimed   = "state.job.claimed"
	EventLeaseRenewed = "state.job.lease_renewed"
	EventLeaseExpired = "state.job.lease_expired"
	EventJobSucceeded = "state.job.succeeded"
	EventJobMemoized  = "state.job.memoized"
	EventJobFailed    = "state.job.failed"
	EventLogChunk     = "state.job.log_chunk"
	EventRunCompleted = "state.run.completed"
	EventRunFailed    = "state.run.failed"
	EventRunCanceled  = "state.run.canceled"
)

// CoordinationActor attributes each event (signed provenance on the wire).
type CoordinationActor struct {
	ID   string
	Type string // user | service_principal | workflow | system
}

// CoordinationEvent is the per-run event envelope (coordination-api.md §8.1)
// with the payload flattened: the fold reads only the fields relevant to Kind.
type CoordinationEvent struct {
	Seq            int
	Kind           string
	RunID          string
	JobID          string // "" for run-level events
	Actor          CoordinationActor
	At             string
	IdempotencyKey string
	V              int

	// payload (only the fields meaningful for Kind are read)
	PlanDigest     string
	SourceHash     string
	RunnerID       string
	LeaseEpoch     int
	LeaseExpiresAt string
	Attempt        int
	ResultDigest   string
	Reason         string
	ErrorText      string
}

// CoordinationPlan is the plan-derived job DAG the fold needs (jobs are not
// carried in events).
type CoordinationPlan struct {
	Jobs map[string]PlanNode
}

// PlanNode is one job's dependency list.
type PlanNode struct {
	Deps []string
}

// JobFoldState is the folded state of a single job.
type JobFoldState struct {
	JobID          string
	Phase          string // queued|claimed|succeeded|memoized|failed|timed_out|canceled
	Holder         string // "" = none
	LeaseEpoch     int    // 0 = none
	LeaseExpiresAt string // "" = none
	Attempt        int
	ResultDigest   string // "" = none
	ErrorText      string // "" = none
}

// RunFoldState is the authoritative state derived from a run's event stream.
type RunFoldState struct {
	RunID      string
	PlanDigest string // "" = none
	SourceHash string // "" = none
	Phase      string // pending|running|succeeded|failed|canceled
	Jobs       map[string]*JobFoldState
	Frontier   []string // queued jobs whose deps are all succeeded|memoized, sorted
	LastSeq    int
}

func jobTerminal(phase string) bool {
	switch phase {
	case "succeeded", "memoized", "failed", "timed_out", "canceled":
		return true
	}
	return false
}

func jobSuccess(phase string) bool {
	return phase == "succeeded" || phase == "memoized"
}

func freshJob(jobID string) *JobFoldState {
	return &JobFoldState{JobID: jobID, Phase: "queued", Attempt: 1}
}

// Fold reduces a run's event stream into authoritative state (coordination-api.md
// §8.2). Pure: identical (events, plan) always yield an identical result. Events
// are sorted by Seq defensively; unknown kinds are ignored (additive
// forward-compat). Run phase and the frontier are derived from job phases;
// RunCompleted/RunFailed are projection signals (no-ops here); RunCanceled is
// honored.
func Fold(events []CoordinationEvent, plan CoordinationPlan) RunFoldState {
	jobs := make(map[string]*JobFoldState, len(plan.Jobs))
	for jobID := range plan.Jobs {
		jobs[jobID] = freshJob(jobID)
	}

	var planDigest, sourceHash, runID string
	lastSeq := 0
	canceled := false

	ordered := make([]CoordinationEvent, len(events))
	copy(ordered, events)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Seq < ordered[j].Seq })

	for _, e := range ordered {
		if e.Seq > lastSeq {
			lastSeq = e.Seq
		}
		if runID == "" {
			runID = e.RunID
		}

		switch e.Kind {
		case EventRunCreated:
			planDigest = e.PlanDigest
			sourceHash = e.SourceHash
			continue
		case EventRunCanceled:
			canceled = true
			for _, job := range jobs {
				if !jobTerminal(job.Phase) {
					job.Phase = "canceled"
					job.Holder = ""
					job.LeaseEpoch = 0
					job.LeaseExpiresAt = ""
				}
			}
			continue
		}

		if e.JobID == "" {
			continue
		}
		job := jobs[e.JobID]
		if job == nil {
			continue // event for a job not in the plan — ignore
		}
		if jobTerminal(job.Phase) && e.Kind != EventJobReady {
			continue // terminal states are sticky
		}

		switch e.Kind {
		case EventJobReady:
			if !jobTerminal(job.Phase) {
				job.Phase = "queued"
				job.Holder = ""
				job.LeaseEpoch = 0
				job.LeaseExpiresAt = ""
				job.Attempt = e.Attempt
			}
		case EventJobClaimed:
			job.Phase = "claimed"
			job.Holder = e.RunnerID
			job.LeaseEpoch = e.LeaseEpoch
			job.LeaseExpiresAt = e.LeaseExpiresAt
			job.Attempt = e.Attempt
		case EventLeaseRenewed:
			if job.Holder == e.RunnerID && job.LeaseEpoch == e.LeaseEpoch {
				job.LeaseExpiresAt = e.LeaseExpiresAt
			}
		case EventLeaseExpired:
			if job.Phase == "claimed" && job.LeaseEpoch == e.LeaseEpoch {
				job.Phase = "queued"
				job.Holder = ""
				job.LeaseEpoch = 0
				job.LeaseExpiresAt = ""
				job.Attempt++
			}
		case EventJobSucceeded:
			job.Phase = "succeeded"
			job.ResultDigest = e.ResultDigest
			job.Holder = ""
			job.LeaseEpoch = 0
			job.LeaseExpiresAt = ""
		case EventJobMemoized:
			job.Phase = "memoized"
			job.ResultDigest = e.ResultDigest
			job.Holder = ""
			job.LeaseEpoch = 0
			job.LeaseExpiresAt = ""
		case EventJobFailed:
			if e.Reason == "timed_out" {
				job.Phase = "timed_out"
			} else {
				job.Phase = "failed"
			}
			job.ErrorText = e.ErrorText
			job.Holder = ""
			job.LeaseEpoch = 0
			job.LeaseExpiresAt = ""
		default:
			// LogChunk and unknown kinds do not change fold state.
		}
	}

	return RunFoldState{
		RunID:      runID,
		PlanDigest: planDigest,
		SourceHash: sourceHash,
		Phase:      deriveRunPhase(jobs, canceled, planDigest != ""),
		Jobs:       jobs,
		Frontier:   computeFrontier(jobs, plan),
		LastSeq:    lastSeq,
	}
}

func deriveRunPhase(jobs map[string]*JobFoldState, canceled, created bool) string {
	if canceled {
		return "canceled"
	}
	if len(jobs) == 0 {
		return "pending"
	}
	anyFailed, allSuccess, active := false, true, false
	for _, j := range jobs {
		if j.Phase == "failed" || j.Phase == "timed_out" {
			anyFailed = true
		}
		if !jobSuccess(j.Phase) {
			allSuccess = false
		}
		if j.Phase != "queued" || j.Attempt > 1 {
			active = true
		}
	}
	if anyFailed {
		return "failed"
	}
	if allSuccess {
		return "succeeded"
	}
	if created || active {
		return "running"
	}
	return "pending"
}

func computeFrontier(jobs map[string]*JobFoldState, plan CoordinationPlan) []string {
	frontier := []string{}
	for jobID, job := range jobs {
		if job.Phase != "queued" {
			continue
		}
		ready := true
		for _, d := range plan.Jobs[jobID].Deps {
			dep := jobs[d]
			if dep == nil || !jobSuccess(dep.Phase) {
				ready = false
				break
			}
		}
		if ready {
			frontier = append(frontier, jobID)
		}
	}
	sort.Strings(frontier)
	return frontier
}
