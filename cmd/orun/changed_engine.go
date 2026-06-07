package main

// changed_engine.go routes `plan`/`run --changed` through the unified
// change-detection engine (internal/affected) over the object-model catalog.
//
// Per the catalog-state design, the object catalog is always the FULL catalog;
// selecting the --changed subset is a plan/run-time duty. This helper refreshes
// the catalog if the source changed (cheap memo hit otherwise), loads the full
// catalog, runs the engine, and returns the selected component *names* — the
// same Selection (DirectlyChanged ∪ include:always closure) the golden parity
// gate (changed_parity_test.go) locks.
//
// This is the single --changed selection path: the legacy file-walking selector
// was retired in CS5 once the CS8 parity + determinism gate went green, so an
// error here surfaces to the caller rather than silently diverging.

import (
	"context"

	"github.com/sourceplane/orun/internal/affected"
	"github.com/sourceplane/orun/internal/git"
	"github.com/sourceplane/orun/internal/objcatalog"
)

// engineChangedSelection refreshes-if-needed and returns the --changed selection
// as a set of component names, computed by the engine over the full object-model
// catalog. changeOptions carries the git base/head (or an explicit --files set).
func engineChangedSelection(ctx context.Context, changeOptions git.ChangeOptions) (map[string]bool, error) {
	if ctx == nil {
		ctx = context.Background()
	}

	// Refresh-if-needed via the shared seam: RefreshCatalog only re-resolves on
	// a memo miss (source changed); otherwise it just moves catalogs/current. So
	// a clean re-run is cheap, while a changed source repopulates the full
	// catalog before we read.
	rc, err := refreshObjectCatalog(ctx)
	if err != nil {
		return nil, err
	}

	view, err := objcatalog.New(rc.store, rc.refs).Load(ctx, "catalogs/current")
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
