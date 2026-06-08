package main

// catalog_refresh_objectmodel.go is the object-model half of `orun catalog
// refresh` (specs/orun-catalog-state/cli-surface.md §2). After the catalogstore
// snapshot is written, the refresh ALSO writes the object-model catalog — source
// + catalog + catalogs/current move + impact/ownership.json — so an explicit
// refresh (not just `orun plan`) populates the cockpit's source and the
// change-detection impact index.
//
// It reuses the SAME resolved CatalogView (no second resolve) and is
// best-effort: any failure is returned for the caller to surface as a warning,
// never a non-zero exit (mirrors object_model_plan.go's posture).

import (
	"context"

	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objplan"
	"github.com/sourceplane/orun/internal/sourcectx"
)

// objectModelRefresh is the optional data.objectModel projection for the
// CatalogRefreshResult envelope.
type objectModelRefresh struct {
	CatalogID  string `json:"catalogId"`
	SourceID   string `json:"sourceId"`
	Components int    `json:"components"`
	// Reused reports an idempotent refresh: catalogs/current already pointed at
	// the same content-addressed catalog id (the workspace did not change). Not
	// serialized — the envelope's top-level created/reused fields carry it.
	Reused bool `json:"-"`
}

// writeObjectModelRefresh writes the object-model catalog for an explicit
// refresh, reusing the already-resolved view. ws is the resolved workspace VCS
// state; srcHumanKey is the catalogstore source key (for the source node's human
// key). Returns the written ids (best-effort) and any error for the caller to
// fold into warnings[].
func writeObjectModelRefresh(ctx context.Context, view *catalogresolve.CatalogView, ws sourcectx.WorkspaceState, srcHumanKey string) (objectModelRefresh, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	store, refs, root, err := openObjectModel()
	if err != nil {
		return objectModelRefresh{}, err
	}
	// Capture the prior catalog target so an unchanged workspace (same
	// content-addressed catalog id) is reported as an idempotent reuse.
	var priorCatalog string
	if r, rerr := refs.Read(ctx, "catalogs/current"); rerr == nil {
		priorCatalog = r.Target
	}

	w := nodewriter.New(store, refs)
	memo := objplan.NewResolveMemo(root)

	res, err := objplan.RefreshCatalog(ctx, w, store, memo, objplan.Input{
		Workspace:      ws,
		SourceHumanKey: srcHumanKey,
		Resolve:        func() (*catalogresolve.CatalogView, error) { return view, nil },
	}, objplan.Options{Strict: catalogStrictFlag})
	if err != nil {
		return objectModelRefresh{}, err
	}

	out := objectModelRefresh{
		CatalogID: string(res.CatalogID),
		SourceID:  string(res.SourceID),
		Reused:    priorCatalog != "" && priorCatalog == string(res.CatalogID),
	}
	if view != nil {
		out.Components = len(view.Manifests)
	}
	return out, nil
}
