package main

// refresh_seam.go is the shared RefreshCatalog seam (Q-1 → a single helper)
// behind the universal refresh hook (§0) and the plan/run --changed engine
// path. It refreshes the object-model catalog from the current workspace and
// repoints catalogs/current, re-resolving only on a source change (the
// RefreshCatalog memo hits otherwise), so a clean re-run is cheap.

import (
	"context"
	"time"

	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objplan"
	"github.com/sourceplane/orun/internal/sourcectx"
)

// refreshedCatalog bundles the freshly-opened stores and the ids written by a
// refresh, so callers can read the just-repointed catalog without re-opening.
type refreshedCatalog struct {
	store     *objectstore.LocalStore
	refs      *refstore.LocalRefStore
	sourceKey string
	result    objplan.RefreshResult
}

// refreshObjectCatalog runs the freshness gate over the current workspace and
// writes the object-model catalog (source + catalog + catalogs/current move +
// impact/ownership.json), reusing objplan.RefreshCatalog's source-keyed memo so
// an unchanged tree is a cheap ref-move and only a changed source re-resolves.
func refreshObjectCatalog(ctx context.Context) (refreshedCatalog, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	workspaceRoot, err := catalogWorkspaceRoot()
	if err != nil {
		return refreshedCatalog{}, err
	}
	ws, err := sourcectx.ResolveSourceSnapshot(ctx, sourcectx.ResolveOptions{WorkspacePath: workspaceRoot})
	if err != nil {
		return refreshedCatalog{}, err
	}

	createdAt := time.Now().UTC().Format(time.RFC3339)
	srcKey := sourcectx.BuildSourceSnapshotKey(ws)
	inputHash := buildCatalogInputHash(ws)
	shortRepo := shortRepoName(ws.Repo, workspaceRoot)
	inputs := resolverInputsFromState(ws, srcKey, inputHash, repoForInputs(ws.Repo, workspaceRoot), createdAt)

	store, refs, root, err := openObjectModel()
	if err != nil {
		return refreshedCatalog{}, err
	}
	w := nodewriter.New(store, refs)
	memo := objplan.NewResolveMemo(root)

	resolve := func() (*catalogresolve.CatalogView, error) {
		view, _, berr := catalogresolve.BuildCatalog(ctx, catalogresolve.Options{WorkspaceRoot: workspaceRoot, Repo: shortRepo}, inputs)
		return view, berr
	}
	res, err := objplan.RefreshCatalog(ctx, w, store, memo, objplan.Input{
		Workspace:      ws,
		SourceHumanKey: srcKey,
		Resolve:        resolve,
	}, objplan.Options{})
	if err != nil {
		return refreshedCatalog{}, err
	}

	return refreshedCatalog{store: store, refs: refs, sourceKey: srcKey, result: res}, nil
}
