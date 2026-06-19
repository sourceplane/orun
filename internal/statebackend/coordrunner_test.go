package statebackend

import (
	"reflect"
	"testing"
)

func TestActionForClaim(t *testing.T) {
	cases := []struct {
		name string
		in   ClaimOutcome
		want RunnerAction
	}{
		{"claimed → execute", ClaimOutcome{Kind: OutcomeClaimed, LeaseEpoch: 1}, ActionExecute},
		{"cached → adopt", ClaimOutcome{Kind: OutcomeCached, ResultDigest: "sha256:x"}, ActionAdoptCached},
		{"deps_not_ready → wait", ClaimOutcome{Kind: OutcomeRejected, Reason: "deps_not_ready"}, ActionWaitDeps},
		{"job_held → skip", ClaimOutcome{Kind: OutcomeRejected, Reason: "job_held"}, ActionSkip},
		{"terminal → skip", ClaimOutcome{Kind: OutcomeRejected, Reason: "terminal"}, ActionSkip},
		{"not_found → skip", ClaimOutcome{Kind: OutcomeRejected, Reason: "not_found"}, ActionSkip},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := ActionForClaim(c.in); got != c.want {
				t.Fatalf("ActionForClaim(%+v) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestActionForHeartbeat(t *testing.T) {
	if ActionForHeartbeat(true) != ActionStop {
		t.Fatal("lost lease must stop the runner")
	}
	if ActionForHeartbeat(false) != ActionExecute {
		t.Fatal("a held lease keeps the runner working")
	}
}

func TestClaimableJobsIsFrontier(t *testing.T) {
	plan := linearPlan()
	// created + a succeeds → b becomes the frontier
	events := []CoordinationEvent{
		evCreated(1), evClaimed(2, "a"), evSucceeded(3, "a", "sha256:ra"),
	}
	state := Fold(events, plan)
	if got := ClaimableJobs(state); !reflect.DeepEqual(got, []string{"b"}) {
		t.Fatalf("ClaimableJobs = %v, want [b]", got)
	}
}
