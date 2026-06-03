package main

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objplan"
	"github.com/sourceplane/orun/internal/runner"
	"github.com/sourceplane/orun/internal/runworktree"
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
//
// The run path NEVER re-resolves the catalog (that is `orun plan`'s job, which
// already published the revision under revisions/by-hash/<planHash>). This keeps
// every run cheap — the common case is a single ref read — which is the
// prerequisite for making the object model the default.
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

	revID, ok := resolveRunRevision(ctx, store, refs, root, plan)
	if !ok {
		return nil
	}

	mgr := runworktree.NewManager(store, refs, root)
	// Seal any working trees orphaned by a prior crash before opening ours.
	if _, rerr := mgr.RecoverStale(ctx); rerr != nil {
		warnObjectModel("recover stale runs: %v", rerr)
	}

	wt, err := mgr.Open(ctx, runworktree.OpenInput{
		ExecutionID: execID,
		RevisionID:  revID,
	})
	if err != nil {
		warnObjectModel("open working tree: %v", err)
		return nil
	}
	return &objectRunSession{mgr: mgr, wt: wt, revID: revID}
}

// resolveRunRevision returns the revision the run attaches to. It first looks up
// the revision `orun plan` already published for this plan
// (revisions/by-hash/<planHash>) — a cheap ref read that dedups perfectly. On a
// miss (a run with no prior object-model plan) it materializes a catalog-free
// degenerate revision (plan only) cheaply, never resolving the catalog.
func resolveRunRevision(ctx context.Context, store *objectstore.LocalStore, refs *refstore.LocalRefStore, root string, plan *model.Plan) (objectstore.ObjectID, bool) {
	planHash, herr := computePlanHashForRevision(plan)
	if herr == nil && planHash != "" {
		if r, rerr := refs.Read(ctx, revByHashPrefix+sanitizeRevSeg(planHash)); rerr == nil {
			return objectstore.ObjectID(r.Target), true
		}
	}

	planBytes, err := canonicalPlanJSON(plan)
	if err != nil {
		warnObjectModel("marshal plan: %v", err)
		return "", false
	}
	w := nodewriter.New(store, refs)
	res, err := objplan.Plan(ctx, w, store, objplan.NewResolveMemo(root), objplan.Input{
		PlanBytes:      planBytes,
		RevisionScope:  nodes.RevisionScope{Mode: "full"},
		JobCount:       len(plan.Jobs),
		LegacyChecksum: planHash,
		Trigger: nodes.TriggerOccurrence{
			Kind:        nodes.KindTriggerOccurrence,
			TriggerName: "system.run",
			Source:      nodes.TriggerSource{Flavor: "system", System: "run"},
			Scope:       nodes.RevisionScope{Mode: "full"},
			Actor:       "runner",
		},
	}, objplan.Options{NoCatalog: true})
	if err != nil {
		warnObjectModel("resolve revision: %v", err)
		return "", false
	}
	return res.RevisionID, true
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
