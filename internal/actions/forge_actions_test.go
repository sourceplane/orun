package actions

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/forge"
)

// fakeWatcher plays a scripted sequence of run states.
type fakeWatcher struct {
	states []forge.Run // one per GetRun call; the last repeats
	latest *forge.Run
	// forCommit is the run GitHub has for the watched commit; it appears only
	// after commitAfter lookups, the way a push's run does.
	forCommit   *forge.Run
	commitAfter int
	lookups     int
	watched     []int64 // the run ids GetRun was asked about
	calls       int
	reruns      int
	rerunErr    error
	failed      []forge.FailedJob
}

func (f *fakeWatcher) LatestRun(context.Context, string, string, string) (*forge.Run, error) {
	return f.latest, nil
}

func (f *fakeWatcher) RunForCommit(context.Context, string, string, string, string) (*forge.Run, error) {
	f.lookups++
	if f.forCommit == nil || f.lookups <= f.commitAfter {
		return nil, nil
	}
	return f.forCommit, nil
}

func (f *fakeWatcher) GetRun(_ context.Context, _, _ string, id int64) (*forge.Run, error) {
	f.watched = append(f.watched, id)
	i := f.calls
	f.calls++
	if i >= len(f.states) {
		i = len(f.states) - 1
	}
	r := f.states[i]
	r.ID = id
	return &r, nil
}

func (f *fakeWatcher) RerunFailed(context.Context, string, string, int64) error {
	f.reruns++
	return f.rerunErr
}

func (f *fakeWatcher) FailedJobs(context.Context, string, string, int64) ([]forge.FailedJob, error) {
	return f.failed, nil
}

func watchInput(t *testing.T, params map[string]any) Input {
	t.Helper()
	resolved, err := Resolve("orun.run/watch@v1", params)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return Input{Params: resolved}
}

func TestWatchReturnsWhenTheRunIsGreen(t *testing.T) {
	f := &fakeWatcher{
		latest: &forge.Run{ID: 7},
		states: []forge.Run{{Status: "in_progress"}, {Status: "completed", Conclusion: "success"}},
	}
	in := watchInput(t, map[string]any{"repo": "acme/product", "waitSeconds": 600, "pollSeconds": 1})
	res, err := watchOn(context.Background(), f, in, func(time.Duration) {})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if res.Outputs["conclusion"] != "success" {
		t.Errorf("expected success, got %v", res.Outputs)
	}
	if f.reruns != 0 {
		t.Error("a green run must not be resumed")
	}
}

// A convergence that trips on something transient heals in place; that is what
// the CI being resume-capable is for.
func TestWatchResumesAFailureAndSucceeds(t *testing.T) {
	f := &fakeWatcher{
		latest: &forge.Run{ID: 7},
		states: []forge.Run{
			{Status: "completed", Conclusion: "failure"},
			{Status: "completed", Conclusion: "success"},
		},
	}
	in := watchInput(t, map[string]any{"repo": "acme/product", "waitSeconds": 600, "pollSeconds": 1})
	res, err := watchOn(context.Background(), f, in, func(time.Duration) {})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if f.reruns != 1 {
		t.Errorf("expected one resume, got %d", f.reruns)
	}
	if res.Outputs["resumes"] != "1" {
		t.Errorf("resumes should be reported, got %v", res.Outputs)
	}
}

// A real regression fails every resume and surfaces after the budget. "Flake"
// is not a root cause, and an unbounded loop would retry a genuine break
// forever.
func TestWatchGivesUpAfterTheBudgetAndNamesTheLanes(t *testing.T) {
	f := &fakeWatcher{
		latest: &forge.Run{ID: 7},
		states: []forge.Run{{Status: "completed", Conclusion: "failure"}},
		failed: []forge.FailedJob{{Name: "test", Conclusion: "failure"}, {Name: "lint", Conclusion: "failure"}},
	}
	in := watchInput(t, map[string]any{
		"repo": "acme/product", "waitSeconds": 600, "pollSeconds": 1, "resumeBudget": 2,
	})
	_, err := watchOn(context.Background(), f, in, func(time.Duration) {})
	if err == nil {
		t.Fatal("a persistent failure must surface")
	}
	if f.reruns != 2 {
		t.Errorf("expected exactly the budget of resumes, got %d", f.reruns)
	}
	if !strings.Contains(err.Error(), "test (failure)") {
		t.Errorf("the failed lanes are the one thing an operator needs; got %v", err)
	}
}

