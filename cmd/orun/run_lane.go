package main

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/statebackend"
	"github.com/sourceplane/orun/internal/ui"
)

// laneUpstreamWait bounds how long a lane job waits for upstreams in other
// lanes. Longer than a single job's claim wait: an upstream lane may itself be
// queued behind the workflow's max-parallel. laneUpstreamPoll is the first
// poll interval, backing off to 15s.
var (
	laneUpstreamWait = 60 * time.Minute
	laneUpstreamPoll = 2 * time.Second
)

// selectLane resolves --lane against the plan (specs/orun-lanes §8). On a plan
// with lanes it sets runLaneJobs and returns the lane's concurrency; on a plan
// without lanes, --lane <id> is --job <id>, so a workflow that switched to
// --lane works against old plans and new ones alike.
func selectLane(plan *model.Plan) (int, error) {
	runLaneJobs = nil
	id := strings.TrimSpace(runLaneID)
	if id == "" {
		return 0, nil
	}
	if runJobID != "" {
		return 0, fmt.Errorf("--lane and --job are mutually exclusive")
	}
	if len(plan.Lanes) == 0 {
		runJobID = id
		return 0, nil
	}
	for _, lane := range plan.Lanes {
		if lane.ID == id {
			runLaneJobs = append([]string{}, lane.Jobs...)
			return lane.Concurrency, nil
		}
	}
	ids := make([]string, 0, len(plan.Lanes))
	for _, lane := range plan.Lanes {
		ids = append(ids, lane.ID)
	}
	sort.Strings(ids)
	return 0, fmt.Errorf("lane %q not found in plan (lanes: %s)", id, strings.Join(ids, ", "))
}

// awaitRemoteUpstreams polls the run's remote state until every upstream has
// completed. A failed upstream (or one blocked by a failure further up) ends
// the wait with an error, except on --retry runs, where the upstream's own
// lane may be re-running it.
func awaitRemoteUpstreams(ctx context.Context, backend statebackend.Backend, runID string, plan *model.Plan, jobID string, upstreams []string, stdout interface{ Write([]byte) (int, error) }, color bool, retryRun bool) error {
	deadline := time.Now().Add(laneUpstreamWait)
	delay := laneUpstreamPoll
	announced := false
	for {
		state, _, err := backend.LoadRunState(ctx, runID)
		if err == nil && state != nil {
			var waiting []string
			for _, up := range upstreams {
				js := state.Jobs[up]
				switch {
				case js != nil && js.Status == "completed":
					continue
				case js != nil && js.Status == "failed" && !retryRun:
					return fmt.Errorf("job %s: upstream %s failed", jobID, up)
				case (js == nil || js.Status == "pending") && !retryRun && isTransitivelyBlocked(state, plan, up):
					return fmt.Errorf("job %s: upstream %s is blocked by an upstream failure", jobID, up)
				}
				waiting = append(waiting, up)
			}
			if len(waiting) == 0 {
				return nil
			}
			if !announced {
				fmt.Fprintf(stdout, "  %s %s waiting for %s\n", ui.Dim(color, "○"), jobID, strings.Join(waiting, ", "))
				announced = true
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("job %s: upstream wait timeout (%s) exceeded", jobID, laneUpstreamWait)
		}
		if err := sleepOrDone(ctx, delay); err != nil {
			return err
		}
		if delay = nextBackoff(delay, 15*time.Second); delay > laneUpstreamWait {
			delay = laneUpstreamWait
		}
	}
}
