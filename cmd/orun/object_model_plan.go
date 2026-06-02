package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objplan"
	"github.com/sourceplane/orun/internal/sourcectx"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// object_model_plan.go is the M5b hook that additionally writes the
// content-addressed object graph (specs/orun-object-model) on `orun plan` when
// ORUN_OBJECT_MODEL is set. It is strictly additive and best-effort: with the
// flag unset the legacy plan path is byte-identical, and any failure with the
// flag set is reported as a warning rather than failing the command.
//
// During this flag-gated coexistence the object graph lives under an isolated
// root (<.orun>/objectmodel/) so its refs/ tree never collides with the legacy
// .orun/refs tree. The M12 cutover relocates it to the .orun/ root and makes it
// canonical.

// objectModelEnabled reports whether the experimental object-model write is on.
func objectModelEnabled() bool { return os.Getenv("ORUN_OBJECT_MODEL") != "" }

// objectModelRoot returns the isolated object-graph root under the .orun dir.
func objectModelRoot(orunDir string) string { return filepath.Join(orunDir, "objectmodel") }

// writeObjectModelPlan writes source → (catalog) → revision → trigger to the
// object graph for the just-compiled plan. orunDir is the absolute path to the
// workspace's .orun directory. catRes carries the already-resolved catalog view
// so no re-resolution happens.
func writeObjectModelPlan(orunDir string, plan *model.Plan, planBytes []byte, planHash, revHumanKey string, trig triggerctx.TriggerOccurrence, catRes planCatalogResolution) {
	if !objectModelEnabled() {
		return
	}
	ctx := context.Background()
	root := objectModelRoot(orunDir)

	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		warnObjectModel("open object store: %v", err)
		return
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Writer: "cli"})
	if err != nil {
		warnObjectModel("open ref store: %v", err)
		return
	}
	w := nodewriter.New(store, refs)
	memo := objplan.NewResolveMemo(root)

	// Resolve the workspace VCS state independently (cheap git probes); a
	// failure degrades to a local-nogit source rather than aborting.
	var ws sourcectx.WorkspaceState
	if wsRoot, werr := catalogWorkspaceRoot(); werr == nil {
		if resolved, rerr := sourcectx.ResolveSourceSnapshot(ctx, sourcectx.ResolveOptions{WorkspacePath: wsRoot}); rerr == nil {
			ws = resolved
		}
	}

	// Reuse the already-resolved catalog view when present; otherwise plan with
	// no catalog edge (degenerate).
	var resolve func() (*catalogresolve.CatalogView, error)
	if catRes.View != nil {
		view := catRes.View
		resolve = func() (*catalogresolve.CatalogView, error) { return view, nil }
	}

	in := objplan.Input{
		Workspace:        ws,
		SourceHumanKey:   sourceHumanKey(catRes),
		Resolve:          resolve,
		PlanBytes:        planBytes,
		RevisionHumanKey: revHumanKey,
		RevisionScope:    nodes.RevisionScope{Mode: planScopeMode(trig)},
		JobCount:         len(plan.Jobs),
		LegacyChecksum:   planHash,
		Trigger:          objectModelTrigger(trig),
	}

	res, err := objplan.Plan(ctx, w, store, memo, in, objplan.Options{
		NoCatalog: resolve == nil,
		Strict:    planCatalogStrict,
	})
	if err != nil {
		warnObjectModel("%v", err)
		return
	}
	fmt.Fprintf(os.Stderr, "object-model: source=%s catalog=%s revision=%s reused=%v trigger=%s\n",
		shortID(res.SourceID), shortID(res.CatalogID), shortID(res.RevisionID), res.RevisionReused, shortID(res.TriggerID))
}

// objectModelTrigger maps a triggerctx.TriggerOccurrence to the node form.
func objectModelTrigger(trig triggerctx.TriggerOccurrence) nodes.TriggerOccurrence {
	name := trig.TriggerName
	if name == "" {
		name = "system.manual"
	}
	flavor := trig.TriggerType
	if flavor == "" {
		flavor = "system"
	}
	return nodes.TriggerOccurrence{
		Kind:        nodes.KindTriggerOccurrence,
		TriggerName: name,
		TriggerKey:  trig.TriggerKey,
		Source:      nodes.TriggerSource{Flavor: flavor, System: trig.Mode},
		Scope:       nodes.RevisionScope{Mode: planScopeMode(trig)},
		Actor:       "cli",
		CreatedAt:   trig.CreatedAt,
	}
}

func planScopeMode(trig triggerctx.TriggerOccurrence) string {
	if trig.PlanScope.Mode != "" {
		return trig.PlanScope.Mode
	}
	return "full"
}

func sourceHumanKey(catRes planCatalogResolution) string {
	if catRes.Source != nil {
		return catRes.Source.SourceSnapshotKey
	}
	return ""
}

func shortID(id objectstore.ObjectID) string {
	s := string(id)
	if s == "" {
		return "-"
	}
	if len(s) > 14 {
		return s[:14] + "…"
	}
	return s
}

func warnObjectModel(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "warning: object-model: "+format+"\n", args...)
}
