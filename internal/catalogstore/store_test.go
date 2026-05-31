package catalogstore_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogstore"
)

// TestStubsReturnErrNotImplemented locks PR-2/PR-3 placeholder methods
// to ErrNotImplemented so a future implementer cannot accidentally swap
// in nil-return without updating both this test and the spec.
func TestStubsReturnErrNotImplemented(t *testing.T) {
	spy := newSpyStore()
	st := catalogstore.New(spy)
	ctx := context.Background()

	checks := []func() error{
		func() error { _, err := st.ResolveCurrentSource(ctx); return err },
		func() error { _, err := st.ResolveSource(ctx, catalogstore.RefSelector{}); return err },
		func() error { _, err := st.ResolveCatalog(ctx, catalogstore.RefSelector{}); return err },
		func() error { _, err := st.ResolveComponent(ctx, catalogstore.RefSelector{}, "n"); return err },
		func() error { _, err := st.ResolveComponentLatest(ctx, "ns/repo/name"); return err },
		func() error { return st.WriteRefs(ctx, catalogstore.RefUpdate{}) },
		func() error { return st.WriteGlobalIndexes(ctx, catalogstore.GlobalIndexUpdate{}) },
		func() error { return st.AppendComponentEvent(ctx, catalogmodel.ComponentHistoryEvent{}) },
	}
	for i, fn := range checks {
		err := fn()
		if !errors.Is(err, catalogstore.ErrNotImplemented) {
			t.Errorf("check[%d]: want ErrNotImplemented, got %v", i, err)
		}
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
