package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/objrun"
	"github.com/sourceplane/orun/internal/runner"
)

// object_model_runner.go wires `orun run` to the shared object-model session
// glue (internal/objrun): when ORUN_OBJECT_RUNNER is enabled the runner writes
// the content-addressed execution natively via a live working tree and seals it
// on terminal. These are thin cmd-side adapters over objrun that add the flag
// gate, the warning surface, and the sealed-run summary line; the actual session
// logic lives in objrun so `orun run` and the TUI run path share one
// implementation.

// beginObjectModelRun opens a live object-model session for a run, honoring the
// ORUN_OBJECT_RUNNER flag. Best-effort: returns nil on any failure (the run
// proceeds unaffected).
func beginObjectModelRun(orunDir string, plan *model.Plan, execID string) *objrun.Session {
	if !objectRunnerEnabled() || plan == nil || execID == "" {
		return nil
	}
	sess, err := objrun.Begin(context.Background(), objectModelRoot(orunDir), plan, execID)
	if err != nil {
		warnObjectModel("%v", err)
		return nil
	}
	return sess
}

// installObjectRunnerHooks chains the session's live working-tree writes onto the
// runner's lifecycle hooks. Nil-safe.
func installObjectRunnerHooks(r *runner.Runner, s *objrun.Session) {
	s.InstallHooks(r)
}

// finishObjectModelRun seals the working tree at the run's terminal status and
// prints the sealed-run summary. Nil-safe.
func finishObjectModelRun(r *runner.Runner, s *objrun.Session, runErr error) {
	if s == nil {
		return
	}
	id, err := s.Finish(context.Background(), r, runErr)
	if err != nil {
		warnObjectModel("seal execution: %v", err)
		return
	}
	fmt.Fprintf(os.Stderr, "object-runner: revision=%s execution=%s sealed (live)\n", shortID(s.RevisionID()), shortID(id))
}
