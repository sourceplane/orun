package scaffold

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/actions"
)

// Phase preconditions and retry (orun-bootstrap-engine BE-O3).

// checkRequires enforces a phase's declared preconditions before it is placed.
//
// The two halves answer different questions and neither substitutes for the
// other. `phases` asks whether earlier phases are on disk, and is answered by
// DERIVING — never by reading a record that claims a phase ran. `probe` asks
// whether what those phases deployed is still there, which no record can know:
// a lock file cannot tell you someone deleted the database binding.
//
// `earlier` names the phases this same run is placing BEFORE this one, and it
// is what makes a whole-blueprint run possible at all (BE-O11). Preconditions
// are checked before the first byte is written, so a run placing every phase
// would otherwise have `02-foundation` ask whether `01-scaffold` is on disk
// during the very run that is about to write it — and always hear no. The
// question a requirement is really asking is "will this be there when my hooks
// run", and for a phase ordered earlier in the same run the answer is yes.
func checkRequires(ctx context.Context, plan *runPlan, opts Options, phase PhasePlan, runner ActionRunner, earlier map[string]bool) error {
	decl := plan.declOf(phase.Name)
	if decl == nil || decl.Requires == nil {
		return nil
	}

	var unmet []string
	for _, need := range decl.Requires.Phases {
		if earlier[need] {
			continue
		}
		st := derivePhaseOf(opts.OutDir, need, plan.byPhase[need], plan.declOf(need))
		if st.State != PhaseDone {
			unmet = append(unmet, fmt.Sprintf("%s (%s)", need, st.State))
		}
	}
	if len(unmet) > 0 {
		// `unknown` reads differently from `pending` and the message says so:
		// a required hook-only phase is not something the tree can vouch for,
		// so the gate fails CLOSED and names the reason rather than passing on
		// the strength of a phase having written no files.
		return gateErr("phase %q requires %s — run %s first%s",
			phase.Name, strings.Join(unmet, ", "), strings.Join(decl.Requires.Phases, ", "),
			unknownNote(unmet))
	}

	if len(decl.Requires.Probe) == 0 {
		return nil
	}
	// A PROBE GATES THE WORK, NOT THE BYTES.
	//
	// Placement writes files; it does not deploy, and it does not read a
	// secret. What `requires.probe` protects is the phase's HOOKS — the
	// landing, the convergence, the thing that will fail if the database
	// binding an earlier phase published has since been deleted. With hooks
	// off there is nothing to protect, and probing anyway means a placement
	// that cannot happen without a workspace and a live provider.
	//
	// That is not hypothetical: it is what made a baseline's own dry
	// instantiation impossible, which is a check every baseline needs and none
	// could run. `requires.phases` still applies either way — it asks the tree,
	// which is always there to ask.
	if !opts.RunHooks {
		return nil
	}
	if runner == nil {
		runner = DefaultActionRunner()
	}
	// Probes run through the same runner as hooks, so a recording runner in a
	// simulation sees them too — a phase's preconditions are part of what it
	// does, not a hidden preamble.
	hr := &hookRunner{
		outDir:  opts.OutDir,
		baseDir: opts.SourceBaseDir,
		actions: runner,
		inputs:  plan.values.nonSecretFields(),
	}
	if decl != nil {
		hr.phase = *decl
	}
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
		// Pending is never retried, in any slot. Retrying a wait turns "still
		// running" into an error after N attempts, when the honest answer is
		// that it is still running. `await` is where a wait BELONGS, not the
		// only place one is honoured.
		if _, waiting := actions.IsPending(err); waiting {
			return ran, err
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

// unknownNote explains a requirement the product tree cannot answer.
func unknownNote(unmet []string) string {
	for _, u := range unmet {
		if strings.Contains(u, string(PhaseUnknown)) {
			return ". A phase reported `unknown` places no files, so placement cannot tell whether it ran — its record is in the task plane and the deployment, not in this repository"
		}
	}
	return ""
}
