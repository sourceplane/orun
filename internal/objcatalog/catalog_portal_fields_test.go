package objcatalog

import (
	"reflect"
	"testing"

	"github.com/sourceplane/orun/internal/nodes"
)

// TestComponentView_PortalFields covers the git-authored portal fields
// (orun-catalog-portal CPF): description / language / tags projected from the
// component source with the documented precedence and tolerant defaults.
func TestComponentView_PortalFields(t *testing.T) {
	t.Run("reads from spec with precedence over metadata and docs", func(t *testing.T) {
		m := nodes.ComponentManifest{
			Kind:     nodes.KindComponentManifest,
			Identity: nodes.ComponentIdentity{ComponentKey: "x/y/api", Name: "api"},
			Spec: map[string]any{
				"description": "spec wins",
				"language":    "Go",
				"tags":        []any{"public", "tier1"},
			},
			Metadata: map[string]any{
				"description": "metadata loses",
				"language":    "Rust",
				"tags":        []any{"ignored"},
			},
			Docs: map[string]any{"summary": "docs loses"},
		}
		v := componentView(m)
		if v.Description != "spec wins" {
			t.Errorf("Description = %q, want spec value", v.Description)
		}
		if v.Language != "Go" {
			t.Errorf("Language = %q, want Go", v.Language)
		}
		if !reflect.DeepEqual(v.Tags, []string{"public", "tier1"}) {
			t.Errorf("Tags = %v, want [public tier1]", v.Tags)
		}
	})

	t.Run("falls back metadata then docs, and languages[]/spec tags", func(t *testing.T) {
		m := nodes.ComponentManifest{
			Kind:     nodes.KindComponentManifest,
			Identity: nodes.ComponentIdentity{ComponentKey: "x/y/b", Name: "b"},
			Spec: map[string]any{
				"languages": []any{"Python", "Bash"},
			},
			Metadata: map[string]any{
				"description": "from metadata",
				"tags":        []any{"from-meta"},
			},
		}
		v := componentView(m)
		if v.Description != "from metadata" {
			t.Errorf("Description = %q, want metadata fallback", v.Description)
		}
		if v.Language != "Python" {
			t.Errorf("Language = %q, want first of spec.languages", v.Language)
		}
		if !reflect.DeepEqual(v.Tags, []string{"from-meta"}) {
			t.Errorf("Tags = %v, want [from-meta]", v.Tags)
		}

		// docs.summary is the final description fallback.
		m2 := nodes.ComponentManifest{
			Kind:     nodes.KindComponentManifest,
			Identity: nodes.ComponentIdentity{ComponentKey: "x/y/c", Name: "c"},
			Docs:     map[string]any{"summary": "from docs"},
		}
		if got := componentView(m2).Description; got != "from docs" {
			t.Errorf("Description = %q, want docs fallback", got)
		}
	})

	t.Run("defaults empty when the source declares none", func(t *testing.T) {
		m := nodes.ComponentManifest{
			Kind:     nodes.KindComponentManifest,
			Identity: nodes.ComponentIdentity{ComponentKey: "x/y/bare", Name: "bare"},
		}
		v := componentView(m)
		if v.Description != "" || v.Language != "" || v.Tags != nil {
			t.Errorf("bare component should project empty portal fields: %+v", v)
		}
	})
}
