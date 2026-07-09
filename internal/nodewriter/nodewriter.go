// Package nodewriter composes the L0–L1 layers (objectstore, refstore, nodes)
// into the tolerant-strict write walk of the orun-object-model
// (design.md §3). Given pre-resolved inputs — a SourceSnapshot, an optional
// CatalogSnapshot + manifests + graphs, a compiled plan, and trigger metadata —
// it writes the content spine in dependency order with Has-gated reuse and
// moves the relevant refs (the atomic publish points).
//
// Resolution itself (sourcectx → catalogresolve) is the caller's job; this
// package only persists. The resolve memoization and the CLI wiring of
// `orun plan` land in M5.
package nodewriter

import (
	"context"
	"errors"
	"fmt"

	"github.com/oklog/ulid/v2"
	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// Writer persists nodes and moves refs. It is safe for sequential use by one
// command invocation; concurrent invocations are serialized at the ref layer
// via CAS.
type Writer struct {
	store objectstore.ObjectStore
	refs  refstore.RefStore
	clk   clock.Clock
	newID func() string
}

// Option configures a Writer.
type Option func(*Writer)

// WithClock overrides the clock (default clock.New()).
func WithClock(c clock.Clock) Option { return func(w *Writer) { w.clk = c } }

// WithIDGen overrides the trigger id generator (default "trg_"+ULID).
func WithIDGen(fn func() string) Option { return func(w *Writer) { w.newID = fn } }

// New constructs a Writer over an object store and a ref store.
func New(store objectstore.ObjectStore, refs refstore.RefStore, opts ...Option) *Writer {
	w := &Writer{
		store: store,
		refs:  refs,
		clk:   clock.New(),
		newID: func() string { return "trg_" + ulid.Make().String() },
	}
	for _, o := range opts {
		o(w)
	}
	return w
}

// Store returns the underlying object store, for callers (e.g. objplan) that
// need Has checks against the same store the writer persists to.
func (w *Writer) Store() objectstore.ObjectStore { return w.store }

// moveRef points name at target via compare-and-swap, retrying on a lost race
// (another writer moved the same ref concurrently). A no-op when the ref already
// points at target.
func (w *Writer) moveRef(ctx context.Context, name string, target objectstore.ObjectID) error {
	const maxAttempts = 8
	for attempt := 0; attempt < maxAttempts; attempt++ {
		cur, err := w.refs.Read(ctx, name)
		old := ""
		switch {
		case err == nil:
			old = cur.Target
		case isNotFound(err):
			old = ""
		default:
			return fmt.Errorf("nodewriter: read ref %q: %w", name, err)
		}
		if old == string(target) {
			return nil
		}
		err = w.refs.Update(ctx, name, old, string(target))
		if err == nil {
			return nil
		}
		if isConflict(err) {
			continue // someone moved it; re-read and retry
		}
		return fmt.Errorf("nodewriter: move ref %q: %w", name, err)
	}
	return fmt.Errorf("nodewriter: move ref %q: too many conflicts", name)
}

// MoveRefs points each named ref at target via compare-and-swap (forward,
// retry-on-conflict). Exported for orchestration layers (e.g. objplan) that
// reuse an already-written object — such as a memoized catalog — and only need
// to refresh its pointers.
func (w *Writer) MoveRefs(ctx context.Context, names []string, target objectstore.ObjectID) error {
	return w.moveRefs(ctx, names, target)
}

func (w *Writer) moveRefs(ctx context.Context, names []string, target objectstore.ObjectID) error {
	for _, n := range names {
		if err := w.moveRef(ctx, n, target); err != nil {
			return err
		}
	}
	return nil
}

// WriteSource assembles the source (idempotent) and points sourceRefs at it.
func (w *Writer) WriteSource(ctx context.Context, src nodes.SourceSnapshot, sourceRefs ...string) (objectstore.ObjectID, error) {
	id, err := nodes.AssembleSource(ctx, w.store, src)
	if err != nil {
		return "", err
	}
	if err := w.moveRefs(ctx, sourceRefs, id); err != nil {
		return "", err
	}
	return id, nil
}

// WriteCatalog assembles the catalog (idempotent) and points catalogRefs at it.
func (w *Writer) WriteCatalog(ctx context.Context, cat nodes.CatalogSnapshot, manifests []nodes.ComponentManifest, graphs []nodes.CatalogGraph, ownership nodes.ImpactOwnership, fingerprints []nodes.ComponentFingerprint, catalogRefs ...string) (objectstore.ObjectID, error) {
	id, err := nodes.AssembleCatalog(ctx, w.store, cat, manifests, graphs, ownership, fingerprints)
	if err != nil {
		return "", err
	}
	if err := w.moveRefs(ctx, catalogRefs, id); err != nil {
		return "", err
	}
	return id, nil
}

// WriteRevision derives the revision id, reuses it when present (Has-gated),
// writes it otherwise, and points revisionRefs at it. reused reports whether the
// revision already existed — the basis for the "revision reused" CLI signal and
// for dedup across triggers.
func (w *Writer) WriteRevision(ctx context.Context, rev nodes.PlanRevision, planBytes []byte, revisionRefs ...string) (id objectstore.ObjectID, reused bool, err error) {
	rev.Kind = nodes.KindPlanRevision
	id, err = nodes.RevisionID(w.store.Algo(), rev, planBytes)
	if err != nil {
		return "", false, err
	}
	has, err := w.store.Has(ctx, id)
	if err != nil {
		return "", false, err
	}
	if !has {
		written, werr := nodes.AssembleRevision(ctx, w.store, rev, planBytes)
		if werr != nil {
			return "", false, werr
		}
		if written != id {
			return "", false, fmt.Errorf("nodewriter: revision id mismatch: precomputed %s, wrote %s", id, written)
		}
	}
	if err := w.moveRefs(ctx, revisionRefs, id); err != nil {
		return "", false, err
	}
	return id, has, nil
}

// RecordTrigger writes a trigger event (always new — events never dedup),
// minting a trigger id when trg.TriggerID is empty, and points triggerRefs at
// it. It sets RevisionID to the revision the trigger produced.
func (w *Writer) RecordTrigger(ctx context.Context, trg nodes.TriggerOccurrence, revisionID objectstore.ObjectID, triggerRefs ...string) (objectstore.ObjectID, error) {
	if trg.TriggerID == "" {
		trg.TriggerID = w.newID()
	}
	if trg.CreatedAt.IsZero() {
		trg.CreatedAt = w.clk.Now()
	}
	trg.RevisionID = string(revisionID)
	id, err := nodes.AssembleTrigger(ctx, w.store, trg)
	if err != nil {
		return "", err
	}
	if err := w.moveRefs(ctx, triggerRefs, id); err != nil {
		return "", err
	}
	return id, nil
}

// CatalogInput carries the optional catalog half of a plan walk.
type CatalogInput struct {
	Snapshot     nodes.CatalogSnapshot
	Manifests    []nodes.ComponentManifest
	Graphs       []nodes.CatalogGraph
	Ownership    nodes.ImpactOwnership
	Fingerprints []nodes.ComponentFingerprint
	Refs         []string
}

// PlanInput carries the pre-resolved inputs for the plan walk (steps 1–4 of
// design.md §3). Catalog is nil for a degenerate (no-catalog) plan.
type PlanInput struct {
	Source       nodes.SourceSnapshot
	SourceRefs   []string
	Catalog      *CatalogInput
	Revision     nodes.PlanRevision
	PlanBytes    []byte
	RevisionRefs []string
	Trigger      nodes.TriggerOccurrence
	TriggerRefs  []string
}

// PlanResult reports the ids the walk produced.
type PlanResult struct {
	SourceID       objectstore.ObjectID
	CatalogID      objectstore.ObjectID
	RevisionID     objectstore.ObjectID
	TriggerID      objectstore.ObjectID
	RevisionReused bool
}

// Plan runs the tolerant-strict write walk: source → (catalog) → revision →
// trigger, threading the parent ids into the revision's edges and moving each
// level's refs. The revision is Has-gated (reused when identical); the trigger
// is always a fresh event.
func (w *Writer) Plan(ctx context.Context, in PlanInput) (PlanResult, error) {
	var res PlanResult

	srcID, err := w.WriteSource(ctx, in.Source, in.SourceRefs...)
	if err != nil {
		return res, err
	}
	res.SourceID = srcID
	in.Revision.SourceID = string(srcID)

	if in.Catalog != nil {
		cat := in.Catalog.Snapshot
		cat.SourceID = string(srcID)
		catID, err := w.WriteCatalog(ctx, cat, in.Catalog.Manifests, in.Catalog.Graphs, in.Catalog.Ownership, in.Catalog.Fingerprints, in.Catalog.Refs...)
		if err != nil {
			return res, err
		}
		res.CatalogID = catID
		in.Revision.CatalogID = string(catID)
	}

	revID, reused, err := w.WriteRevision(ctx, in.Revision, in.PlanBytes, in.RevisionRefs...)
	if err != nil {
		return res, err
	}
	res.RevisionID = revID
	res.RevisionReused = reused

	trgID, err := w.RecordTrigger(ctx, in.Trigger, revID, in.TriggerRefs...)
	if err != nil {
		return res, err
	}
	res.TriggerID = trgID
	return res, nil
}

func isNotFound(err error) bool { return errors.Is(err, refstore.ErrNotFound) }
func isConflict(err error) bool { return errors.Is(err, refstore.ErrConflict) }

// WriteAgentType assembles a sealed agent type (idempotent — identical
// persona + envelope dedup to one object) and points agentTypeRefs at it
// (orun-agents AG1; conventionally refs "agents/types/<name>/latest").
func (w *Writer) WriteAgentType(ctx context.Context, at nodes.AgentTypeSnapshot, body []byte, literacyName string, literacyBody []byte, agentTypeRefs ...string) (objectstore.ObjectID, error) {
	id, err := nodes.AssembleAgentType(ctx, w.store, at, body, literacyName, literacyBody)
	if err != nil {
		return "", err
	}
	if err := w.moveRefs(ctx, agentTypeRefs, id); err != nil {
		return "", err
	}
	return id, nil
}
