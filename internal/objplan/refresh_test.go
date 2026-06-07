package objplan

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/catalogresolve"
)

func TestRefreshCatalogWritesSourceAndCatalog(t *testing.T) {
	t.Parallel()
	w, _, memo, refs := rig(t)
	ctx := context.Background()

	res, err := RefreshCatalog(ctx, w, w.Store(), memo,
		sampleInput(func() (*catalogresolve.CatalogView, error) { return sampleView(), nil }),
		Options{})
	if err != nil {
		t.Fatalf("RefreshCatalog: %v", err)
	}
	if res.SourceID == "" || res.CatalogID == "" {
		t.Fatalf("ids = %+v", res)
	}
	// catalogs/current points at the written catalog (main scope ⇒ also catalogs/main).
	if r, _ := refs.Read(ctx, "catalogs/current"); r.Target != string(res.CatalogID) {
		t.Errorf("catalogs/current = %s, want %s", r.Target, res.CatalogID)
	}
	if r, _ := refs.Read(ctx, "sources/current"); r.Target != string(res.SourceID) {
		t.Errorf("sources/current = %s, want %s", r.Target, res.SourceID)
	}
}

func TestRefreshCatalogMemoizesAcrossCalls(t *testing.T) {
	t.Parallel()
	w, _, memo, _ := rig(t)
	ctx := context.Background()
	n := 0
	resolve := func() (*catalogresolve.CatalogView, error) { n++; return sampleView(), nil }
	in := sampleInput(resolve)

	a, err := RefreshCatalog(ctx, w, w.Store(), memo, in, Options{})
	if err != nil {
		t.Fatalf("first: %v", err)
	}
	b, err := RefreshCatalog(ctx, w, w.Store(), memo, in, Options{})
	if err != nil {
		t.Fatalf("second: %v", err)
	}
	if a.CatalogID != b.CatalogID {
		t.Errorf("memoized catalog id changed: %s vs %s", a.CatalogID, b.CatalogID)
	}
	if n != 1 {
		t.Errorf("resolve ran %d times, want 1 (memoized)", n)
	}
}

func TestRefreshCatalogNoCatalog(t *testing.T) {
	t.Parallel()
	w, _, memo, _ := rig(t)
	ctx := context.Background()

	res, err := RefreshCatalog(ctx, w, w.Store(), memo,
		sampleInput(func() (*catalogresolve.CatalogView, error) { return sampleView(), nil }),
		Options{NoCatalog: true})
	if err != nil {
		t.Fatalf("RefreshCatalog: %v", err)
	}
	if res.SourceID == "" {
		t.Fatalf("source must still be written")
	}
	if res.CatalogID != "" {
		t.Errorf("NoCatalog ⇒ no catalog id, got %s", res.CatalogID)
	}
}
