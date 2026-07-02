package catalogresolve

import "testing"

func TestRepoDeclFromIntent(t *testing.T) {
	// No intent / no repo block → no declaration (the feature is opt-in).
	if repoDeclFromIntent(nil, "default", "orun") != nil {
		t.Error("nil intent should yield nil")
	}
	if repoDeclFromIntent(&intentFile{}, "default", "orun") != nil {
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
	d := repoDeclFromIntent(in, "sourceplane", "orun")
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
	d2 := repoDeclFromIntent(&intentFile{Repo: &intentRepoBlock{Description: "block desc"}}, "default", "orun")
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
