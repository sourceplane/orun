package catalogresolve

import (
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// TestAuthoredToManifest_CatalogHubBlocks asserts SC6: authored integrations /
// links / docs / extensions carry to the resolved manifest verbatim, and an
// unknown x-<vendor> extension block is preserved (round-trip, data-model §8).
func TestAuthoredToManifest_CatalogHubBlocks(t *testing.T) {
	am := AuthoredManifest{
		SourceFile: "apps/api/component.yaml",
		Component: catalogmodel.ComponentYAML{
			Metadata: catalogmodel.ComponentYAMLMetadata{Name: "api"},
			Spec: catalogmodel.ComponentYAMLSpec{
				Type: "worker",
				Integrations: map[string]any{
					"datadog": map[string]any{"service": "api", "team": "edge"},
				},
				Links: []catalogmodel.ComponentYAMLLink{{Title: "Dash", URL: "https://x", Icon: "dashboard"}},
				Docs:  &catalogmodel.ComponentYAMLDocs{TechDocs: "docs/", Runbooks: []string{"docs/run.md"}},
				Extensions: map[string]any{
					"x-acme": map[string]any{"tier": "gold"}, // unknown vendor block
				},
			},
		},
	}
	cm := authoredToManifest(am, "default", "orun")

	if cm.Integrations["datadog"] == nil {
		t.Errorf("integrations not carried: %v", cm.Integrations)
	}
	if len(cm.Links) != 1 || cm.Links[0].Title != "Dash" || cm.Links[0].Icon != "dashboard" {
		t.Errorf("links = %+v", cm.Links)
	}
	if cm.Docs == nil || cm.Docs.TechDocs != "docs/" || len(cm.Docs.Runbooks) != 1 {
		t.Errorf("docs = %+v", cm.Docs)
	}
	// The unknown x-<vendor> extension is preserved verbatim.
	x, ok := cm.Extensions["x-acme"].(map[string]any)
	if !ok || x["tier"] != "gold" {
		t.Errorf("extension x-acme not preserved: %v", cm.Extensions)
	}
}
