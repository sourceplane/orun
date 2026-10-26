package objplan

import (
	"context"
	"errors"
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
	// OwnerResolver, when set, supplies CODEOWNERS-derived owners for a
	// component's source path (orun-service-catalog SC1, S-2). It is provided by
	// the caller (which reads CODEOWNERS) so catalogresolve stays pure. Authored
	// ownership still wins; this fills the gap when none is authored.
	OwnerResolver OwnerResolver
	// CompositionResolver, when set, maps a component type to the composition
	// that backs it (from the composition lock, SC7), emitting composedBy
	// relations + derived Composition entities. Caller-provided to keep
	// catalogresolve pure; derived identically at every catalog-build path so the
	// catalog content id stays path-independent.
	CompositionResolver CompositionResolver
	// InputsDigest is WorkspaceInputsDigest(root) — the CODEOWNERS + composition
	// lock digest folded into the resolve-memo key, so a change to those files
	// (which feed the catalog but may be untracked) always misses the memo
	// instead of serving a stale catalog.
	InputsDigest string
	// Warn, when set, receives one line per catalog problem worth a build
	// log's attention: today, every catalog ref that entities of different
	// kinds resolve to (nodes.EntityRefCollisions — the Component keeps the
	// ref). Called on a memo hit too, from the issues the cached catalog
	// recorded, so a re-plan of an unchanged tree still says it.
	Warn func(msg string)
}

// OwnerResolver maps a component's workspace-relative source path to its
// CODEOWNERS owners (the first is primary), or nil when none match.
type OwnerResolver func(path string) []string

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

// RefreshResult carries the ids written by RefreshCatalog.
type RefreshResult struct {
	SourceID  objectstore.ObjectID
	CatalogID objectstore.ObjectID
	// CatalogSkipped is the resolve error the refresh tolerated (non-strict
	// mode) when it left CatalogID empty and the catalogs/* refs unmoved. It
	// is nil whenever CatalogID is set. Callers that need the catalog — the
	// --changed selection does — must treat it as a failure; callers for
	// which the catalog is best-effort may log it.
	CatalogSkipped error
}

// ErrCatalogSkipped reports that a tolerant (non-strict) catalog write skipped
// the catalog edge because the resolver failed. Err is the resolve error.
type ErrCatalogSkipped struct {
	Err error
}

func (e *ErrCatalogSkipped) Error() string {
	return "objplan: catalog skipped: " + e.Err.Error()
}

func (e *ErrCatalogSkipped) Unwrap() error { return e.Err }

// RefreshCatalog writes source → (catalog, memoized) to the object graph and
// moves the catalogs/* refs (incl. catalogs/current) — the explicit-refresh
// seam used by `orun catalog refresh` and the universal refresh hook. Unlike
// Plan it writes no revision/trigger: a refresh records the current full
// catalog, not a plan. Catalog resolution is memoized by (sourceId,
// resolverVersion) exactly as in Plan, so a re-refresh on an unchanged source
// only moves refs.
func RefreshCatalog(ctx context.Context, w *nodewriter.Writer, store objectstore.ObjectStore, memo *ResolveMemo, in Input, opts Options) (RefreshResult, error) {
	var res RefreshResult
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

	if opts.NoCatalog || in.Resolve == nil {
		return res, nil
	}
	catID, err := writeCatalogMemoized(ctx, w, store, memo.WithInputsDigest(opts.InputsDigest), src, srcID, rv, in.Resolve, opts)
	if err != nil {
		var skipped *ErrCatalogSkipped
		if errors.As(err, &skipped) {
			res.CatalogSkipped = skipped.Err
			return res, nil
		}
		return res, err
	}
	res.CatalogID = catID
	return res, nil
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
		catID, err = writeCatalogMemoized(ctx, w, store, memo.WithInputsDigest(opts.InputsDigest), src, srcID, rv, in.Resolve, opts)
		if err != nil {
			var skipped *ErrCatalogSkipped
			if !errors.As(err, &skipped) {
				return res, err
			}
			catID = "" // tolerant: the revision is written without a catalog edge
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
// and the memo is updated. A resolve error is fatal under strict; otherwise
// the catalog edge is skipped and the error comes back wrapped in
// *ErrCatalogSkipped so the caller decides whether that is fatal for it.
func writeCatalogMemoized(
	ctx context.Context,
	w *nodewriter.Writer,
	store objectstore.ObjectStore,
	memo *ResolveMemo,
	src nodes.SourceSnapshot,
	srcID objectstore.ObjectID,
	resolverVersion int,
	resolve func() (*catalogresolve.CatalogView, error),
	opts Options,
) (objectstore.ObjectID, error) {
	catalogRefs := CatalogRefs(src)

	if memo != nil {
		if cached, ok := memo.Get(srcID, resolverVersion); ok {
			if has, err := store.Has(ctx, cached); err == nil && has {
				if err := w.MoveRefs(ctx, catalogRefs, cached); err != nil {
					return "", fmt.Errorf("objplan: refresh catalog refs: %w", err)
				}
				if opts.Warn != nil {
					warnCachedCatalogIssues(ctx, store, cached, opts.Warn)
				}
				return cached, nil
			}
		}
	}

	view, err := resolve()
	if err != nil {
		if opts.Strict {
			return "", fmt.Errorf("objplan: resolve catalog: %w", err)
		}
		return "", &ErrCatalogSkipped{Err: err} // tolerant: skip the catalog edge, but say so
	}
	cat, manifests, graphs, ownership, fingerprints := BuildCatalogNodes(view, resolverVersion, opts.OwnerResolver, opts.CompositionResolver)
	cat.SourceID = string(srcID)
	if opts.Warn != nil {
		for _, c := range nodes.EntityRefCollisions(manifests, cat) {
			opts.Warn(c.Message())
		}
	}
	catID, err := w.WriteCatalog(ctx, cat, manifests, graphs, ownership, fingerprints, catalogRefs...)
	if err != nil {
		return "", fmt.Errorf("objplan: write catalog: %w", err)
	}
	if memo != nil {
		_ = memo.Put(srcID, resolverVersion, catID) // derived cache; best-effort
	}
	return catID, nil
}