// The resume is best effort; the diagnosis is not.
func TestWatchStillReportsTheLanesWhenTheResumeCannotBeIssued(t *testing.T) {
	f := &fakeWatcher{
		latest:   &forge.Run{ID: 7},
		states:   []forge.Run{{Status: "completed", Conclusion: "failure"}},
		rerunErr: fmt.Errorf("403 no actions: write"),
		failed:   []forge.FailedJob{{Name: "deploy", Conclusion: "failure", URL: "https://github.com/acme/product/actions/runs/7/job/9", Step: "Run set -euo pipefail", StepNumber: 6, StepURL: "https://github.com/acme/product/actions/runs/7/job/9#step:6:1"}},
	}
	in := watchInput(t, map[string]any{"repo": "acme/product", "waitSeconds": 600, "pollSeconds": 1})
	_, err := watchOn(context.Background(), f, in, func(time.Duration) {})
	if err == nil {
		t.Fatal("expected a failure")
	}
	if !strings.Contains(err.Error(), "deploy (failure)") {
		t.Errorf("losing the diagnosis because the retry failed is the costly part; got %v", err)
	}
	if !strings.Contains(err.Error(), "403") {
		t.Errorf("the reason the resume failed should also be visible; got %v", err)
	}
}

func TestWatchReportsPendingRatherThanBlockingForever(t *testing.T) {
	f := &fakeWatcher{latest: &forge.Run{ID: 7}, states: []forge.Run{{Status: "in_progress"}}}
	in := watchInput(t, map[string]any{"repo": "acme/product"}) // waitSeconds defaults to 0
	res, err := watchOn(context.Background(), f, in, func(time.Duration) {})
	if err != nil {
		t.Fatalf("a running convergence is not an error: %v", err)
	}
	if res.Pending == nil {
		t.Fatal("expected pending")
	}
	if !strings.Contains(res.Pending.Reason, "in_progress") {
		t.Errorf("the reason should say what it is waiting on; got %q", res.Pending.Reason)
	}
}

// A repository whose CI has not landed yet has nothing to converge. Waiting for
// a run that will never exist would hang the first phase of every bootstrap.
func TestWatchWithNoRunAtAllIsARealAnswer(t *testing.T) {
	f := &fakeWatcher{latest: nil}
	in := watchInput(t, map[string]any{"repo": "acme/product", "waitSeconds": 600})
	res, err := watchOn(context.Background(), f, in, func(time.Duration) {})
	if err != nil {
		t.Fatalf("no run is not an error: %v", err)
	}
	if res.Pending != nil {
		t.Errorf("no run must not park the phase: %v", res.Pending)
	}
	if res.Outputs["conclusion"] != "none" {
		t.Errorf("expected conclusion none, got %v", res.Outputs)
	}
}

func TestWatchRejectsAMalformedRepo(t *testing.T) {
	f := &fakeWatcher{}
	in := watchInput(t, map[string]any{"repo": "product"})
	if _, err := watchOn(context.Background(), f, in, func(time.Duration) {}); err == nil {
		t.Fatal("repo must be owner/name")
	}
}

// The landing merged a moment ago; GitHub has not registered its run yet, and
// the branch's newest run is the PREVIOUS phase's, already green. The watch
// must wait for the landed commit's run rather than report that one.
func TestWatchWaitsForTheLandedCommitsRunNotTheBranchsNewest(t *testing.T) {
	f := &fakeWatcher{
		latest:      &forge.Run{ID: 1, Status: "completed", Conclusion: "success"},
		forCommit:   &forge.Run{ID: 9},
		commitAfter: 2,
		states:      []forge.Run{{Status: "in_progress"}, {Status: "completed", Conclusion: "success"}},
	}
	in := watchInput(t, map[string]any{"repo": "acme/product", "sha": "abc123", "waitSeconds": 600, "pollSeconds": 1})
	res, err := watchOn(context.Background(), f, in, func(time.Duration) {})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if res.Outputs["runId"] != "9" {
		t.Errorf("watched run %s, want the landed commit's run 9", res.Outputs["runId"])
	}
	for _, id := range f.watched {
		if id == 1 {
			t.Error("the previous phase's run was consulted as if it were this landing's convergence")
		}
	}
}

// If the commit's run never appears inside the wait, that is "not yet" —
// pending — and never "converged", however green the branch looks.
func TestWatchIsPendingWhileTheLandedCommitHasNoRun(t *testing.T) {
	f := &fakeWatcher{latest: &forge.Run{ID: 1, Status: "completed", Conclusion: "success"}}
	in := watchInput(t, map[string]any{"repo": "acme/product", "sha": "abc123"})
	res, err := watchOn(context.Background(), f, in, func(time.Duration) {})
	if err != nil {
		t.Fatalf("watch: %v", err)
	}
	if res.Pending == nil {
		t.Fatalf("want pending, got outputs %v", res.Outputs)
	}
	if !strings.Contains(res.Pending.Reason, "abc123") {
		t.Errorf("the pending reason should name the commit, got %q", res.Pending.Reason)
	}
}
