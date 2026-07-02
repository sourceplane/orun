package objplan

import (
	"testing"

	"github.com/sourceplane/orun/internal/catalogresolve"
	"github.com/sourceplane/orun/internal/nodes"
)

func TestBuildCatalogNodesEmitsRepoEntity(t *testing.T) {
	view := &catalogresolve.CatalogView{
		ResolvedCatalog: &catalogresolve.ResolvedCatalog{
			RepoDecl: &catalogresolve.RepoDeclaration{
				EntityKey:   "default/orun/orun",
				Name:        "orun",
				Namespace:   "default",
				Repo:        "orun",
				DisplayName: "Orun",
				Description: "the platform",
				Owner:       "group:platform",
				Overview:    "docs/overview.md",
				Links:       []catalogresolve.RepoLink{{Title: "Runbook", URL: "https://x", Icon: "book"}},
				Tags:        []string{"saas"},
			},
		},
	}
	cat, _, _, _, _ := BuildCatalogNodes(view, 15, nil, nil)
	if len(cat.DeclaredEntities) != 1 {
		t.Fatalf("DeclaredEntities = %d, want 1", len(cat.DeclaredEntities))
	}
	e := cat.DeclaredEntities[0]
	if e.Kind != nodes.EntityKindRepo || e.Identity.EntityKey != "default/orun/orun" || e.Identity.Name != "orun" {
		t.Errorf("identity = %+v (kind %q)", e.Identity, e.Kind)
	}
	if e.Docs["overview"] != "docs/overview.md" {
		t.Errorf("docs = %v", e.Docs)
	}
	if e.Ownership["owner"] != "group:platform" {
		t.Errorf("ownership = %v", e.Ownership)
	}
	if e.Metadata["displayName"] != "Orun" || e.Metadata["description"] != "the platform" {
		t.Errorf("metadata = %v", e.Metadata)
	}
	if tags, _ := e.Metadata["tags"].([]any); len(tags) != 1 || tags[0] != "saas" {
		t.Errorf("tags = %v", e.Metadata["tags"])
	}
	if len(e.Links) != 1 || e.Links[0]["title"] != "Runbook" || e.Links[0]["icon"] != "book" {
		t.Errorf("links = %v", e.Links)
	}
}

func TestBuildCatalogNodesNoRepoBlock(t *testing.T) {
	// No repo declaration → no declared entities (existing catalogs unchanged).
	cat, _, _, _, _ := BuildCatalogNodes(
		&catalogresolve.CatalogView{ResolvedCatalog: &catalogresolve.ResolvedCatalog{}}, 15, nil, nil)
	if len(cat.DeclaredEntities) != 0 {
		t.Errorf("DeclaredEntities = %d, want 0", len(cat.DeclaredEntities))
	}
}
