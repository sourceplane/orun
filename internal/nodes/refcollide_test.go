package nodes

import (
	"context"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

// webDomainClash is the catalog the turso-starter baseline built before its
// sweep: apps/web/component.yaml declared `name: web` and `spec.domain: web`,
// so the Domain and the Component resolved to one ref.
func webDomainClash() []ComponentManifest {
	return []ComponentManifest{
		{Identity: ComponentIdentity{ComponentKey: "ns/repo/web", Name: "web", Namespace: "ns", Repo: "repo", Path: "apps/web/component.yaml"},
			Relations: []EntityRelation{{Type: "partOf", To: "ns/repo/web", ToKind: EntityKindDomain}}},
		{Identity: ComponentIdentity{ComponentKey: "ns/repo/db", Name: "db", Namespace: "ns", Repo: "repo", Path: "packages/db/component.yaml"},
			Relations: []EntityRelation{{Type: "partOf", To: "ns/repo/data", ToKind: EntityKindDomain}}},
	}
}

func decodeCatalog(t *testing.T, s *objectstore.MemStore, catID objectstore.ObjectID) CatalogSnapshot {
	t.Helper()
	catBlob, _ := findEntry(t, s, catID, fileCatalog)
	snap, err := Decode[CatalogSnapshot]([]byte(blobBody(t, s, catBlob)))
	if err != nil {
		t.Fatalf("decode catalog: %v", err)
	}
	return snap
}

func entityKeysOfKind(t *testing.T, s *objectstore.MemStore, catID objectstore.ObjectID, kind string) []string {
	t.Helper()
	ctx := context.Background()
	entTree, _ := findEntry(t, s, catID, dirEntities)
	kinds, _ := s.GetTree(ctx, entTree)
	var keys []string
	for _, kd := range kinds {
		if kd.Name != kind {
			continue
		}
		blobs, _ := s.GetTree(ctx, kd.ID)
		for _, b := range blobs {
			e, err := Decode[Entity]([]byte(blobBody(t, s, b.ID)))
			if err != nil {
				t.Fatalf("decode %s entity: %v", kind, err)
			}
			keys = append(keys, e.Identity.EntityKey)
		}
	}
	return keys
}

// TestAssembleCatalogComponentKeepsItsRef: a Domain named like a component is
// left out of the catalog — the Component keeps the ref instead of whichever
// a ref-keyed reader projects last — and the catalog records why.
func TestAssembleCatalogComponentKeepsItsRef(t *testing.T) {
	t.Parallel()
	s := mem()
	manifests := webDomainClash()
	catID, err := AssembleCatalog(context.Background(), s, CatalogSnapshot{SourceID: goodID("a"), ResolverVersion: 1}, manifests, nil, ImpactOwnership{}, nil)
	if err != nil {
		t.Fatalf("AssembleCatalog: %v", err)
	}

	// The component survives under components/.
	compTree, _ := findEntry(t, s, catID, dirComponents)
	comps, _ := s.GetTree(context.Background(), compTree)
	names := map[string]bool{}
	for _, c := range comps {
		names[c.Name] = true
	}
	if !names["web.json"] || !names["db.json"] {
		t.Fatalf("components/ = %v, want web.json and db.json", names)
	}
	// The colliding Domain is withheld; the other Domain is written.
	if got := entityKeysOfKind(t, s, catID, EntityKindDomain); len(got) != 1 || got[0] != "ns/repo/data" {
		t.Fatalf("Domain entities = %v, want only ns/repo/data", got)
	}

	snap := decodeCatalog(t, s, catID)
	if snap.CountsByKind[EntityKindDomain] != 1 || snap.CountsByKind[EntityKindComponent] != 2 {
		t.Errorf("countsByKind = %v, want Domain 1, Component 2", snap.CountsByKind)
	}
	if len(snap.Issues) != 1 {
		t.Fatalf("issues = %+v, want one collision", snap.Issues)
	}
	is := snap.Issues[0]
	if is.Code != IssueCodeEntityRefCollision || is.Severity != "warning" || is.Component != "ns/repo/web" {
		t.Errorf("issue = %+v", is)
	}
	for _, want := range []string{
		"catalog ref ns/repo/web",
		`Component "web" (apps/web/component.yaml)`,
		`Domain "web" (spec.domain in apps/web/component.yaml)`,
		"keeps the Component and leaves the Domain out",
		"Rename one of them — e.g. the Domain's spec.domain in apps/web/component.yaml",
	} {
		if !strings.Contains(is.Message, want) {
			t.Errorf("message missing %q:\n%s", want, is.Message)
		}
	}
}

// TestEntityRefCollisionsCleanCatalog: a catalog where every ref names one
// entity reports nothing, and its catalog.json carries no issues.
func TestEntityRefCollisionsCleanCatalog(t *testing.T) {
	t.Parallel()
	manifests := []ComponentManifest{
		{Identity: ComponentIdentity{ComponentKey: "ns/repo/web", Name: "web", Namespace: "ns", Repo: "repo", Path: "apps/web/component.yaml"},
			Relations: []EntityRelation{
				{Type: "partOf", To: "ns/repo/app", ToKind: EntityKindDomain},
				{Type: "partOf", To: "ns/repo/shop", ToKind: EntityKindSystem},
				{Type: "deployedTo", To: "ns/repo/prod", ToKind: EntityKindEnvironment},
			}},
	}
	cat := CatalogSnapshot{SourceID: goodID("a"), ResolverVersion: 1,
		DeclaredEntities: []Entity{{Kind: EntityKindRepo, Identity: EntityIdentity{EntityKey: "ns/repo/repo", Kind: EntityKindRepo, Name: "repo"}}}}
	if got := EntityRefCollisions(manifests, cat); len(got) != 0 {
		t.Fatalf("collisions = %+v, want none", got)
	}
	s := mem()
	catID, err := AssembleCatalog(context.Background(), s, cat, manifests, nil, ImpactOwnership{}, nil)
	if err != nil {
		t.Fatalf("AssembleCatalog: %v", err)
	}
	if body := blobBody(t, s, mustEntry(t, s, catID, fileCatalog)); strings.Contains(body, `"issues"`) {
		t.Errorf("clean catalog.json carries issues: %s", body)
	}
	if got := entityKeysOfKind(t, s, catID, EntityKindDomain); len(got) != 1 {
		t.Errorf("Domain entities = %v, want ns/repo/app", got)
	}
}

func mustEntry(t *testing.T, s *objectstore.MemStore, tree objectstore.ObjectID, name string) objectstore.ObjectID {
	t.Helper()
	id, _ := findEntry(t, s, tree, name)
	return id
}

// TestEntityRefCollisionsEveryKind: every kind that mints a repo-local ref is
// checked against the components — declared entities (Repo, AgentType, a
// declared environment) as well as derived ones — and each claim names where
// it is declared.
func TestEntityRefCollisionsEveryKind(t *testing.T) {
	t.Parallel()
	manifests := []ComponentManifest{
		{Identity: ComponentIdentity{ComponentKey: "ns/repo/web", Name: "web", Namespace: "ns", Repo: "repo", Path: "apps/web/component.yaml"},
			Relations: []EntityRelation{{Type: "partOf", To: "ns/repo/api", ToKind: EntityKindSystem}}},
		{Identity: ComponentIdentity{ComponentKey: "ns/repo/api", Name: "api", Namespace: "ns", Repo: "repo", Path: "apps/api/component.yaml"},
			Contracts: map[string]any{"provides": []any{map[string]any{"api": "ns/repo/web"}}}},
	}
	cat := CatalogSnapshot{
		DeclaredEntities: []Entity{
			{Kind: EntityKindRepo, Identity: EntityIdentity{EntityKey: "ns/repo/web", Kind: EntityKindRepo, Name: "web"}},
			{Kind: EntityKindAgentType, Identity: EntityIdentity{EntityKey: "ns/repo/api", Kind: EntityKindAgentType, Name: "api", Path: "agents/api.md"}},
		},
		EntityEnrichments: []EntityEnrichment{{Kind: EntityKindEnvironment, Name: "web", Declared: true, EntityKey: "ns/repo/web"}},
	}
	got := EntityRefCollisions(manifests, cat)
	if len(got) != 2 {
		t.Fatalf("collisions = %+v, want ns/repo/api and ns/repo/web", got)
	}
	kinds := func(c EntityRefCollision) []string {
		var out []string
		for _, cl := range c.Claims {
			out = append(out, cl.Kind)
		}
		return out
	}
	if c := got[0]; c.EntityRef != "ns/repo/api" || strings.Join(kinds(c), ",") != "Component,AgentType,System" {
		t.Errorf("api collision = %s %v", c.EntityRef, kinds(c))
	}
	c := got[1]
	if c.EntityRef != "ns/repo/web" || strings.Join(kinds(c), ",") != "Component,API,Environment,Repo" {
		t.Fatalf("web collision = %s %v", c.EntityRef, kinds(c))
	}
	msg := c.Message()
	for _, want := range []string{
		`API "web" (providesApis/consumesApis in apps/api/component.yaml)`,
		`Environment "web" (environments.web in intent.yaml)`,
		`Repo "web" (metadata.name in intent.yaml)`,
		"leaves the API, Environment and Repo out",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
	if !strings.Contains(got[0].Message(), `AgentType "api" (agents/api.md)`) {
		t.Errorf("agent type source missing:\n%s", got[0].Message())
	}
	if _, src := got[0].Fix(); src.File != "agents/api.md" {
		t.Errorf("fix = %+v, want the agent type's file", src)
	}

	// All of them are withheld: the components keep both refs.
	kept := withheldByComponents(manifests, append(deriveEntities(manifests, cat.EntityEnrichments), cat.DeclaredEntities...))
	for _, e := range kept {
		if e.Identity.EntityKey == "ns/repo/web" || e.Identity.EntityKey == "ns/repo/api" {
			t.Errorf("entity %s %s kept, want withheld", e.Kind, e.Identity.EntityKey)
		}
	}
}

// TestEntityRefCollisionsWithoutComponentKeepsBoth: two non-Component kinds on
// one ref are reported, and both stay in the catalog — neither is the
// deployable, and the catalog tree tells them apart by kind.
func TestEntityRefCollisionsWithoutComponentKeepsBoth(t *testing.T) {
	t.Parallel()
	manifests := []ComponentManifest{
		{Identity: ComponentIdentity{ComponentKey: "ns/repo/billing-api", Name: "billing-api", Namespace: "ns", Repo: "repo", Path: "apps/billing-api/component.yaml"},
			Relations: []EntityRelation{
				{Type: "partOf", To: "ns/repo/billing", ToKind: EntityKindSystem},
				{Type: "partOf", To: "ns/repo/billing", ToKind: EntityKindDomain},
			}},
	}
	got := EntityRefCollisions(manifests, CatalogSnapshot{})
	if len(got) != 1 || got[0].HasComponent() {
		t.Fatalf("collisions = %+v, want one without a component", got)
	}
	msg := got[0].Message()
	for _, want := range []string{
		`Domain "billing" (spec.domain in apps/billing-api/component.yaml)`,
		`System "billing" (spec.system in apps/billing-api/component.yaml)`,
		"Both are kept here, but Orunbase keeps one entity per ref",
	} {
		if !strings.Contains(msg, want) {
			t.Errorf("message missing %q:\n%s", want, msg)
		}
	}
	if is := got[0].Issue(); is.Component != "" {
		t.Errorf("issue component = %q, want empty", is.Component)
	}

	s := mem()
	catID, err := AssembleCatalog(context.Background(), s, CatalogSnapshot{SourceID: goodID("b"), ResolverVersion: 1}, manifests, nil, ImpactOwnership{}, nil)
	if err != nil {
		t.Fatalf("AssembleCatalog: %v", err)
	}
	if d, sys := entityKeysOfKind(t, s, catID, EntityKindDomain), entityKeysOfKind(t, s, catID, EntityKindSystem); len(d) != 1 || len(sys) != 1 {
		t.Errorf("Domain %v / System %v, want both written", d, sys)
	}
	if snap := decodeCatalog(t, s, catID); len(snap.Issues) != 1 {
		t.Errorf("issues = %+v, want the collision recorded", snap.Issues)
	}
}

// TestDescribeClaimListsAtMostThreeSources keeps a domain shared by many
// components to one readable line.
func TestDescribeClaimListsAtMostThreeSources(t *testing.T) {
	t.Parallel()
	cl := EntityRefClaim{Kind: EntityKindDomain, Name: "web"}
	for _, f := range []string{"a/component.yaml", "b/component.yaml", "c/component.yaml", "d/component.yaml", "e/component.yaml"} {
		cl.Sources = append(cl.Sources, EntityRefSource{File: f, Field: "spec.domain"})
	}
	want := `Domain "web" (spec.domain in a/component.yaml, spec.domain in b/component.yaml, spec.domain in c/component.yaml and 2 more)`
	if got := describeClaim(cl); got != want {
		t.Errorf("describeClaim =\n%s\nwant\n%s", got, want)
	}
}
