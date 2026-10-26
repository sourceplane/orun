package objplan

import (
	"context"
	"encoding/json"

	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
)

// EntityRefCollisions reports every catalog ref that entities of different
// kinds resolve to in the catalog this view builds — exactly what
// nodes.AssembleCatalog would record and act on (the Component keeps the
// ref). It builds the catalog nodes in memory and writes nothing, so a lint
// pass (`orun catalog validate`) sees the same collisions a plan does,
// composition-derived entities included. Nil when the view carries no
// resolved catalog.
func EntityRefCollisions(view *catalogresolve.CatalogView, ownerResolver OwnerResolver, compositionResolver CompositionResolver) []nodes.EntityRefCollision {
	if view == nil || view.ResolvedCatalog == nil {
		return nil
	}
	cat, manifests, _, _, _ := BuildCatalogNodes(view, 1, ownerResolver, compositionResolver)
	return nodes.EntityRefCollisions(manifests, cat)
}

// warnCachedCatalogIssues re-reports the ref collisions a memoized catalog
// recorded when it was written (catalog.json issues), so a re-plan of an
// unchanged tree says what the first plan said. Best-effort: an unreadable
// catalog is silent here — the memo hit is still served.
func warnCachedCatalogIssues(ctx context.Context, store objectstore.ObjectStore, catID objectstore.ObjectID, warn func(string)) {
	entries, err := store.GetTree(ctx, catID)
	if err != nil {
		return
	}
	for _, e := range entries {
		if e.Name != "catalog.json" || e.Kind != objectstore.KindBlob {
			continue
		}
		_, b, err := store.Get(ctx, e.ID)
		if err != nil {
			return
		}
		var rec struct {
			Issues []nodes.CatalogIssue `json:"issues"`
		}
		if json.Unmarshal(b, &rec) != nil {
			return
		}
		for _, is := range rec.Issues {
			if is.Code == nodes.IssueCodeEntityRefCollision {
				warn(is.Message)
			}
		}
		return
	}
}
