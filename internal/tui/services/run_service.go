package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"time"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/objrun"
	"github.com/sourceplane/orun/internal/runner"
)

// validateRunRequest is the canonical scope guard for RunPlan. It exposes
// the validation logic so tests can assert fail-closed behavior without
// spinning up a runner.
//
// Supported scope:
//   - req.Plan != nil
//   - req.DryRun in {true, false} — dry-run preview and real local execution
//   - req.RemoteState == false
//
// Real execution writes per-step logs and persists state to the local
// store, which is what powers live log streaming in the cockpit. Remote
// state remains gated until its own phase; anything out of scope returns a
// sentinel error so callers surface it instead of silently executing.
func validateRunRequest(req RunRequest) error {
	if req.Plan == nil {
		return errors.New("RunPlan: req.Plan is required")
	}
	if req.RemoteState {
		return errors.New("RunPlan: RemoteState=true is not supported from the TUI yet")
	}
	return nil
}

// RunPlan executes a compiled plan as a local dry-run and streams
// RunEvents back through the returned channel. The channel is closed
// after the run terminates (success, failure, or ctx cancellation) and a
// single RunEventRunDone sentinel is emitted before close so callers
// have an unambiguous end-of-stream marker.
//
// Implementation notes:
//   - Constructs internal/runner.Runner directly (no exec.Command).
//   - Wires RunnerHooks{BeforeJob, AfterJobTerminal} to translate runner
//     callbacks into RunEvent values. We use BeforeJob purely as a
//     job-started signal (always returns skip=false, nil).
//   - Runner stdout/stderr are discarded — the TUI surfaces progress via
//     events, not the runner's text dashboard.
//   - Sends are select-guarded on ctx.Done() to avoid deadlocking the
//     runner if the UI stops draining the channel. The channel is
//     buffered (64) to absorb bursty job activity.
//   - Runner.Run is executed synchronously on a goroutine so the public
//     surface stays non-blocking.
func (s *LiveOrunService) RunPlan(ctx context.Context, req RunRequest) (<-chan RunEvent, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := validateRunRequest(req); err != nil {
		return nil, err
	}

	// Resolve workdir: explicit > IntentRoot > "."
	workDir := req.WorkDir
	useOverride := workDir != ""
	if workDir == "" {
		if s.cfg.IntentRoot != "" {
			workDir = s.cfg.IntentRoot
		} else {
			workDir = "."
		}
	}

	// ExecID: explicit > derived from plan name
	execID := req.ExecID
	if execID == "" {
		name := req.Plan.Metadata.Name
		if name == "" {
			name = "tui-dryrun"
		}
		execID = execmodel.GenerateExecID(name)
	}

	concurrency := req.Concurrency
	if concurrency <= 0 {
		concurrency = req.Plan.Execution.Concurrency
	}
	if concurrency <= 0 {
		concurrency = 1
	}

	// Local executor only — dry-run never actually runs commands, so the
	// executor choice mostly governs RuntimeContext.
	localExec, err := executor.Get("local")
	if err != nil {
		return nil, fmt.Errorf("RunPlan: resolve local executor: %w", err)
	}
	runtime := executor.RuntimeContext{Runner: "local", Environment: "local"}

	ch := make(chan RunEvent, 64)

	send := func(ev RunEvent) {
		if ev.Timestamp.IsZero() {
			ev.Timestamp = time.Now()
		}
		// Stamp the resolved execID on every event so the UI can scope live
		// log tailing to this run without a separate lookup.
		if ev.ExecID == "" {
			ev.ExecID = execID
		}
		select {
		case <-ctx.Done():
		case ch <- ev:
		}
	}

	r := runner.NewRunner(
		workDir,
		useOverride,
		io.Discard,
		io.Discard,
		req.DryRun, // real execution when false — persists state + per-step logs
		req.JobID,
		false, // retry
		false, // verbose
		localExec,
		runtime,
		execID,
		concurrency,
		req.Components,
		"", // env filter — not part of RunRequest scope today
	)
	r.PlanID = execmodel.PlanChecksumShort(req.Plan)

	// Index jobs so hooks can attach component/env metadata to events.
	jobIndex := make(map[string]model.PlanJob, len(req.Plan.Jobs))
	for _, j := range req.Plan.Jobs {
		jobIndex[j.ID] = j
	}

	r.Hooks = &runner.RunnerHooks{
		BeforeJob: func(jobID string) (bool, error) {
			job := jobIndex[jobID]
			send(RunEvent{
				Kind:      RunEventJobStarted,
				JobID:     jobID,
				Component: job.Component,
				Env:       job.Environment,
				Status:    "running",
			})
			return false, nil
		},
		AfterJobTerminal: func(jobID string, success bool, errText string) {
			job := jobIndex[jobID]
			ev := RunEvent{
				JobID:     jobID,
				Component: job.Component,
				Env:       job.Environment,
			}
			if success {
				ev.Kind = RunEventJobCompleted
				ev.Status = "completed"
			} else {
				ev.Kind = RunEventJobFailed
				ev.Status = "failed"
				ev.Error = errText
			}
			send(ev)
		},
	}

	// Native object-model run session: a real (non-dry) run opens a live working
	// tree and seals a native ExecutionRun on terminal — the same path `orun run`
	// takes — so the run is then readable via ListRuns / TailLogs. Best-effort:
	// failure to open never blocks the run. Dry runs persist nothing.
	var sess *objrun.Session
	if !req.DryRun && s.cfg.ObjectModelRoot != "" {
		root := filepath.Join(s.cfg.ObjectModelRoot, "objectmodel")
		if opened, oerr := objrun.Begin(context.Background(), root, req.Plan, execID); oerr == nil {
			sess = opened
		}
	}
	sess.InstallHooks(r) // nil-safe

	// If the context cancels before/during the run, abort the runner. We
	// can't cancel the runner mid-step from outside (no Run(ctx) yet), but
	// the send() helper makes hooks non-blocking so the goroutine can drain
	// and exit. We surface ctx.Err() through the run_done event so the UI
	// can tell graceful completion from cancellation.
	go func() {
		defer close(ch)

		// Pre-cancel guard: emit a single done event so the channel never
		// silently produces no rows. Still seal the (empty) session so the live
		// working tree is not orphaned.
		if err := ctx.Err(); err != nil {
			_, _ = sess.Finish(context.Background(), r, err)
			ch <- RunEvent{
				Kind:      RunEventRunDone,
				ExecID:    execID,
				Status:    "cancelled",
				Error:     err.Error(),
				Timestamp: time.Now(),
			}
			return
		}

		runErr := r.Run(req.Plan)

		// Seal the run into the object graph from the runner's terminal state
		// (nil-safe for dry runs / failed-to-open sessions).
		_, _ = sess.Finish(context.Background(), r, runErr)

		done := RunEvent{
			Kind:      RunEventRunDone,
			ExecID:    execID,
			Timestamp: time.Now(),
		}
		switch {
		case ctx.Err() != nil:
			done.Status = "cancelled"
			done.Error = ctx.Err().Error()
		case runErr != nil:
			done.Status = "failed"
			done.Error = runErr.Error()
		default:
			done.Status = "completed"
		}
		// Final send is unguarded by ctx.Done so we always close with a
		// done sentinel — UIs rely on it to stop re-arming.
		select {
		case ch <- done:
		default:
			// Channel full and no reader; drop the sentinel rather than
			// block forever. WaitForRunEvent's close-detect synthesizes a
			// terminal RunEventRunDone so the UI still terminates.
		}
	}()

	return ch, nil
}
