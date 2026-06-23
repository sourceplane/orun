package statebackend

import (
	"context"
	"fmt"
)

// Runner orchestration loop (NC2+NC3 capstone). Ties the pure driver (NC2:
// ClaimableJobs / ActionForClaim) to the HTTP client (NC3: Claim / Complete /
// ReadLog) and the fold (NC0): read the log → fold → for each runnable job, try
// the conditional :claim and act on the outcome, until the run is terminal. The
// server's :claim is the authority for who-runs-what; this loop only proposes
// from the local frontier, so it is safe to run many in parallel.

// JobExecutor runs one claimed job and returns its result digest (the content
// address of its outputs) or an error. The loop reports the outcome via :complete.
type JobExecutor func(ctx context.Context, jobID string) (resultDigest string, err error)

// RunLoopOptions configures one runner's drive of a run.
type RunLoopOptions struct {
	RunID    string
	RunnerID string
	Plan     CoordinationPlan
	Execute  JobExecutor
	// MaxTicks bounds the read→claim→act cycles (a stuck/non-converging run
	// fails closed rather than spinning forever). 0 disables the bound.
	MaxTicks int
}

func runTerminal(phase string) bool {
	return phase == "succeeded" || phase == "failed" || phase == "canceled"
}

// RunLoop drives the run to a terminal phase: on each tick it folds the current
// log, then for every job on the runnable frontier it claims and acts —
// executing+completing jobs it wins, adopting memoized hits, and leaving held or
// dep-blocked jobs for a later tick (or another runner). It returns nil once the
// run is terminal.
func (c *CoordClient) RunLoop(ctx context.Context, opts RunLoopOptions) error {
	for tick := 0; opts.MaxTicks == 0 || tick < opts.MaxTicks; tick++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		events, err := c.ReadLog(ctx, opts.RunID, 0, 0)
		if err != nil {
			return err
		}
		state := Fold(events, opts.Plan)
		if runTerminal(state.Phase) {
			return nil
		}

		progressed := false
		for _, jobID := range ClaimableJobs(state) {
			outcome, err := c.Claim(ctx, opts.RunID, jobID, ClaimRequest{RunnerID: opts.RunnerID})
			if err != nil {
				return err
			}
			switch ActionForClaim(outcome) {
			case ActionExecute:
				if err := c.executeAndComplete(ctx, opts, jobID, outcome.LeaseEpoch); err != nil {
					return err
				}
				progressed = true
			case ActionAdoptCached:
				// The memoized result is already recorded server-side; nothing to run.
				progressed = true
			case ActionWaitDeps, ActionSkip, ActionStop:
				// Not ours to advance this tick — another runner holds it, its deps
				// aren't ready, or our lease was lost. Re-fold next tick.
			}
		}

		if !progressed {
			// No frontier job advanced and the run isn't terminal: either another
			// runner is mid-flight (re-fold next tick) or we've hit the bound.
			if opts.MaxTicks != 0 && tick+1 >= opts.MaxTicks {
				return fmt.Errorf("run %s did not converge within %d ticks (phase=%s)", opts.RunID, opts.MaxTicks, state.Phase)
			}
		}
	}
	return fmt.Errorf("run %s exceeded MaxTicks=%d", opts.RunID, opts.MaxTicks)
}

// executeAndComplete runs a won job and reports its terminal outcome. A lost
// lease on :complete is not an error for the loop — another runner took over.
func (c *CoordClient) executeAndComplete(ctx context.Context, opts RunLoopOptions, jobID string, leaseEpoch int) error {
	digest, execErr := opts.Execute(ctx, jobID)
	req := CompleteRequest{RunnerID: opts.RunnerID, LeaseEpoch: leaseEpoch}
	if execErr != nil {
		req.Outcome = "failed"
		req.ErrorText = execErr.Error()
	} else {
		req.Outcome = "succeeded"
		req.ResultDigest = digest
	}
	if _, err := c.Complete(ctx, opts.RunID, jobID, req); err != nil {
		return err
	}
	return nil
}
