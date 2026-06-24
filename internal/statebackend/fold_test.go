package statebackend

import (
	"reflect"
	"testing"
)

// These golden vectors mirror the platform's coordination-vectors.ts verbatim
// (the cross-language contract, coordination-api.md §8.2). Fold() must reproduce
// every expected state for the same (events, plan).

const (
	vecAT = "2026-06-19T00:00:00Z"
	vecLE = "2026-06-19T00:01:00Z"
)

var vecActor = CoordinationActor{ID: "u1", Type: "user"}

func evCreated(seq int) CoordinationEvent {
	return CoordinationEvent{
		Seq: seq, Kind: EventRunCreated, RunID: "r", Actor: vecActor, At: vecAT,
		IdempotencyKey: "r", V: 1, PlanDigest: "sha256:plan", SourceHash: "sha256:src",
	}
}
func evClaimed(seq int, jobID string) CoordinationEvent {
	return CoordinationEvent{
		Seq: seq, Kind: EventJobClaimed, RunID: "r", JobID: jobID, Actor: vecActor, At: vecAT,
		IdempotencyKey: jobID + ":claimed:1", V: 1,
		RunnerID: "runner-1", LeaseEpoch: 1, LeaseExpiresAt: vecLE, Attempt: 1,
	}
}
func evSucceeded(seq int, jobID, result string) CoordinationEvent {
	return CoordinationEvent{
		Seq: seq, Kind: EventJobSucceeded, RunID: "r", JobID: jobID, Actor: vecActor, At: vecAT,
		IdempotencyKey: jobID + ":succeeded:1", V: 1,
		RunnerID: "runner-1", LeaseEpoch: 1, ResultDigest: result,
	}
}
func evMemoized(seq int, jobID, result string) CoordinationEvent {
	return CoordinationEvent{
		Seq: seq, Kind: EventJobMemoized, RunID: "r", JobID: jobID, Actor: vecActor, At: vecAT,
		IdempotencyKey: jobID + ":memoized:0", V: 1, ResultDigest: result,
	}
}
func evFailed(seq int, jobID, reason, errText string) CoordinationEvent {
	return CoordinationEvent{
		Seq: seq, Kind: EventJobFailed, RunID: "r", JobID: jobID, Actor: vecActor, At: vecAT,
		IdempotencyKey: jobID + ":failed:1", V: 1,
		RunnerID: "runner-1", LeaseEpoch: 1, Reason: reason, ErrorText: errText,
	}
}
func evLeaseExpired(seq int, jobID string) CoordinationEvent {
	return CoordinationEvent{
		Seq: seq, Kind: EventLeaseExpired, RunID: "r", JobID: jobID,
		Actor: CoordinationActor{ID: "system:state-sweep", Type: "system"}, At: vecAT,
		IdempotencyKey: jobID + ":lease_expired:1", V: 1, RunnerID: "runner-1", LeaseEpoch: 1,
	}
}
func evCanceled(seq int) CoordinationEvent {
	return CoordinationEvent{
		Seq: seq, Kind: EventRunCanceled, RunID: "r", Actor: vecActor, At: vecAT,
		IdempotencyKey: "r:canceled:0", V: 1, Reason: "user",
	}
}

// Run-terminal *signals* the server now emits (coordination-api.md §3). The fold
// derives run phase from job phases, so these must be tolerated as no-ops.
func evRunCompleted(seq int) CoordinationEvent {
	return CoordinationEvent{Seq: seq, Kind: EventRunCompleted, RunID: "r", Actor: vecActor, At: vecAT, V: 1}
}

func evRunFailed(seq int, reason string) CoordinationEvent {
	return CoordinationEvent{Seq: seq, Kind: EventRunFailed, RunID: "r", Actor: vecActor, At: vecAT, V: 1, Reason: reason}
}

func job(jobID string, mut func(*JobFoldState)) *JobFoldState {
	j := &JobFoldState{JobID: jobID, Phase: "queued", Attempt: 1}
	if mut != nil {
		mut(j)
	}
	return j
}

func linearPlan() CoordinationPlan {
	return CoordinationPlan{Jobs: map[string]PlanNode{"a": {}, "b": {Deps: []string{"a"}}}}
}
func diamondPlan() CoordinationPlan {
	return CoordinationPlan{Jobs: map[string]PlanNode{
		"a": {}, "b": {Deps: []string{"a"}}, "c": {Deps: []string{"a"}}, "d": {Deps: []string{"b", "c"}},
	}}
}

type foldVector struct {
	name     string
	plan     CoordinationPlan
	events   []CoordinationEvent
	expected RunFoldState
}

