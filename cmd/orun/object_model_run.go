package main

import (
	"context"
	"fmt"
	"os"

	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/execseal"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objexec"
	"github.com/sourceplane/orun/internal/objplan"
	"github.com/sourceplane/orun/internal/sourcectx"
	"github.com/sourceplane/orun/internal/state"
)

// object_model_run.go is the M7c hook that seals a finished run into the
// content-addressed object graph when ORUN_OBJECT_RUNNER is set. Like the M5b
// plan hook it is strictly additive and best-effort: with the flag unset the
// legacy run path is byte-identical, and any failure with the flag set is a
// warning, never a non-zero exit.
//
// Attribution: rather than map the legacy revKey to a content id, the hook
// re-runs the objplan walk from the plan it just executed (chosen design). The
// revision tree excludes the trigger, so this dedups to exactly the revision a
// prior `orun plan` wrote for the same plan+catalog; otherwise it materializes
// the correct revision for the plan that actually ran. The finished legacy
// ExecState is then translated by internal/objexec and sealed by
// internal/execseal under that revision.

// objectRunnerEnabled reports whether the experimental object-model run seal is
// turned on.
func objectRunnerEnabled() bool { return flagDefaultOn("ORUN_OBJECT_RUNNER") }

// sealObjectModelRun re-resolves the object-model revision for plan and seals
// the finished legacy execution (execID) under it. orunDir is the absolute path
// to the workspace's .orun directory.
func sealObjectModelRun(orunDir string, plan *model.Plan, legacyStore *state.Store, execID string) {
	if !objectRunnerEnabled() || plan == nil || legacyStore == nil || execID == "" {
		return
	}
	ctx := context.Background()
	root := objectModelRoot(orunDir)

	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		warnObjectModel("open object store: %v", err)
		return
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Writer: "runner"})
	if err != nil {
		warnObjectModel("open ref store: %v", err)
		return
	}
	w := nodewriter.New(store, refs)

	planBytes, err := canonicalPlanJSON(plan)
	if err != nil {
		warnObjectModel("marshal plan: %v", err)
		return
	}

	// Re-resolve source + catalog so the revision id matches the plan's.
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
			TriggerName: "system.replay",
			Source:      nodes.TriggerSource{Flavor: "system", System: "replay"},
			Scope:       nodes.RevisionScope{Mode: "full"},
			Actor:       "runner",
		},
	}, objplan.Options{NoCatalog: resolve == nil})
	if err != nil {
		warnObjectModel("resolve revision: %v", err)
		return
	}

	// Translate the finished legacy execution and seal it natively under the
	// revision.
	st, err := legacyStore.LoadState(execID)
	if err != nil || st == nil {
		warnObjectModel("load execution state %s: %v", execID, err)
		return
	}
	meta, _ := legacyStore.LoadMetadata(execID)

	sealedID, err := execseal.New(w).Seal(ctx, objexec.FromLegacyState(res.RevisionID, "", execID, execID, st, meta))
	if err != nil {
		warnObjectModel("seal execution: %v", err)
		return
	}
	fmt.Fprintf(os.Stderr, "object-runner: revision=%s execution=%s sealed\n", shortID(res.RevisionID), shortID(sealedID))
}
