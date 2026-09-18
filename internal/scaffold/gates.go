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
		// DRIFTED SATISFIES A REQUIREMENT. The question is "will the
		// predecessor's files be there when my hooks run", and for a drifted
		// phase every one of them is — that is what drift means, as against
		// `partial`, where some are missing.
		//
		// Requiring `done` here made a phased bootstrap impossible for any
		// baseline that brands what it places. cirrus's `01-scaffold` rewrites
		// the baseline's identity out of the tree it just wrote, so by the time
		// `02-foundation` asks, `01-scaffold` reads `drifted` — and the run
		// died at step two of the bootstrap it exists to perform:
		//
		//     ✕ phase "02-foundation" requires 01-scaffold (drifted)
		//
		// `pending`, `partial` and `unknown` still fail, and each for a reason
		// the tree can back: nothing placed, a placement interrupted, or a
		// phase the tree cannot answer for at all.
		if st.State != PhaseDone && st.State != PhaseDrifted {
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

	return checkProbes(ctx, plan, opts, phase, runner)
}

// checkProbes evaluates a phase's `requires.probe`.
//
// A PENDING answer comes back AS IS, not wrapped as a gate failure, because
// "not yet" is not "never" and only the caller knows which one matters where.
// Up front, before anything is written, a pending probe means the phase that
// satisfies it has not run — possibly a phase THIS RUN is about to place — so
// the sweep defers it. At phase time it means what a pending hook means, and
// parks the same way.
//
// Conflating the two is what made a full bootstrap impossible: phase 04 asks
// whether phase 03 published its D1 and KV bindings, and up front the honest
// answer is always no:
//
//	✕ phase "04-workers" precondition "wiring" is not met: hook "wiring"
//	  (orun.secrets/exists@v1): pending — secret(s) not published yet:
//	  WIRING_CLOUDFLARE_D1, WIRING_CLOUDFLARE_KV
//
// The action had already made the distinction — its own comment says a missing
// secret is "pending, not failure … a precondition that fails hard here would
// turn 'run phase 03 first' into a broken bootstrap" — and the gate threw it
// away.
func checkProbes(ctx context.Context, plan *runPlan, opts Options, phase PhasePlan, runner ActionRunner) error {
	decl := plan.declOf(phase.Name)
	if decl == nil || decl.Requires == nil || len(decl.Requires.Probe) == 0 {
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
			if _, waiting := actions.IsPending(err); waiting {
				return err
			}
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
//
// `place` writes the phase's files between pre and post, inside the attempt, so
// a retry starts from what the phase places rather than from whatever a failed
// post hook left half-edited. Nil places nothing.
func runPhaseHooks(ctx context.Context, hr *hookRunner, decl *Phase, pre []Hook, place func() error, post []Hook) ([]string, error) {
	attempts := 1
	backoff := 0
	if decl != nil && decl.Retry != nil {
		attempts = decl.Retry.Attempts
		backoff = decl.Retry.BackoffSeconds
	}
	var lastErr error
	for attempt := 1; attempt <= attempts; attempt++ {
		hr.resetOutputs()
		ran, err := runPrePlacePost(ctx, hr, pre, place, post)
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

// runPrePlacePost is one attempt: the pre hooks, the placement, the post hooks.
// Outputs recorded by a pre hook stay readable by a post hook — the placement
// between them does not reset the phase's scope.
func runPrePlacePost(ctx context.Context, hr *hookRunner, pre []Hook, place func() error, post []Hook) ([]string, error) {
	ran, err := hr.run(ctx, pre)
	if err != nil {
		return ran, err
	}
	if place != nil {
		if err := place(); err != nil {
			return ran, fmt.Errorf("placing the phase's files: %w", err)
		}
	}
	more, err := hr.run(ctx, post)
	return append(ran, more...), err
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
