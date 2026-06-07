package main

// changed_engine.go routes `plan`/`run --changed` through the unified
// change-detection engine (internal/affected) over the object-model catalog.
//
// Per the catalog-state design, the object catalog is always the FULL catalog;
// selecting the --changed subset is a plan/run-time duty. This helper refreshes
// the catalog if the source changed (cheap memo hit otherwise), loads the full
// catalog, runs the engine, and returns the selected component *names* — the
// same Selection (DirectlyChanged ∪ include:always closure) the parity gate
// proves equals the legacy collectChangedComponents → ResolveComponentSet.
//
// It is best-effort at the call site: a failure here (no object store, non-git
// workspace, resolve error) lets the caller fall back to the legacy selector.

import (
	"context"
	"time"

	"github.com/sourceplane/orun/internal/affected"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/git"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/objplan"
	"github.com/sourceplane/orun/internal/sourcectx"
)

// engineChangedSelection refreshes-if-needed and returns the --changed selection
// as a set of component names, computed by the engine over the full object-model
// catalog. changeOptions carries the git base/head (or an explicit --files set).
func engineChangedSelection(ctx context.Context, changeOptions git.ChangeOptions) (map[string]bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	workspaceRoot, err := catalogWorkspaceRoot()
	if err != nil {
		return nil, err
	}
	ws, err := sourcectx.ResolveSourceSnapshot(ctx, sourcectx.ResolveOptions{WorkspacePath: workspaceRoot})
	if err != nil {
		return nil, err
	}

	createdAt := time.Now().UTC().Format(time.RFC3339)
	srcKey := sourcectx.BuildSourceSnapshotKey(ws)
	inputHash := buildCatalogInputHash(ws)
	shortRepo := shortRepoName(ws.Repo, workspaceRoot)
	inputs := resolverInputsFromState(ws, srcKey, inputHash, repoForInputs(ws.Repo, workspaceRoot), createdAt)

	store, refs, root, err := openObjectModel()
	if err != nil {
		return nil, err
	}
	w := nodewriter.New(store, refs)
	memo := objplan.NewResolveMemo(root)

	// Refresh-if-needed: RefreshCatalog only re-resolves on a memo miss (source
	// changed); otherwise it just moves catalogs/current. So a clean re-run is
	// cheap, while a changed source repopulates the full catalog before we read.
	resolve := func() (*catalogresolve.CatalogView, error) {
		view, _, berr := catalogresolve.BuildCatalog(ctx, catalogresolve.Options{WorkspaceRoot: workspaceRoot, Repo: shortRepo}, inputs)
		return view, berr
	}
	if _, err := objplan.RefreshCatalog(ctx, w, store, memo, objplan.Input{
		Workspace:      ws,
		SourceHumanKey: srcKey,
		Resolve:        resolve,
	}, objplan.Options{}); err != nil {
		return nil, err
	}

	view, err := objcatalog.New(store, refs).Load(ctx, "catalogs/current")
	if err != nil {
		return nil, err
	}
	res, err := affected.NewDetector(&view, affected.IntentImpact(intentImpact)).
		Detect(ctx, affected.GitChangeSource{Options: changeOptions, IntentPath: "intent.yaml"})
	if err != nil {
		return nil, err
	}

	// Map the engine's component keys back to the names the instance filter uses.
	keyToName := make(map[string]string, len(view.Components))
	for _, c := range view.Components {
		keyToName[c.ComponentKey] = c.Name
	}
	out := make(map[string]bool, len(res.Selection))
	for _, k := range res.Selection {
		if name := keyToName[k]; name != "" {
			out[name] = true
		}
	}
	return out, nil
}
