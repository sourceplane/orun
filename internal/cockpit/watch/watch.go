// Package watch turns a bridge.Source into a stream of RunView updates.
//
// Both the CLI (`orun status --watch`) and the TUI (Phase 3 run pane)
// subscribe here so they see the same refresh cadence and the same
// terminal-state semantics. The implementation polls on a tick — fast
// enough for live cockpits, no fsnotify dependency, identical behaviour
// for local stores and remote backends.
package watch

import (
	"context"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/cockpit/bridge"
	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
)

// Update is one frame on the run-view stream.
type Update struct {
	View viewmodel.RunView
	Err  error
	// Terminal is true on the final emission (status completed/failed or
	// context cancelled). Callers can use it to break out of refresh
	// loops without re-checking the status field.
	Terminal bool
}

// Options tunes the watch loop.
type Options struct {
	// Interval between polls. Floored at 100ms; defaults to 500ms.
	Interval time.Duration
	// ResolveExecID is called every tick so the loop can pick up the
	// latest run when ExecID is empty (e.g. `orun status --watch` with
	// no --exec-id). Optional.
	ResolveExecID func() (string, error)
	// ExecID pins the loop to one execution. Ignored when
	// ResolveExecID is set.
	ExecID string
}

// Run streams RunView updates on the returned channel until ctx is
// cancelled or a terminal status is observed. The channel is closed
// after the final emission.
func Run(ctx context.Context, src bridge.Source, opts Options) <-chan Update {
	if opts.Interval < 100*time.Millisecond {
		opts.Interval = 500 * time.Millisecond
	}
	out := make(chan Update, 1)
	go func() {
		defer close(out)
		t := time.NewTicker(opts.Interval)
		defer t.Stop()

		emit := func() bool {
			execID := opts.ExecID
			if opts.ResolveExecID != nil {
				resolved, err := opts.ResolveExecID()
				if err != nil {
					select {
					case out <- Update{Err: err}:
					case <-ctx.Done():
						return true
					}
					return false
				}
				execID = resolved
			}
			if execID == "" {
				return false
			}
			view, err := bridge.LoadRunView(ctx, src, execID)
			terminal := err == nil && isTerminal(view.Status)
			select {
			case out <- Update{View: view, Err: err, Terminal: terminal}:
			case <-ctx.Done():
				return true
			}
			return terminal
		}

		if emit() {
			return
		}
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if emit() {
					return
				}
			}
		}
	}()
	return out
}

func isTerminal(status string) bool {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "completed", "failed", "succeeded", "success", "error":
		return true
	}
	return false
}
