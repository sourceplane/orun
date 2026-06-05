package objplan

import (
	"context"
	"fmt"

	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/sourcectx"
)

// Options tune the tolerant-strict walk.
type Options struct {
	// Strict promotes a catalog resolution error from a silent skip to a hard
	// failure (the rest of the walk still treats validation *issues* leniently —
	// that policy lives in the caller).
	Strict bool
	// NoCatalog skips catalog resolution entirely; the revision is written with
	// no catalogId edge (degenerate / emergency planning).
	NoCatalog bool
	// ResolverVersion keys the resolve memo; defaults to 1 when zero.
	ResolverVersion int
}

// Input carries the pre-computed plan inputs plus a lazy catalog resolver. The
// resolver is only invoked on a memo miss, which is what makes the strict walk
// cheap on an unchanged source.
type Input struct {
	Workspace        sourcectx.WorkspaceState
	SourceHumanKey   string
	Resolve          func() (*catalogresolve.CatalogView, error)
	PlanBytes        []byte
	RevisionHumanKey string
	RevisionScope    nodes.RevisionScope
	JobCount         int
	LegacyChecksum   string
	Trigger          nodes.TriggerOccurrence
}

// Plan runs source → (catalog, memoized) → revision → trigger and returns the
// resulting ids. Catalog resolution is memoized by (sourceId, resolverVersion):
// on a hit whose object is still present, the cached catalog id is reused (no
// resolve, no re-write) and only its refs are refreshed.
func Plan(ctx context.Context, w *nodewriter.Writer, store objectstore.ObjectStore, memo *ResolveMemo, in Input, opts Options) (nodewriter.PlanResult, error) {
	var res nodewriter.PlanResult
	rv := opts.ResolverVersion
	if rv == 0 {
		rv = 1
	}

	src := BuildSourceNode(in.Workspace, in.SourceHumanKey)
	srcID, err := w.WriteSource(ctx, src, SourceRefs(src)...)
	if err != nil {
		return res, fmt.Errorf("objplan: write source: %w", err)
	}
	res.SourceID = srcID

	var catID objectstore.ObjectID
	if !opts.NoCatalog && in.Resolve != nil {
		catID, err = writeCatalogMemoized(ctx, w, store, memo, src, srcID, rv, in.Resolve, opts.Strict)
		if err != nil {
			return res, err
		}
		res.CatalogID = catID
	}

	rev := nodes.PlanRevision{
		Kind:           nodes.KindPlanRevision,
		HumanKey:       in.RevisionHumanKey,
		SourceID:       string(srcID),
		CatalogID:      string(catID),
		Scope:          in.RevisionScope,
		JobCount:       in.JobCount,
		LegacyChecksum: in.LegacyChecksum,
	}
	if rev.Scope.Mode == "" {
		rev.Scope.Mode = "full"
	}
	revID, reused, err := w.WriteRevision(ctx, rev, in.PlanBytes, RevisionRefs(in.LegacyChecksum)...)
	if err != nil {
		return res, fmt.Errorf("objplan: write revision: %w", err)
	}
	res.RevisionID = revID
	res.RevisionReused = reused

	trgID, err := w.RecordTrigger(ctx, in.Trigger, revID, TriggerRefs(in.Trigger.TriggerName)...)
	if err != nil {
		return res, fmt.Errorf("objplan: record trigger: %w", err)
	}
	res.TriggerID = trgID
	return res, nil
}

// writeCatalogMemoized resolves+writes the catalog, or reuses a memoized one.
// On a memo hit whose object is still present, the cached catalog id is reused
// (no resolve, no re-write) and its refs are refreshed. On a miss — or a stale
// cache entry whose object is gone — the resolver runs, the catalog is written,
// and the memo is updated. A resolve error is fatal under strict, else a
// silently-skipped catalog (returns "", nil).
func writeCatalogMemoized(
	ctx context.Context,
	w *nodewriter.Writer,
	store objectstore.ObjectStore,
	memo *ResolveMemo,
	src nodes.SourceSnapshot,
	srcID objectstore.ObjectID,
	resolverVersion int,
	resolve func() (*catalogresolve.CatalogView, error),
	strict bool,
) (objectstore.ObjectID, error) {
	catalogRefs := CatalogRefs(src)

	if memo != nil {
		if cached, ok := memo.Get(srcID, resolverVersion); ok {
			if has, err := store.Has(ctx, cached); err == nil && has {
				if err := w.MoveRefs(ctx, catalogRefs, cached); err != nil {
					return "", fmt.Errorf("objplan: refresh catalog refs: %w", err)
				}
				return cached, nil
			}
		}
	}

	view, err := resolve()
	if err != nil {
		if strict {
			return "", fmt.Errorf("objplan: resolve catalog: %w", err)
		}
		return "", nil // tolerant: skip the catalog edge
	}
	cat, manifests, graphs, ownership := BuildCatalogNodes(view, resolverVersion)
	cat.SourceID = string(srcID)
	catID, err := w.WriteCatalog(ctx, cat, manifests, graphs, ownership, catalogRefs...)
	if err != nil {
		return "", fmt.Errorf("objplan: write catalog: %w", err)
	}
	if memo != nil {
		_ = memo.Put(srcID, resolverVersion, catID) // derived cache; best-effort
	}
	return catID, nil
}
