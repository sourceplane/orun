// Package catalogread is the cockpit's catalog data provider: it loads the
// object-model catalog, runs the change-detection engine for the live
// changed/affected overlay, and returns the presentation-neutral view-models the
// cockpit renders (specs/orun-catalog-state/consumers.md §2).
//
// It is the read seam — it composes internal/objcatalog (the catalog read view),
// internal/affected (the engine), and internal/cockpit/viewmodel (the
// render-ready structs). It performs no rendering and holds no action seam.
package catalogread

import (
	"context"

	"github.com/sourceplane/orun/internal/affected"
	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
	"github.com/sourceplane/orun/internal/objcatalog"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// catalogCurrentRef is the ref the cockpit reads (the workspace's current
// catalog). Kept local, mirroring the objread/objcatalog convention.
const catalogCurrentRef = "catalogs/current"

// Reader provides the cockpit's catalog views over one object/ref store pair.
type Reader struct {
	cat           *objcatalog.Reader
	workspaceRoot string
	policy        affected.IntentImpact
}

// New constructs a Reader. workspaceRoot is the absolute workspace path (used to
// recompute the content-aware overlay); policy defaults to "watch".
func New(store objectstore.ObjectStore, refs refstore.RefStore, workspaceRoot string) *Reader {
	return &Reader{cat: objcatalog.New(store, refs), workspaceRoot: workspaceRoot, policy: affected.IntentImpactWatch}
}

// WithPolicy sets the intent-impact policy for the overlay.
func (r *Reader) WithPolicy(p affected.IntentImpact) *Reader {
	r.policy = p
	return r
}

// CatalogView loads the current catalog. When withOverlay is set, it also runs
// the content-aware fingerprint source through the engine and annotates each row
// with its changed/affected state (the Q2 overlay). The overlay is best-effort:
// a detection error degrades to the plain catalog view, never failing the read.
func (r *Reader) CatalogView(ctx context.Context, withOverlay bool) (viewmodel.CatalogView, error) {
	cat, err := r.cat.Load(ctx, catalogCurrentRef)
	if err != nil {
		return viewmodel.CatalogView{}, err
	}

	var overlay *affected.Result
	if withOverlay {
		res, derr := affected.NewDetector(&cat, r.policy).
			Detect(ctx, affected.FingerprintChangeSource{Catalog: &cat, WorkspaceRoot: r.workspaceRoot})
		if derr == nil {
			overlay = &res
		}
	}
	return viewmodel.BuildCatalogView(cat, overlay), nil
}

// ComponentView loads one component's detail page by component key. The bool
// reports whether the component exists in the current catalog.
func (r *Reader) ComponentView(ctx context.Context, componentKey string) (viewmodel.ComponentView, bool, error) {
	cat, err := r.cat.Load(ctx, catalogCurrentRef)
	if err != nil {
		return viewmodel.ComponentView{}, false, err
	}
	for _, c := range cat.Components {
		if c.ComponentKey == componentKey {
			return viewmodel.BuildComponentView(c), true, nil
		}
	}
	return viewmodel.ComponentView{}, false, nil
}
