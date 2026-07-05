package catalogresolve

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

func TestRepoDeclFromIntent(t *testing.T) {
	// No intent / no repo block → no declaration (the feature is opt-in).
	if repoDeclFromIntent(nil, "default", "orun", nil) != nil {
		t.Error("nil intent should yield nil")
	}
	if repoDeclFromIntent(&intentFile{}, "default", "orun", nil) != nil {
		t.Error("intent without a repo block should yield nil")
	}

	// A full block: entity key is repo-local (<ns>/<repo>/<name>), name comes
	// from metadata, description defaults from metadata when the block omits it.
	in := &intentFile{
		Metadata: &intentMetadata{Name: "lumen", Description: "meta desc"},
		Repo: &intentRepoBlock{
			DisplayName: "Lumen Platform",
			Owner:       "group:platform",
			Docs:        &intentRepoDocs{Overview: "docs/overview.md"},
			Links:       []intentRepoLink{{Title: "Runbook", URL: "https://x", Icon: "book"}},
			Tags:        []string{"saas"},
		},
	}
	d := repoDeclFromIntent(in, "sourceplane", "orun", nil)
	if d == nil {
		t.Fatal("expected a declaration")
	}
	if d.EntityKey != "sourceplane/orun/lumen" {
		t.Errorf("EntityKey = %q, want sourceplane/orun/lumen", d.EntityKey)
	}
	if d.Name != "lumen" || d.DisplayName != "Lumen Platform" {
		t.Errorf("name/display = %q/%q", d.Name, d.DisplayName)
	}
	if d.Description != "meta desc" {
		t.Errorf("Description = %q, want defaulted from metadata", d.Description)
	}
	if d.Owner != "group:platform" || d.Overview != "docs/overview.md" {
		t.Errorf("owner/overview = %q/%q", d.Owner, d.Overview)
	}
	if len(d.Links) != 1 || d.Links[0].Title != "Runbook" || d.Links[0].Icon != "book" {
		t.Errorf("links = %+v", d.Links)
	}
	if len(d.Tags) != 1 || d.Tags[0] != "saas" {
		t.Errorf("tags = %+v", d.Tags)
	}

	// Defaults: no metadata → name = repo segment, displayName = name; a block
	// description takes precedence over metadata.
	d2 := repoDeclFromIntent(&intentFile{Repo: &intentRepoBlock{Description: "block desc"}}, "default", "orun", nil)
	if d2.Name != "orun" || d2.EntityKey != "default/orun/orun" {
		t.Errorf("defaulted name/key = %q/%q", d2.Name, d2.EntityKey)
	}
	if d2.DisplayName != "orun" {
		t.Errorf("defaulted displayName = %q", d2.DisplayName)
	}
	if d2.Description != "block desc" {
		t.Errorf("Description = %q, want block value", d2.Description)
	}
}

func TestRepoDeclResolvesDocSet(t *testing.T) {
	// The doc set (overview + pages) resolves through the doc context: bytes
	// read, sha256'd, titles defaulted from the first H1 (WO3b + CD1).
	dir := t.TempDir()
	body := []byte("# Overview\n\nHello.\n")
	arch := []byte("# The Architecture\n\nBoxes.\n")
	if err := os.MkdirAll(filepath.Join(dir, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "overview.md"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docs", "architecture.md"), arch, 0o644); err != nil {
		t.Fatal(err)
	}
	in := &intentFile{Repo: &intentRepoBlock{Docs: &intentRepoDocs{
		Overview: "docs/overview.md",
		Pages:    []catalogmodel.DocPage{{Path: "docs/architecture.md", Role: "architecture"}},
	}}}
	d := repoDeclFromIntent(in, "default", "orun", newDocResolveContext(dir))
	if len(d.Docs) != 2 {
		t.Fatalf("Docs = %+v, want overview + 1 page", d.Docs)
	}
	ov := d.Docs[0]
	if ov.Key != "overview" || string(ov.Bytes) != string(body) {
		t.Errorf("overview = %+v", ov)
	}
	sum := sha256.Sum256(body)
	if ov.SHA != hex.EncodeToString(sum[:]) {
		t.Errorf("overview SHA = %q", ov.SHA)
	}
	pg := d.Docs[1]
	if pg.Key != "architecture" || pg.Role != "architecture" || !pg.Attached() {
		t.Errorf("page = %+v", pg)
	}
	if pg.Title != "The Architecture" {
		t.Errorf("page title = %q, want first H1", pg.Title)
	}

	// A missing file stays a declared-only entry (path pointer, reason set).
	miss := repoDeclFromIntent(&intentFile{Repo: &intentRepoBlock{Docs: &intentRepoDocs{Overview: "docs/nope.md"}}}, "default", "orun", newDocResolveContext(dir))
	if miss.Overview != "docs/nope.md" {
		t.Errorf("missing-file path = %q", miss.Overview)
	}
	if len(miss.Docs) != 1 || miss.Docs[0].Attached() || miss.Docs[0].Reason == "" {
		t.Errorf("missing-file doc = %+v", miss.Docs)
	}
}
