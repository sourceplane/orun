package objplan

import (
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
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

func TestDocsWireShapes(t *testing.T) {
	// Attached overview + attached page + declared-only page (CD1 wire shape,
	// model.md §2b): overview {path,commit,sha}, page {key,title,role,path,
	// commit,size}, declared-only {key,path,reason}; digests stamp at assembly.
	resolved := []catalogmodel.ResolvedDoc{
		{Key: "overview", Path: "docs/overview.md", Commit: "c0ffee", SHA: "aa", Bytes: []byte("# O\n")},
		{Key: "arch", Title: "Architecture", Role: "architecture", Path: "docs/arch.md", Commit: "c0ffee", Bytes: []byte("# A\n")},
		{Key: "missing", Title: "Missing", Role: "guide", Path: "docs/missing.md", Reason: "unreadable: file not found"},
	}
	legacy := &catalogmodel.ComponentDocs{TechDocs: "docs/", Runbooks: []string{"ops/rb.md"}}
	docs, pending := docsWire(legacy, "", resolved)

	ov, _ := docs["overview"].(map[string]any)
	if ov == nil || ov["path"] != "docs/overview.md" || ov["commit"] != "c0ffee" || ov["sha"] != "aa" {
		t.Errorf("overview = %v", docs["overview"])
	}
	pages, _ := docs["pages"].([]any)
	if len(pages) != 2 {
		t.Fatalf("pages = %v", docs["pages"])
	}
	arch := pages[0].(map[string]any)
	if arch["key"] != "arch" || arch["role"] != "architecture" || arch["commit"] != "c0ffee" || arch["size"] != 4 {
		t.Errorf("arch page = %v", arch)
	}
	miss := pages[1].(map[string]any)
	if miss["key"] != "missing" || miss["reason"] != "unreadable: file not found" {
		t.Errorf("missing page = %v", miss)
	}
	if _, has := miss["size"]; has {
		t.Errorf("declared-only page must not carry size: %v", miss)
	}
	if docs["techdocs"] != "docs/" {
		t.Errorf("legacy techdocs lost: %v", docs)
	}
	if len(pending) != 2 || string(pending["overview"]) != "# O\n" || string(pending["arch"]) != "# A\n" {
		t.Errorf("pending = %v", pending)
	}

	// Declared-only overview keeps the WO bare-path shape.
	docs2, pending2 := docsWire(nil, "", []catalogmodel.ResolvedDoc{{Key: "overview", Path: "docs/o.md", Reason: "unreadable: file not found"}})
	if docs2["overview"] != "docs/o.md" || pending2 != nil {
		t.Errorf("declared-only overview = %v / %v", docs2, pending2)
	}

	// Legacy pointer fallback (no resolution ran).
	docs3, _ := docsWire(nil, "docs/legacy.md", nil)
	if docs3["overview"] != "docs/legacy.md" {
		t.Errorf("legacy overview = %v", docs3)
	}
}
