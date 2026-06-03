package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objplan"
	"github.com/sourceplane/orun/internal/runner"
	"github.com/sourceplane/orun/internal/runworktree"
	"github.com/sourceplane/orun/internal/sourcectx"
)

// object_model_runner.go is the M12 T2 wiring: when ORUN_OBJECT_RUNNER is set,
// the runner writes the content-addressed execution *natively* via a live
// working tree (internal/runworktree) and seals it on terminal — rather than the
// M7c post-hoc translation of finished legacy state. It stays strictly additive
// and best-effort (any failure warns, never changes the run's exit code) and
// runs alongside the legacy state.json writes, which remain authoritative for
// the legacy readers until the T3/T4 read-side cutover. The legacy-translation
// seal (sealObjectModelRun) remains for the remote / dry-run fall-through.

// objectRunSession holds the live working tree for one run.
type objectRunSession struct {
	mgr   *runworktree.Manager
	wt    *runworktree.WorkTree
	revID objectstore.ObjectID
}

// beginObjectModelRun resolves the revision the execution attaches to and opens
// a live working tree for it. Best-effort: returns nil on any failure (the run
// proceeds; the legacy path is unaffected).
func beginObjectModelRun(orunDir string, plan *model.Plan, execID string) *objectRunSession {
	if !objectRunnerEnabled() || plan == nil || execID == "" {
		return nil
	}
	ctx := context.Background()
	root := objectModelRoot(orunDir)

	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		warnObjectModel("open object store: %v", err)
		return nil
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Writer: "runner"})
	if err != nil {
		warnObjectModel("open ref store: %v", err)
		return nil
	}
	w := nodewriter.New(store, refs)

	planBytes, err := canonicalPlanJSON(plan)
	if err != nil {
		warnObjectModel("marshal plan: %v", err)
		return nil
	}

	// Resolve source + catalog so the revision id matches the plan's (the
	// revision tree excludes the trigger, so this dedups to the revision a prior
	// `orun plan` wrote for the same plan+catalog).
	catRes, _ := resolvePlanCatalog(ctx, planCatalogOptions{})
	var ws sourcectx.WorkspaceState
	if wsRoot, werr := catalogWorkspaceRoot(); werr == nil {
		if resolved, rerr := sourcectx.ResolveSourceSnapshot(ctx, sourcectx.ResolveOptions{WorkspacePath: wsRoot}); rerr == nil {
			ws = resolved
		}
	}
	var resolve func() (*catalogresolve.CatalogView, error)
	if catRes.View != nil {
		view := catRes.View
		resolve = func() (*catalogresolve.CatalogView, error) { return view, nil }
	}

	res, err := objplan.Plan(ctx, w, store, objplan.NewResolveMemo(root), objplan.Input{
		Workspace:      ws,
		SourceHumanKey: sourceHumanKey(catRes),
		Resolve:        resolve,
		PlanBytes:      planBytes,
		RevisionScope:  nodes.RevisionScope{Mode: "full"},
		JobCount:       len(plan.Jobs),
		Trigger: nodes.TriggerOccurrence{
			Kind:        nodes.KindTriggerOccurrence,
			TriggerName: "system.run",
			Source:      nodes.TriggerSource{Flavor: "system", System: "run"},
			Scope:       nodes.RevisionScope{Mode: "full"},
			Actor:       "runner",
		},
	}, objplan.Options{NoCatalog: resolve == nil})
	if err != nil {
		warnObjectModel("resolve revision: %v", err)
		return nil
	}

	mgr := runworktree.NewManager(store, refs, root)
	// Seal any working trees orphaned by a prior crash before opening ours.
	if _, rerr := mgr.RecoverStale(ctx); rerr != nil {
		warnObjectModel("recover stale runs: %v", rerr)
	}

	wt, err := mgr.Open(ctx, runworktree.OpenInput{
		ExecutionID: execID,
		RevisionID:  res.RevisionID,
	})
	if err != nil {
		warnObjectModel("open working tree: %v", err)
		return nil
	}
	return &objectRunSession{mgr: mgr, wt: wt, revID: res.RevisionID}
}