func foldGoldenVectors() []foldVector {
	return []foldVector{
		{
			name:   "empty stream — initial frontier is the deps-free jobs",
			plan:   linearPlan(),
			events: []CoordinationEvent{},
			expected: RunFoldState{
				Phase:    "pending",
				Jobs:     map[string]*JobFoldState{"a": job("a", nil), "b": job("b", nil)},
				Frontier: []string{"a"},
			},
		},
		{
			name:   "created + claim a — a held, b blocked, run running",
			plan:   linearPlan(),
			events: []CoordinationEvent{evCreated(1), evClaimed(2, "a")},
			expected: RunFoldState{
				RunID: "r", PlanDigest: "sha256:plan", SourceHash: "sha256:src", Phase: "running",
				Jobs: map[string]*JobFoldState{
					"a": job("a", func(j *JobFoldState) {
						j.Phase = "claimed"
						j.Holder = "runner-1"
						j.LeaseEpoch = 1
						j.LeaseExpiresAt = vecLE
					}),
					"b": job("b", nil),
				},
				Frontier: []string{}, LastSeq: 2,
			},
		},
		{
			name:   "a succeeds — unblocks b into the frontier",
			plan:   linearPlan(),
			events: []CoordinationEvent{evCreated(1), evClaimed(2, "a"), evSucceeded(3, "a", "sha256:res-a")},
			expected: RunFoldState{
				RunID: "r", PlanDigest: "sha256:plan", SourceHash: "sha256:src", Phase: "running",
				Jobs: map[string]*JobFoldState{
					"a": job("a", func(j *JobFoldState) { j.Phase = "succeeded"; j.ResultDigest = "sha256:res-a" }),
					"b": job("b", nil),
				},
				Frontier: []string{"b"}, LastSeq: 3,
			},
		},
		{
			name:   "out-of-order seq is sorted defensively",
			plan:   linearPlan(),
			events: []CoordinationEvent{evClaimed(2, "a"), evCreated(1)},
			expected: RunFoldState{
				RunID: "r", PlanDigest: "sha256:plan", SourceHash: "sha256:src", Phase: "running",
				Jobs: map[string]*JobFoldState{
					"a": job("a", func(j *JobFoldState) {
						j.Phase = "claimed"
						j.Holder = "runner-1"
						j.LeaseEpoch = 1
						j.LeaseExpiresAt = vecLE
					}),
					"b": job("b", nil),
				},
				Frontier: []string{}, LastSeq: 2,
			},
		},
		{
			name: "diamond runs to completion — run succeeded",
			plan: diamondPlan(),
			events: []CoordinationEvent{
				evCreated(1), evClaimed(2, "a"), evSucceeded(3, "a", "sha256:res-a"),
				evClaimed(4, "b"), evClaimed(5, "c"),
				evSucceeded(6, "b", "sha256:res-b"), evSucceeded(7, "c", "sha256:res-c"),
				evClaimed(8, "d"), evSucceeded(9, "d", "sha256:res-d"),
			},
			expected: RunFoldState{
				RunID: "r", PlanDigest: "sha256:plan", SourceHash: "sha256:src", Phase: "succeeded",
				Jobs: map[string]*JobFoldState{
					"a": job("a", func(j *JobFoldState) { j.Phase = "succeeded"; j.ResultDigest = "sha256:res-a" }),
					"b": job("b", func(j *JobFoldState) { j.Phase = "succeeded"; j.ResultDigest = "sha256:res-b" }),
					"c": job("c", func(j *JobFoldState) { j.Phase = "succeeded"; j.ResultDigest = "sha256:res-c" }),
					"d": job("d", func(j *JobFoldState) { j.Phase = "succeeded"; j.ResultDigest = "sha256:res-d" }),
				},
				Frontier: []string{}, LastSeq: 9,
			},
		},
		{
			name: "explicit run.completed signal is a no-op (phase still derived from jobs)",
			plan: linearPlan(),
			events: []CoordinationEvent{
				evCreated(1), evClaimed(2, "a"), evSucceeded(3, "a", "sha256:res-a"),
				evClaimed(4, "b"), evSucceeded(5, "b", "sha256:res-b"),
				evRunCompleted(6), // the server's run-terminal signal — fold must tolerate it
			},
			expected: RunFoldState{
				RunID: "r", PlanDigest: "sha256:plan", SourceHash: "sha256:src", Phase: "succeeded",
				Jobs: map[string]*JobFoldState{
					"a": job("a", func(j *JobFoldState) { j.Phase = "succeeded"; j.ResultDigest = "sha256:res-a" }),
					"b": job("b", func(j *JobFoldState) { j.Phase = "succeeded"; j.ResultDigest = "sha256:res-b" }),
				},
				Frontier: []string{}, LastSeq: 6,
			},
		},
		{
			name: "explicit run.failed signal is a no-op (phase still derived from jobs)",
			plan: linearPlan(),
			events: []CoordinationEvent{
				evCreated(1), evClaimed(2, "a"), evFailed(3, "a", "step_failed", "boom"),
				evRunFailed(4, "job_failed"),
			},
			expected: RunFoldState{
				RunID: "r", PlanDigest: "sha256:plan", SourceHash: "sha256:src", Phase: "failed",
				Jobs: map[string]*JobFoldState{
					"a": job("a", func(j *JobFoldState) { j.Phase = "failed"; j.ErrorText = "boom" }),
					"b": job("b", nil),
				},
				Frontier: []string{}, LastSeq: 4,
			},
		},
		{
			name:   "failed dependency blocks downstream and fails the run",
			plan:   linearPlan(),
			events: []CoordinationEvent{evCreated(1), evClaimed(2, "a"), evFailed(3, "a", "step_failed", "boom")},
			expected: RunFoldState{
				RunID: "r", PlanDigest: "sha256:plan", SourceHash: "sha256:src", Phase: "failed",
				Jobs: map[string]*JobFoldState{
					"a": job("a", func(j *JobFoldState) { j.Phase = "failed"; j.ErrorText = "boom" }),
					"b": job("b", nil),
				},
				Frontier: []string{}, LastSeq: 3,
			},
		},
		{
			name:   "lease expiry re-queues the job (attempt+1) into the frontier",
			plan:   CoordinationPlan{Jobs: map[string]PlanNode{"a": {}}},
			events: []CoordinationEvent{evCreated(1), evClaimed(2, "a"), evLeaseExpired(3, "a")},
			expected: RunFoldState{
				RunID: "r", PlanDigest: "sha256:plan", SourceHash: "sha256:src", Phase: "running",
				Jobs:     map[string]*JobFoldState{"a": job("a", func(j *JobFoldState) { j.Attempt = 2 })},
				Frontier: []string{"a"}, LastSeq: 3,
			},
		},
		{
			name:   "memoized job counts as success and unblocks downstream",
			plan:   linearPlan(),
			events: []CoordinationEvent{evCreated(1), evMemoized(2, "a", "sha256:cached-a")},
			expected: RunFoldState{
				RunID: "r", PlanDigest: "sha256:plan", SourceHash: "sha256:src", Phase: "running",
				Jobs: map[string]*JobFoldState{
					"a": job("a", func(j *JobFoldState) { j.Phase = "memoized"; j.ResultDigest = "sha256:cached-a" }),
					"b": job("b", nil),
				},
				Frontier: []string{"b"}, LastSeq: 2,
			},
		},
		{
			name:   "cancel marks non-terminal jobs canceled and the run canceled",
			plan:   linearPlan(),
			events: []CoordinationEvent{evCreated(1), evClaimed(2, "a"), evCanceled(3)},
			expected: RunFoldState{
				RunID: "r", PlanDigest: "sha256:plan", SourceHash: "sha256:src", Phase: "canceled",
				Jobs: map[string]*JobFoldState{
					"a": job("a", func(j *JobFoldState) { j.Phase = "canceled" }),
					"b": job("b", func(j *JobFoldState) { j.Phase = "canceled" }),
				},
				Frontier: []string{}, LastSeq: 3,
			},
		},
	}
}

