package scaffold

import (
	"context"
	"fmt"
	"strings"

	"github.com/sourceplane/orun/internal/actions"
)

// The run's background hooks (`background: true` on hooks.preInstantiate).
//
// A build's preInstantiate opens its work — the epic, a milestone per phase, a
// task per landing — and the first phase waited for all of it before it did
// anything. That is five to ten platform round trips, 16–50 s of a todo
// build, and nothing in the phase needs them until it lands: the provider
// check, the placement, the rebrand, the repository and the secrets do not
// touch the task plane. Only the landing does (a pull request opens FOR a
// task), and the phase's `task` hook that finds the key it lands under.
//
// So a preInstantiate hook marked `background: true` — and every hook after
// it; the background is a suffix of the list, run in order — starts with the
// run and runs beside the phases:
//
//   - a hook whose action works on the task plane (Spec.TaskPlane: task
//     ensure, task rollup, landing) waits for the background to finish first.
//     It reads what the background made, and two finds-or-creates of one task
//     at once would make it twice;
//   - a background hook that fails stops the run at the next hook, with its
//     own error. It is reported as it fails, as a foreground one is;
//   - the run never ends with the background still running: every way out of
//     the run waits for it, and a success reports its failure.
//
// Nothing else changes. The background has its own output scope (a phase never
// reads preInstantiate's outputs — resetOutputs), its own step reports under
// `phase: ""`, and the same action runner.

// background is the run's background hooks, running.
type background struct {
	done chan struct{}
	// err and ran are written once, before done is closed.
	err error
	ran []string
}

// splitBackground is preInstantiate's foreground hooks, then its background
// ones. Validation has already held the background to a suffix.
func splitBackground(hooks []Hook) (fg, bg []Hook) {
	for i, h := range hooks {
		if h.Background {
			return hooks[:i], hooks[i:]
		}
	}
	return hooks, nil
}

// startBackground runs hooks on their own goroutine. failed is told the
// moment one fails, so the reader hears it then, not at the next join.
func startBackground(ctx context.Context, hr *hookRunner, hooks []Hook, failed func(error)) *background {
	b := &background{done: make(chan struct{})}
	go func() {
		defer close(b.done)
		b.ran, b.err = hr.run(ctx, hooks)
		if b.err != nil && failed != nil {
			failed(b.err)
		}
	}()
	return b
}

// wait blocks until the background has finished and returns its failure.
func (b *background) wait(ctx context.Context) error {
	if b == nil {
		return nil
	}
	select {
	case <-b.done:
		return b.err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// failure is the background's failure if it has already failed; it never
// blocks.
func (b *background) failure() error {
	if b == nil {
		return nil
	}
	select {
	case <-b.done:
		return b.err
	default:
		return nil
	}
}

// settle waits for the background however the run is ending, so nothing it
// does outlives the run. Its error is the run's to report, not this.
func (b *background) settle() {
	if b != nil {
		<-b.done
	}
}

// joinFor is what a hook owes the background before it runs: a hook on the
// task plane waits for it; any other hook only stops if it has failed.
func (hr *hookRunner) joinFor(ctx context.Context, h Hook) error {
	if hr.bg == nil {
		return nil
	}
	if h.IsAction() {
		if spec, ok := actions.Lookup(h.Uses); ok && spec.TaskPlane {
			return hr.bg.wait(ctx)
		}
	}
	return hr.bg.failure()
}

// validateBackground holds `background:` to the one place it means something:
// a run-level preInstantiate hook that calls an action, followed only by
// others like it. An argv hook in the background would share the product tree
// with the phase writing it; a foreground hook after a background one would
// have no order to run in.
func (bp *Blueprint) validateBackground() error {
	seen := ""
	for i, h := range bp.Hooks.PreInstantiate {
		switch {
		case h.Background && !h.IsAction():
			return fmt.Errorf("hooks.preInstantiate[%d] (%s): background is for `uses:` hooks — a `run:` hook would share the product tree with the phase writing it", i, h.ID)
		case h.Background:
			seen = h.ID
		case seen != "":
			return fmt.Errorf("hooks.preInstantiate[%d] (%s): follows background hook %q, so it must be `background: true` too — the background is the end of the list, run in order", i, h.ID, seen)
		}
	}
	var misplaced []string
	for i, h := range bp.Hooks.PostInstantiate {
		if h.Background {
			misplaced = append(misplaced, fmt.Sprintf("hooks.postInstantiate[%d] (%s)", i, h.ID))
		}
	}
	for _, ph := range bp.Phases {
		for _, h := range ph.Hooks.All() {
			if h.Background {
				misplaced = append(misplaced, fmt.Sprintf("phase %s hook %s", ph.Name, h.ID))
			}
		}
	}
	if len(misplaced) > 0 {
		return fmt.Errorf("%s: background is only for hooks.preInstantiate — the run's set-up, which a phase's task-plane hooks wait for", strings.Join(misplaced, ", "))
	}
	return nil
}