// installObjectRunnerHooks chains the live working-tree writes onto the runner's
// lifecycle hooks: each state tick projects the runner's authoritative job/step
// state into the working tree (and bumps the heartbeat), and each step log is
// streamed as a content blob. The chain preserves any previously-installed
// callbacks (local/remote/revision hooks).
func installObjectRunnerHooks(r *runner.Runner, s *objectRunSession) {
	if s == nil {
		return
	}
	if r.Hooks == nil {
		r.Hooks = &runner.RunnerHooks{}
	}
	prev := r.Hooks.AfterStateUpdate
	r.Hooks.AfterStateUpdate = func() {
		if prev != nil {
			prev()
		}
		// Project the runner's authoritative in-memory state directly — the
		// object-model path does not read the legacy on-disk state.json.
		if st := r.SnapshotState(); st != nil {
			_ = s.wt.Project(projectFromExecState(st))
		}
	}
	prevLog := r.Hooks.AfterStepLog
	r.Hooks.AfterStepLog = func(jobID, stepID, output string) {
		if prevLog != nil {
			prevLog(jobID, stepID, output)
		}
		if output != "" {
			_ = s.wt.SetStepLog(jobID, stepID, []byte(output))
		}
	}
}

// finishObjectModelRun applies a final projection (catching the post-shutdown
// state) and seals the working tree at the run's terminal status. It reads the
// runner's in-memory state, not the legacy store.
func finishObjectModelRun(r *runner.Runner, s *objectRunSession, runErr error) {
	if s == nil {
		return
	}
	ctx := context.Background()
	st := r.SnapshotState()
	if st != nil {
		_ = s.wt.Project(projectFromExecState(st))
	}

	status := nodes.StatusSucceeded
	switch {
	case runErr != nil:
		status = nodes.StatusFailed
	case st != nil:
		if execmodel.SummarizeExecutionState(st).Failed > 0 {
			status = nodes.StatusFailed
		}
	}

	id, err := s.wt.Seal(ctx, status, time.Time{})
	if err != nil {
		warnObjectModel("seal execution: %v", err)
		return
	}
	fmt.Fprintf(os.Stderr, "object-runner: revision=%s execution=%s sealed (live)\n", shortID(s.revID), shortID(id))
}

// projectFromExecState maps the legacy runner state into the working-tree
// projection input, in deterministic (sorted) order so the sealed tree is
// reproducible.
func projectFromExecState(st *execmodel.ExecState) []runworktree.ProjectedJob {
	if st == nil || len(st.Jobs) == 0 {
		return nil
	}
	jobIDs := make([]string, 0, len(st.Jobs))
	for id := range st.Jobs {
		jobIDs = append(jobIDs, id)
	}
	sort.Strings(jobIDs)

	out := make([]runworktree.ProjectedJob, 0, len(jobIDs))
	for _, jid := range jobIDs {
		js := st.Jobs[jid]
		pj := runworktree.ProjectedJob{
			JobID:     jid,
			Status:    runnerStatusToNode(js.Status),
			LastError: js.LastError,
		}
		stepIDs := make([]string, 0, len(js.Steps))
		for sid := range js.Steps {
			stepIDs = append(stepIDs, sid)
		}
		sort.Strings(stepIDs)
		for _, sid := range stepIDs {
			pj.Steps = append(pj.Steps, runworktree.ProjectedStep{
				StepID: sid,
				Status: runnerStatusToNode(js.Steps[sid]),
			})
		}
		out = append(out, pj)
	}
	return out
}

// runnerStatusToNode folds the runner's status vocabulary onto the node status
// set (mirrors internal/objexec.mapStatus, kept independent so the legacy bridge
// can be deleted without touching the live path).
func runnerStatusToNode(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "success", "succeeded", "completed", "complete", "ok", "passed":
		return nodes.StatusSucceeded
	case "failed", "failure", "error", "errored":
		return nodes.StatusFailed
	case "cancelled", "canceled", "skipped":
		return nodes.StatusCancelled
	case "running", "in_progress", "in-progress", "started":
		return nodes.StatusRunning
	default:
		return nodes.StatusPending
	}
}
