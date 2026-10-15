package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
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
			if lane.Concurrency == 0 {
				// concurrency: auto — the runner's core count, on this machine,
				// divided by the most cores one of the lane's jobs declared
				// (specs/orun-runner-efficiency §5.2, §5.4).
				per := 1
				if lane.VCPU > per {
					per = lane.VCPU
				}
				n := runtime.NumCPU() / per
				if n < 1 {
					n = 1
				}
				if runConcurrency == 0 {
					note := ""
					if per > 1 {
						note = fmt.Sprintf(", %d per job", per)
					}
					fmt.Fprintf(os.Stderr, "  %s lane %s: concurrency auto → %d (%d CPUs%s)\n", ui.Dim(ui.ColorEnabledForWriter(os.Stderr), "○"), id, n, runtime.NumCPU(), note)
				}
				return n, nil
			}
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

// awaitRemoteUpstreams waits on the run's remote state until every upstream
// has completed. A failed upstream (or one blocked by a failure further up)
// ends the wait with an error, except on --retry runs, where the upstream's
// own lane may be re-running it. A running upstream whose heartbeat is older
// than staleAfter() has lost its runner (a reclaimed spot instance): the
// wait ends too, naming it, rather than holding this runner for the
// upstream timeout; nil staleAfter uses the default. The wait is bounded by
// the plan's execution.lanes.upstreamWait, else laneUpstreamWait.
//
// A backend that can long-poll its event stream (runEventWaiter) is woken
// by the next event instead of polled on a timer: the job starts within
// moments of its upstream finishing, and a waiting runner costs the server
// one open request rather than one state read per interval
// (specs/orun-runner-efficiency §5.7).
func awaitRemoteUpstreams(ctx context.Context, backend statebackend.Backend, runID string, plan *model.Plan, jobID string, upstreams []string, stdout interface{ Write([]byte) (int, error) }, color bool, retryRun bool, staleAfter func() time.Duration) error {
	if staleAfter == nil {
		staleAfter = func() time.Duration { return defaultStaleAfter }
	}
	wait := laneUpstreamWait
	if plan != nil && plan.Metadata.Lanes != nil {
		if d, err := time.ParseDuration(strings.TrimSpace(plan.Metadata.Lanes.UpstreamWait)); err == nil && d > 0 {
			wait = d
		}
	}
	deadline := time.Now().Add(wait)
	delay := laneUpstreamPoll
	announced := false
	events := newEventWait(ctx, backend, runID)
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
				case js != nil && js.Status == "running" && heartbeatOlderThan(js.HeartbeatAt, staleAfter()):
					return fmt.Errorf("job %s: upstream %s lost its runner (no heartbeat for %s); rerun its lane with --retry", jobID, up, staleAfter().Round(time.Second))
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
			return fmt.Errorf("job %s: upstream wait timeout (%s) exceeded", jobID, wait)
		}
		woke, blocked := events.wait(ctx, delay)
		if woke {
			continue // news: re-read now, and keep the short interval
		}
		if !blocked {
			if err := sleepOrDone(ctx, delay); err != nil {
				return err
			}
		}
		if delay = nextBackoff(delay, 15*time.Second); delay > wait {
			delay = wait
		}
	}
}

// eventWait is the event-driven half of a remote wait: on a backend that
// long-polls its event stream it blocks until the next event or until the
// long-poll lapses, and reports which; on any other backend it does not block
// and the caller sleeps as before.
//
// A LAPSED LONG-POLL IS NOT AN EVENT. The first version reported every
// successful long-poll as a wake, a timeout included. A lane job waiting on
// another lane's job then re-read the whole run every 2 s for as long as it
// waited (its backoff never grew, because a wake skips the backoff), and a
// --job's wait re-read on every timeout: three requests, the whole event log
// and the plan object each time (specs/orun-sync-efficiency F1). Now a wake
// is reported only when the head sequence advanced, and a lapse is reported
// as time already spent, so the caller neither sleeps again nor treats it as
// news.
type eventWait struct {
	waiter   runEventWaiter
	progress runProgressWaiter // when the backend can sleep through lease renewals
	runID    string
	seq      int
}

// runProgressWaiter is the optional refinement of runEventWaiter: a wait that
// the server holds through lease renewals and that reports whether anything
// past the cursor was more than a renewal. The runner's waits use it, so a
// job waiting on its upstreams is woken by their progress, not by every
// heartbeat in the run; `status --watch` keeps the plain wait, since it
// shows the heartbeats.
type runProgressWaiter interface {
	WaitForRunProgress(ctx context.Context, runID string, sinceSeq, waitSeconds int) (head int, progressed bool, err error)
}

// eventLongPollSeconds is how long one event wait blocks before the caller
// re-reads state regardless: well under the server's cap, and the interval
// at which a lost heartbeat is noticed.
const eventLongPollSeconds = 15

func newEventWait(ctx context.Context, backend statebackend.Backend, runID string) *eventWait {
	w, ok := backend.(runEventWaiter)
	if !ok || w == nil {
		return &eventWait{}
	}
	e := &eventWait{waiter: w, runID: runID}
	e.progress, _ = backend.(runProgressWaiter)
	if head, err := w.WaitForRunEvents(ctx, runID, 0, 0); err == nil {
		e.seq = head
	}
	return e
}

// wait blocks for up to max (at most eventLongPollSeconds) on the run's next
// event. woke is true when new events arrived; blocked is true when the wait
// held for its interval, whether or not events came, so the caller does not
// sleep the same interval again. On a backend that cannot long-poll, or once
// the long-poll fails, both are false and the caller sleeps as before.
func (e *eventWait) wait(ctx context.Context, max time.Duration) (woke, blocked bool) {
	if e == nil || e.waiter == nil {
		return false, false
	}
	secs := eventLongPollSeconds
	if max > 0 && max < time.Duration(secs)*time.Second {
		secs = int(max / time.Second)
		if secs < 1 {
			secs = 1
		}
	}
	var head int
	var err error
	if e.progress != nil {
		var progressed bool
		head, progressed, err = e.progress.WaitForRunProgress(ctx, e.runID, e.seq, secs)
		if err == nil {
			// The cursor moves past everything the page carried; only
			// progress is a wake.
			if head > e.seq {
				e.seq = head
			}
			return progressed, true
		}
	} else {
		head, err = e.waiter.WaitForRunEvents(ctx, e.runID, e.seq, secs)
	}
	if err != nil {
		if ctx.Err() != nil {
			return false, false
		}
		e.waiter, e.progress = nil, nil // fall back to polling for the rest of this wait
		return false, false
	}
	woke = head > e.seq
	if woke {
		e.seq = head
	}
	return woke, true
}

// heartbeatOlderThan reports whether a job's last heartbeat is older than
// max; an unparseable or missing heartbeat is not stale.
func heartbeatOlderThan(heartbeatAt string, max time.Duration) bool {
	if heartbeatAt == "" {
		return false
	}
	hb, err := time.Parse(time.RFC3339, heartbeatAt)
	if err != nil {
		return false
	}
	return time.Since(hb) > max
}
