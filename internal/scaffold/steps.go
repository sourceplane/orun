package scaffold

import (
	"context"
	"errors"

	"github.com/sourceplane/orun/internal/actions"
)

// Saying each step as it happens (orun-bootstrap-engine BE-O6; orun-cloud
// saas-build-panel BV3).
//
// A phase's hooks are its long part — a landing waits on CI, a watch on a
// convergence — and a build that reported nothing until all of them had
// finished left its page on one line for an hour. So each hook is reported
// as it runs:
//
//	running   as an action with an operator line (Spec.Doing) starts
//	fact      what it made, as it learns it — a pull request the moment it
//	          is opened — and again only when that changes
//	done      the hook's authored line (`narrate:`), else the action's Did
//	          over its outputs, the moment it completes
//
// Bookkeeping (finding a task) has neither a Doing nor a Did, and says
// nothing. A `quiet` hook files no facts and leaves no generated line; its
// own `narrate:` still speaks.
//
// The run's own hooks — preInstantiate, postInstantiate — are reported the
// same way under `phase: ""`: the epic a build opens before its first phase
// is the first thing a reader sees it make.

// reportSteps wires hr's step callbacks to the stream for one phase, or for
// the run's own hooks when phase is "". scope is the narration scope at the
// moment a hook completes — it reads this phase's hook outputs, including
// the hook's own.
func reportSteps(ctx context.Context, em *emitter, hr *hookRunner, phase string, scope func() map[string]any) {
	hr.onStart = func(h Hook) {
		if h.Quiet {
			return
		}
		if spec, ok := actions.Lookup(h.Uses); ok && spec.Doing != "" {
			em.emit(ctx, Event{Phase: phase, Step: h.ID, State: EventRunning, Narration: spec.Doing})
		}
	}
	hr.onProgress = func(h Hook, params map[string]any, out map[string]string) {
		em.hookFacts(ctx, phase, h, params, out, false)
	}
	// What a failing or parked hook had is sent before the failure or the
	// wait is: a fact is never sent after its phase's last transition.
	hr.onFail = func(h Hook, params map[string]any, out map[string]string, err error) {
		_, pending := actions.IsPending(err)
		em.hookFacts(ctx, phase, h, params, out, !pending)
	}
	hr.onDone = func(h Hook, params map[string]any, out map[string]string) {
		em.hookFacts(ctx, phase, h, params, out, false)
		line := hookNarration(phase, h, scope())
		if line == "" && !h.Quiet && h.IsAction() {
			line = actions.DidOf(h.Uses, params, out)
		}
		if line != "" {
			em.emit(ctx, Event{Phase: phase, Step: h.ID, State: EventDone, Narration: line})
		}
	}
}

// runStepFailed is the event for a run-level hook (preInstantiate,
// postInstantiate) that failed. It names the hook, so the run's own
// `failed`, which follows it, is not the only word on what broke.
func runStepFailed(err error) Event {
	meta, step := failureMeta(nil, err)
	name := step
	if name == "" {
		name = "A step before the first phase"
	}
	return Event{Step: step, State: EventFailed, Narration: generatedNarration(name, EventFailed),
		Detail: err.Error(), Meta: meta}
}

// pendingStep is the HOOK a pending error came from — the id a reader finds
// in the blueprint — falling back to the action's id when the error carries
// no hook.
func pendingStep(err error, pe *actions.PendingError) string {
	var he *HookError
	if errors.As(err, &he) && he.ID != "" {
		return he.ID
	}
	if pe != nil {
		return pe.ID
	}
	return ""
}

// endRun is the run's last word: `done`, or `failed` with the error, under
// `phase: ""` and no step. A parked run is waiting, not over, and says
// nothing.
func (e *emitter) endRun(ctx context.Context, name string, err error) {
	var parked *ParkedError
	switch {
	case errors.As(err, &parked):
		return
	case err != nil:
		e.emit(ctx, Event{State: EventFailed, Narration: generatedNarration(name, EventFailed), Detail: err.Error()})
	default:
		e.emit(ctx, Event{State: EventDone, Narration: generatedNarration(name, EventDone)})
	}
}
