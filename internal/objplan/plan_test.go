package objplan

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/sourcectx"
)

var errBoom = errors.New("boom")

func rig(t *testing.T) (*nodewriter.Writer, *objectstore.MemStore, *ResolveMemo, *refstore.LocalRefStore) {
	t.Helper()
	root := t.TempDir()
	store := objectstore.NewMemStore("")
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Clock: clock.Fixed{T: time.Unix(0, 0).UTC()}})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	var n int
	w := nodewriter.New(store, refs,
		nodewriter.WithClock(clock.Fixed{T: time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)}),
		nodewriter.WithIDGen(func() string { n++; return fmt.Sprintf("trg_%03d", n) }))
	return w, store, NewResolveMemo(root), refs
}

func sampleInput(resolve func() (*catalogresolve.CatalogView, error)) Input {
	return Input{
		Workspace:      sourcectx.WorkspaceState{Repo: "ns/repo", HeadRevision: "abc123def456", TreeHash: "9aa7710", Branch: "main"},
		SourceHumanKey: "src-main-abc",
		Resolve:        resolve,
		PlanBytes:      []byte(`{"plan":"A"}`),
		RevisionScope:  nodes.RevisionScope{Mode: "full"},
		JobCount:       1,
		Trigger: nodes.TriggerOccurrence{
			TriggerName: "system.manual",
			Source:      nodes.TriggerSource{Flavor: "system", System: "manual"},
			Scope:       nodes.RevisionScope{Mode: "full"}, Actor: "cli",
		},
	}
}

func TestPlanHappyWithCatalog(t *testing.T) {
	t.Parallel()
	w, _, memo, refs := rig(t)
	ctx := context.Background()
	res, err := Plan(ctx, w, w.Store(), memo, sampleInput(func() (*catalogresolve.CatalogView, error) { return sampleView(), nil }), Options{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if res.SourceID == "" || res.CatalogID == "" || res.RevisionID == "" || res.TriggerID == "" {
		t.Fatalf("incomplete: %+v", res)
	}
	// Catalog ref points at the catalog; revision ref at the revision.
	if r, _ := refs.Read(ctx, "refs/catalogs/main"); r.Target != string(res.CatalogID) {
		t.Fatalf("catalog ref = %s, want %s", r.Target, res.CatalogID)
	}
	if r, _ := refs.Read(ctx, "refs/revisions/latest"); r.Target != string(res.RevisionID) {
		t.Fatalf("revision ref mismatch")
	}
}

func TestPlanMemoizesCatalog(t *testing.T) {
	t.Parallel()
	w, _, memo, _ := rig(t)
	ctx := context.Background()
	calls := 0
	resolve := func() (*catalogresolve.CatalogView, error) { calls++; return sampleView(), nil }
	first, err := Plan(ctx, w, w.Store(), memo, sampleInput(resolve), Options{})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	second, err := Plan(ctx, w, w.Store(), memo, sampleInput(resolve), Options{})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if calls != 1 {
		t.Fatalf("resolver called %d times, want 1 (memo hit)", calls)
	}
	if first.CatalogID != second.CatalogID || first.RevisionID != second.RevisionID {
		t.Fatalf("memo hit changed ids")
	}
	if !second.RevisionReused {
		t.Fatalf("second revision should be reused")
	}
}

func TestPlanStaleMemoFallsThrough(t *testing.T) {
	t.Parallel()
	w, _, memo, _ := rig(t)
	ctx := context.Background()
	// Pre-seed the memo with a catalog id that was never written.
	src := BuildSourceNode(sampleInput(nil).Workspace, "")
	srcID, _ := nodes.SourceID(objectstore.AlgoSHA256, src)
	_ = memo.Put(srcID, 1, id("f"))
	calls := 0
	_, err := Plan(ctx, w, w.Store(), memo, sampleInput(func() (*catalogresolve.CatalogView, error) { calls++; return sampleView(), nil }), Options{})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if calls != 1 {
		t.Fatalf("stale memo should fall through to a resolve, calls=%d", calls)
	}
}

func TestPlanNoCatalog(t *testing.T) {
	t.Parallel()
	w, _, memo, _ := rig(t)
	called := false
	res, err := Plan(context.Background(), w, w.Store(), memo,
		sampleInput(func() (*catalogresolve.CatalogView, error) { called = true; return sampleView(), nil }),
		Options{NoCatalog: true})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if called {
		t.Fatalf("NoCatalog must not resolve")
	}
	if res.CatalogID != "" {
		t.Fatalf("NoCatalog should leave catalog id empty")
	}
}

func TestPlanStrictResolveError(t *testing.T) {
	t.Parallel()
	w, _, memo, _ := rig(t)
	_, err := Plan(context.Background(), w, w.Store(), memo,
		sampleInput(func() (*catalogresolve.CatalogView, error) { return nil, errBoom }),
		Options{Strict: true})
	if !errors.Is(err, errBoom) {
		t.Fatalf("strict resolve error = %v, want errBoom", err)
	}
}

func TestPlanTolerantResolveErrorSkipsCatalog(t *testing.T) {
	t.Parallel()
	w, _, memo, _ := rig(t)
	res, err := Plan(context.Background(), w, w.Store(), memo,
		sampleInput(func() (*catalogresolve.CatalogView, error) { return nil, errBoom }),
		Options{Strict: false})
	if err != nil {
		t.Fatalf("tolerant resolve error should not fail Plan: %v", err)
	}
	if res.CatalogID != "" {
		t.Fatalf("tolerant skip should leave catalog id empty")
	}
	if res.RevisionID == "" {
		t.Fatalf("revision should still be written")
	}
}
