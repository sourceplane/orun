package catalogstore_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogstore"
)

// TestPR3ResolversImplemented confirms PR-3 wired up the five
// previously-stubbed Resolver methods. None of them may return
// ErrNotImplemented for a representative happy-or-NotFound call. The
// PR-2 sibling TestPR2WritersImplemented mirrors this idiom for the
// writer side; once both pass, ErrNotImplemented is unreachable from
// the public surface.
func TestPR3ResolversImplemented(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	checks := []func() error{
		func() error { _, err := st.ResolveCurrentSource(ctx); return err },
		func() error { _, err := st.ResolveSource(ctx, catalogstore.RefSelector{}); return err },
		func() error { _, err := st.ResolveCatalog(ctx, catalogstore.RefSelector{}); return err },
		func() error {
			_, err := st.ResolveComponent(ctx, catalogstore.RefSelector{}, "n")
			return err
		},
		func() error { _, err := st.ResolveComponentLatest(ctx, "ns/repo/name"); return err },
	}
	for i, fn := range checks {
		err := fn()
		if errors.Is(err, catalogstore.ErrNotImplemented) {
			t.Errorf("check[%d]: must NOT return ErrNotImplemented after PR-3: %v", i, err)
		}
		// Each check runs against an empty spy store, so the resolver
		// must report a typed not-found via statestore.ErrNotFound's
		// chain. A nil error here would mean the implementation is
		// silently fabricating data — fail loudly.
		if err == nil {
			t.Errorf("check[%d]: empty store must surface a not-found error, got nil", i)
		}
	}

	// RebuildIndexes is also a Resolver method (catalog-store.md §8,
	// T-STORE-3). On an empty store it must return nil — not
	// ErrNotImplemented and not a not-found — because "nothing to walk"
	// is success. Asserted separately from the not-found checks above.
	if err := st.RebuildIndexes(ctx); err != nil {
		t.Errorf("RebuildIndexes on empty store: want nil, got %v", err)
	}
	if errors.Is(st.RebuildIndexes(ctx), catalogstore.ErrNotImplemented) {
		t.Errorf("RebuildIndexes must NOT return ErrNotImplemented after PR-3")
	}
}

// TestPR2WritersImplemented confirms PR-2 wired up the three previously
// stubbed Writer methods. None of them may return ErrNotImplemented for
// a representative happy-path call.
func TestPR2WritersImplemented(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	// WriteRefs with both Source and Catalog nil → no-op success.
	if err := st.WriteRefs(ctx, catalogstore.RefUpdate{}); err != nil {
		t.Errorf("WriteRefs(empty): %v", err)
	}
	if errors.Is(nil, catalogstore.ErrNotImplemented) {
		t.Fatalf("sanity: ErrNotImplemented Is(nil) must be false")
	}
	// WriteGlobalIndexes with no entries → no-op success.
	if err := st.WriteGlobalIndexes(ctx, catalogstore.GlobalIndexUpdate{}); err != nil {
		t.Errorf("WriteGlobalIndexes(empty): %v", err)
	}
	// AppendComponentEvent on a zero event surfaces a validation error,
	// NOT ErrNotImplemented — that's the contract we're pinning.
	err := st.AppendComponentEvent(ctx, catalogmodel.ComponentHistoryEvent{})
	if err == nil {
		t.Errorf("AppendComponentEvent(zero) should fail validation, got nil")
	}
	if errors.Is(err, catalogstore.ErrNotImplemented) {
		t.Errorf("AppendComponentEvent must NOT return ErrNotImplemented after PR-2: %v", err)
	}
}