func TestFoldGoldenVectors(t *testing.T) {
	for _, v := range foldGoldenVectors() {
		t.Run(v.name, func(t *testing.T) {
			got := Fold(v.events, v.plan)
			if !reflect.DeepEqual(got, v.expected) {
				t.Fatalf("fold mismatch\n got: %+v\nwant: %+v", got, v.expected)
			}
		})
	}
}

func TestFoldDeterministic(t *testing.T) {
	v := foldGoldenVectors()[4] // diamond
	if !reflect.DeepEqual(Fold(v.events, v.plan), Fold(v.events, v.plan)) {
		t.Fatal("fold is not deterministic")
	}
}

func TestFoldTerminalSticky(t *testing.T) {
	plan := CoordinationPlan{Jobs: map[string]PlanNode{"a": {}}}
	events := []CoordinationEvent{
		evCreated(1), evClaimed(2, "a"),
		evFailed(3, "a", "step_failed", "boom"),
		evSucceeded(4, "a", "sha256:late"), // must NOT revive the failure
	}
	s := Fold(events, plan)
	if s.Jobs["a"].Phase != "failed" || s.Jobs["a"].ResultDigest != "" || s.Phase != "failed" {
		t.Fatalf("terminal state not sticky: %+v", s.Jobs["a"])
	}
}

func TestFoldIdempotentDuplicate(t *testing.T) {
	plan := CoordinationPlan{Jobs: map[string]PlanNode{"a": {}}}
	once := Fold([]CoordinationEvent{evCreated(1), evClaimed(2, "a")}, plan)
	twice := Fold([]CoordinationEvent{evCreated(1), evClaimed(2, "a"), evClaimed(2, "a")}, plan)
	if !reflect.DeepEqual(once, twice) {
		t.Fatalf("duplicate append not idempotent:\n once: %+v\ntwice: %+v", once, twice)
	}
}

func TestFoldIgnoresUnknownJob(t *testing.T) {
	plan := CoordinationPlan{Jobs: map[string]PlanNode{"a": {}}}
	s := Fold([]CoordinationEvent{evClaimed(1, "ghost")}, plan)
	if len(s.Jobs) != 1 || s.Jobs["a"] == nil || !reflect.DeepEqual(s.Frontier, []string{"a"}) {
		t.Fatalf("unknown-job event not ignored: %+v", s)
	}
}
