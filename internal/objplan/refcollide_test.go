package objplan

import (
	"context"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/objcatalog"
)

// domainView is a resolved catalog whose web component declares a domain of
// its own name (the pre-sweep turso-starter shape) when domain == "web", and a
// clean one otherwise.
func domainView(domain string) *catalogresolve.CatalogView {
	return &catalogresolve.CatalogView{
		ResolvedCatalog: &catalogresolve.ResolvedCatalog{
			Namespace: "ns",
			Repo:      "repo",
			Manifests: []*catalogmodel.ComponentManifest{
				{
					Identity: catalogmodel.ComponentIdentity{ComponentKey: "ns/repo/db", Name: "db", Namespace: "ns", Repo: "repo", SourceFile: "packages/db/component.yaml"},
					Spec:     catalogmodel.ComponentSpec{Domain: "data"},
				},
				{
					Identity: catalogmodel.ComponentIdentity{ComponentKey: "ns/repo/web", Name: "web", Namespace: "ns", Repo: "repo", SourceFile: "apps/web/component.yaml"},
					Spec:     catalogmodel.ComponentSpec{Domain: domain},
				},
			},
		},
		Snapshot: &catalogmodel.CatalogSnapshot{CatalogSnapshotKey: "cat-web"},
	}
}

// TestPlanWarnsWhenADomainShadowsAComponent: the plan path names both
// declarations in a warning, keeps the web Component in the catalog it
// writes, leaves the Domain of the same ref out — and says so again on a memo
// hit, so a re-plan of the same tree still carries it in its log.
func TestPlanWarnsWhenADomainShadowsAComponent(t *testing.T) {
	t.Parallel()
	w, store, memo, refs := rig(t)
	ctx := context.Background()
	var warned []string
	opts := Options{Warn: func(m string) { warned = append(warned, m) }}
	resolve := func() (*catalogresolve.CatalogView, error) { return domainView("web"), nil }

	first, err := Plan(ctx, w, store, memo, sampleInput(resolve), opts)
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(warned) != 1 {
		t.Fatalf("warnings = %q, want one", warned)
	}
	for _, want := range []string{
		"catalog ref ns/repo/web",
		`Component "web" (apps/web/component.yaml)`,
		`Domain "web" (spec.domain in apps/web/component.yaml)`,
		"keeps the Component",
	} {
		if !strings.Contains(warned[0], want) {
			t.Errorf("warning missing %q:\n%s", want, warned[0])
		}
	}

	view, err := objcatalog.New(store, refs).Load(ctx, "catalogs/main")
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	var comps []string
	for _, c := range view.Components {
		comps = append(comps, c.ComponentKey)
	}
	if strings.Join(comps, ",") != "ns/repo/db,ns/repo/web" {
		t.Errorf("components = %v, want db and web (the web component survives)", comps)
	}
	var domains []string
	for _, e := range view.Entities {
		if e.Kind == catalogmodel.EntityKindDomain {
			domains = append(domains, e.EntityKey)
		}
	}
	if strings.Join(domains, ",") != "ns/repo/data" {
		t.Errorf("domains = %v, want only ns/repo/data (the web Domain is left out)", domains)
	}

	// Memo hit: no resolve, same catalog, the same warning.
	warned = nil
	second, err := Plan(ctx, w, store, memo, sampleInput(func() (*catalogresolve.CatalogView, error) {
		t.Fatal("memo hit must not resolve")
		return nil, nil
	}), opts)
	if err != nil {
		t.Fatalf("second Plan: %v", err)
	}
	if second.CatalogID != first.CatalogID {
		t.Fatalf("memo hit changed the catalog id")
	}
	if len(warned) != 1 || !strings.Contains(warned[0], `Domain "web"`) {
		t.Errorf("memo-hit warnings = %q, want the collision again", warned)
	}
}

// TestPlanCleanCatalogDoesNotWarn: distinct names, no warning, and the Domain
// is written as before.
func TestPlanCleanCatalogDoesNotWarn(t *testing.T) {
	t.Parallel()
	w, store, memo, refs := rig(t)
	ctx := context.Background()
	var warned []string
	if _, err := Plan(ctx, w, store, memo, sampleInput(func() (*catalogresolve.CatalogView, error) { return domainView("app"), nil }),
		Options{Warn: func(m string) { warned = append(warned, m) }}); err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if len(warned) != 0 {
		t.Fatalf("warnings = %q, want none", warned)
	}
	view, err := objcatalog.New(store, refs).Load(ctx, "catalogs/main")
	if err != nil {
		t.Fatalf("load catalog: %v", err)
	}
	n := 0
	for _, e := range view.Entities {
		if e.Kind == catalogmodel.EntityKindDomain {
			n++
		}
	}
	if n != 2 {
		t.Errorf("domains = %d, want app and data", n)
	}
	if got := EntityRefCollisions(domainView("app"), nil, nil); len(got) != 0 {
		t.Errorf("EntityRefCollisions = %+v, want none", got)
	}
}

// TestEntityRefCollisionsSeesCompositions: the lint-side helper builds the same
// nodes the plan writes, so a Composition entity (only known through the
// composition lock) named like a component is caught too.
func TestEntityRefCollisionsSeesCompositions(t *testing.T) {
	t.Parallel()
	view := domainView("app")
	view.Manifests[1].Spec.Type = "worker"
	comp := func(t string) *CompositionMeta {
		if t == "worker" {
			return &CompositionMeta{Name: "web"}
		}
		return nil
	}
	got := EntityRefCollisions(view, nil, comp)
	if len(got) != 1 || got[0].EntityRef != "ns/repo/web" || len(got[0].Claims) != 2 || got[0].Claims[1].Kind != catalogmodel.EntityKindComposition {
		t.Fatalf("collisions = %+v, want web Component vs web Composition", got)
	}
	if !strings.Contains(got[0].Message(), `Composition "web" (spec.type in apps/web/component.yaml)`) {
		t.Errorf("message = %s", got[0].Message())
	}
	if got := EntityRefCollisions(nil, nil, nil); got != nil {
		t.Errorf("nil view = %+v", got)
	}
}
