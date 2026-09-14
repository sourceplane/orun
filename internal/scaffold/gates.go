package scaffold

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Phase preconditions and retry (orun-bootstrap-engine BE-O3).

// checkRequires enforces a phase's declared preconditions before it is placed.
//
// The two halves answer different questions and neither substitutes for the
// other. `phases` asks whether earlier phases are on disk, and is answered by
// DERIVING — never by reading a record that claims a phase ran. `probe` asks
// whether what those phases deployed is still there, which no record can know:
// a lock file cannot tell you someone deleted the database binding.
func checkRequires(ctx context.Context, plan *runPlan, opts Options, phase PhasePlan, runner ActionRunner) error {
	decl := plan.declOf(phase.Name)
	if decl == nil || decl.Requires == nil {
		return nil
	}

	var unmet []string
	for _, need := range decl.Requires.Phases {
		st := derivePhase(opts.OutDir, need, plan.byPhase[need])
		if st.State != PhaseDone {
			unmet = append(unmet, fmt.Sprintf("%s (%s)", need, st.State))
		}
	}
	if len(unmet) > 0 {
		return gateErr("phase %q requires %s — run %s first",
			phase.Name, strings.Join(unmet, ", "), strings.Join(decl.Requires.Phases, ", "))
	}

	if len(decl.Requires.Probe) == 0 {
		return nil
	}
	if runner == nil {
		runner = DefaultActionRunner()
	}
	// Probes run through the same runner as hooks, so a recording runner in a
	// simulation sees them too — a phase's preconditions are part of what it
	// does, not a hidden preamble.
	hr := &hookRunner{outDir: opts.OutDir, baseDir: opts.SourceBaseDir, actions: runner}
	hr.resetOutputs()
	for _, probe := range decl.Requires.Probe {
		if err := hr.runAction(ctx, probe); err != nil {
			return gateErr("phase %q precondition %q is not met: %v", phase.Name, probe.ID, err)
		}
	}
	return nil
}

// runPhaseHooks runs a phase's hooks, retrying the whole list per the phase's
// declared policy.
//
// Retry governs HOOKS and not placement. Placement is deterministic — the same
// inputs render the same bytes, so a second attempt produces exactly what the
// first did. Hooks reach the network, and that is where a transient failure
// actually lives.
//
// The whole list is retried rather than the failing hook alone, because hooks
// within a phase are written to be idempotent (find-or-create, additive apply)
// and re-running from the top is the behaviour a baseline's phases already
// document. Resuming mid-list would require knowing which hooks are safe to
// skip, which is a claim only the hook itself could make.
func runPhaseHooks(ctx context.Context, hr *hookRunner, decl *Phase, hooks []Hook) ([]string, error) {
	attempts := 1
	backoff := 0
	if decl != nil && decl.Retry != nil {
		attempts = decl.Retry.Attempts
		backoff = decl.Retry.BackoffSeconds
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		hr.resetOutputs()
		ran, err := hr.run(ctx, hooks)
		if err == nil {
			return ran, nil
		}
		lastErr = err
		if attempt == attempts {
			break
		}
		wait := time.Duration(attempt*backoff) * time.Second
		if wait > 0 {
			select {
			case <-ctx.Done():
				return ran, ctx.Err()
			case <-time.After(wait):
			}
		}
	}
	if attempts > 1 {
		return nil, fmt.Errorf("after %d attempt(s): %w", attempts, lastErr)
	}
	return nil, lastErr
}
