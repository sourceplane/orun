package main

import (
	"context"
	"io"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/statebackend"
)

func withLaneFlags(t *testing.T, lane, job string) {
	t.Helper()
	oldLane, oldJob, oldJobs := runLaneID, runJobID, runLaneJobs
	runLaneID, runJobID = lane, job
	t.Cleanup(func() { runLaneID, runJobID, runLaneJobs = oldLane, oldJob, oldJobs })
}

func TestSelectLane(t *testing.T) {
	plan := &model.Plan{
		Jobs:  []model.PlanJob{{ID: "a"}, {ID: "b"}},
		Lanes: []model.PlanLane{{ID: "stage/auto-1", Jobs: []string{"a", "b"}, Concurrency: 4}},
	}

	withLaneFlags(t, "stage/auto-1", "")
	conc, err := selectLane(plan)
	if err != nil || conc != 4 || !reflect.DeepEqual(runLaneJobs, []string{"a", "b"}) || runJobID != "" {
		t.Fatalf("selectLane = %d %v %v job=%q", conc, err, runLaneJobs, runJobID)
	}

	withLaneFlags(t, "nope", "")
	if _, err := selectLane(plan); err == nil || !strings.Contains(err.Error(), "stage/auto-1") {
		t.Fatalf("unknown lane must list the lanes: %v", err)
	}

	// A plan without lanes: --lane <id> is --job <id>.
	withLaneFlags(t, "a", "")
	conc, err = selectLane(&model.Plan{Jobs: plan.Jobs})
	if err != nil || conc != 0 || runLaneJobs != nil || runJobID != "a" {
		t.Fatalf("fallback = %d %v %v job=%q", conc, err, runLaneJobs, runJobID)
	}

	withLaneFlags(t, "", "a")
	if _, err := selectLane(plan); err != nil || runLaneJobs != nil || runJobID != "a" {
		t.Fatalf("--job alone is untouched: %v", err)
	}
}

// fakeRunState serves LoadRunState from a sequence of snapshots.
type fakeRunState struct {
	statebackend.Backend
	mu     sync.Mutex
	states []map[string]string
	calls  int
}

func (f *fakeRunState) LoadRunState(context.Context, string) (*execmodel.ExecState, *execmodel.ExecMetadata, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	i := f.calls
	if i >= len(f.states) {
		i = len(f.states) - 1
	}
	f.calls++
	st := &execmodel.ExecState{Jobs: map[string]*execmodel.JobState{}}
	for id, status := range f.states[i] {
		st.Jobs[id] = &execmodel.JobState{Status: status}
	}
	return st, nil, nil
}

func TestAwaitRemoteUpstreams(t *testing.T) {
	oldWait, oldPoll := laneUpstreamWait, laneUpstreamPoll
	laneUpstreamWait, laneUpstreamPoll = 200*time.Millisecond, time.Millisecond
	t.Cleanup(func() { laneUpstreamWait, laneUpstreamPoll = oldWait, oldPoll })
	plan := &model.Plan{Jobs: []model.PlanJob{{ID: "up"}, {ID: "down", DependsOn: []string{"up"}}}}
	ctx := context.Background()

	waits := &fakeRunState{states: []map[string]string{{"up": "pending"}, {"up": "running"}, {"up": "completed"}}}
	if err := awaitRemoteUpstreams(ctx, waits, "run", plan, "down", []string{"up"}, io.Discard, false, false); err != nil || waits.calls != 3 {
		t.Fatalf("want success after 3 polls: %v (%d)", err, waits.calls)
	}

	failed := &fakeRunState{states: []map[string]string{{"up": "failed"}}}
	if err := awaitRemoteUpstreams(ctx, failed, "run", plan, "down", []string{"up"}, io.Discard, false, false); err == nil || !strings.Contains(err.Error(), "upstream up failed") {
		t.Fatalf("want the failure: %v", err)
	}

	// --retry: a failed upstream may be re-run by its own lane; keep waiting.
	retried := &fakeRunState{states: []map[string]string{{"up": "failed"}, {"up": "completed"}}}
	if err := awaitRemoteUpstreams(ctx, retried, "run", plan, "down", []string{"up"}, io.Discard, false, true); err != nil {
		t.Fatalf("retry run must wait out a failed upstream: %v", err)
	}

	// orun#737: a lane leaves a job unrun only below a failed upstream, so the
	// job stays pending on the platform. A dependent in another lane must see
	// it as blocked on the first poll, not wait out the upstream timeout.
	chain := &model.Plan{Jobs: []model.PlanJob{{ID: "root"}, {ID: "up", DependsOn: []string{"root"}}, {ID: "down", DependsOn: []string{"up"}}}}
	blocked := &fakeRunState{states: []map[string]string{{"root": "failed", "up": "pending"}}}
	if err := awaitRemoteUpstreams(ctx, blocked, "run", chain, "down", []string{"up"}, io.Discard, false, false); err == nil || !strings.Contains(err.Error(), "blocked by an upstream failure") || blocked.calls != 1 {
		t.Fatalf("want the blocked upstream on the first poll: %v (%d polls)", err, blocked.calls)
	}

	stuck := &fakeRunState{states: []map[string]string{{"up": "running"}}}
	if err := awaitRemoteUpstreams(ctx, stuck, "run", plan, "down", []string{"up"}, io.Discard, false, false); err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("want a timeout: %v", err)
	}
}