// TestPathHelpers_AllErrorPathsExercised drives every helper through a
// failing input so each helper's validate-and-return-error branch is
// covered. Each helper has an early-error guard the happy-path test
// never exercises; this batches them into one O(N) loop.
func TestPathHelpers_AllErrorPathsExercised(t *testing.T) {
	bad := strings.Repeat("X", 200) // uppercase + oversize → fails ValidateSegment

	type tc struct {
		label string
		fn    func() (string, error)
	}
	const goodSrc = "src-branch-main-cabcdef-tabcdef0"
	const goodCat = "cat-deadbeef"

	cases := []tc{
		{"SourceDir", func() (string, error) { return catalogstore.SourceDir(bad) }},
		{"SourceDocPath", func() (string, error) { return catalogstore.SourceDocPath(bad) }},
		{"CatalogDir/srcKey", func() (string, error) { return catalogstore.CatalogDir(bad, goodCat) }},
		{"CatalogDir/catKey", func() (string, error) { return catalogstore.CatalogDir(goodSrc, bad) }},
		{"CatalogDocPath/srcKey", func() (string, error) { return catalogstore.CatalogDocPath(bad, goodCat) }},
		{"ComponentDir/srcKey", func() (string, error) { return catalogstore.ComponentDir(bad, goodCat, "x") }},
		{"ComponentDir/name", func() (string, error) { return catalogstore.ComponentDir(goodSrc, goodCat, bad) }},
		{"ComponentManifestPath/name", func() (string, error) {
			return catalogstore.ComponentManifestPath(goodSrc, goodCat, bad)
		}},
		{"CatalogGraphPath/srcKey", func() (string, error) {
			return catalogstore.CatalogGraphPath(bad, goodCat, "owners")
		}},
		{"CatalogGraphPath/kind", func() (string, error) {
			return catalogstore.CatalogGraphPath(goodSrc, goodCat, "BAD")
		}},
		{"CatalogRevisionDir/rev", func() (string, error) {
			return catalogstore.CatalogRevisionDir(goodSrc, goodCat, bad)
		}},
		{"CatalogRevisionPlanPath/rev", func() (string, error) {
			return catalogstore.CatalogRevisionPlanPath(goodSrc, goodCat, bad)
		}},
		{"CatalogExecutionDir/exec", func() (string, error) {
			return catalogstore.CatalogExecutionDir(goodSrc, goodCat, "rev-1", bad)
		}},
		{"SourceRefPath", func() (string, error) { return catalogstore.SourceRefPath("BAD") }},
		{"SourceBranchRefPath", func() (string, error) { return catalogstore.SourceBranchRefPath(bad) }},
		{"SourcePRRefPath", func() (string, error) { return catalogstore.SourcePRRefPath(bad) }},
		{"CatalogRefPath", func() (string, error) { return catalogstore.CatalogRefPath("BAD") }},
		{"CatalogBranchRefPath", func() (string, error) { return catalogstore.CatalogBranchRefPath(bad) }},
		{"CatalogPRRefPath", func() (string, error) { return catalogstore.CatalogPRRefPath(bad) }},
		{"ComponentLocalIndexPath/name", func() (string, error) {
			return catalogstore.ComponentLocalIndexPath(goodSrc, goodCat, bad)
		}},
		{"OwnerLocalIndexPath", func() (string, error) {
			return catalogstore.OwnerLocalIndexPath(goodSrc, goodCat, bad)
		}},
		{"SystemLocalIndexPath", func() (string, error) {
			return catalogstore.SystemLocalIndexPath(goodSrc, goodCat, bad)
		}},
		{"DomainLocalIndexPath", func() (string, error) {
			return catalogstore.DomainLocalIndexPath(goodSrc, goodCat, bad)
		}},
		{"TypeLocalIndexPath", func() (string, error) {
			return catalogstore.TypeLocalIndexPath(goodSrc, goodCat, bad)
		}},
		{"CatalogGlobalIndexPath", func() (string, error) { return catalogstore.CatalogGlobalIndexPath(bad) }},
		{"SourceGlobalIndexPath", func() (string, error) { return catalogstore.SourceGlobalIndexPath(bad) }},
		{"ComponentHistoryEventPath/name", func() (string, error) {
			return catalogstore.ComponentHistoryEventPath(goodSrc, goodCat, bad, 1, "ev")
		}},
	}

	for _, c := range cases {
		got, err := c.fn()
		if err == nil {
			t.Errorf("%s: expected error, got %q", c.label, got)
		}
	}
}
