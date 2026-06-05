package affected

import (
	"context"
	"reflect"
	"testing"

	"github.com/sourceplane/orun/internal/objcatalog"
)

// fakeSource is a ChangeSource returning canned data.
type fakeSource struct {
	files  []string
	intent IntentChange
	err    error
}

func (f fakeSource) ChangedPaths(context.Context) ([]string, IntentChange, error) {
	return f.files, f.intent, f.err
}

// sampleCatalog builds a three-component catalog:
//
//	api (apps/api) depends_on shared (libs/shared)
//	web (apps/web) depends_on shared
//
// shared is a leaf. api watches the "env" intent section.
func sampleCatalog() *objcatalog.CatalogView {
	return &objcatalog.CatalogView{
		Components: []objcatalog.CatalogComponentView{
			{ComponentKey: "ns/repo/api", Name: "api", Spec: map[string]any{
				"change": map[string]any{"watches": []any{"env"}},
			}},
			{ComponentKey: "ns/repo/web", Name: "web"},
			{ComponentKey: "ns/repo/shared", Name: "shared"},
		},
		Graph: map[string]objcatalog.GraphView{
			"dependencies": {Edges: []objcatalog.GraphEdgeView{
				{From: "ns/repo/api", To: "ns/repo/shared", Type: "depends_on"},
				{From: "ns/repo/web", To: "ns/repo/shared", Type: "depends_on"},
			}},
		},
		Ownership: &objcatalog.OwnershipView{
			SchemaVersion: 1,
			Components: map[string]string{
				"apps/api":    "ns/repo/api",
				"apps/web":    "ns/repo/web",
				"libs/shared": "ns/repo/shared",
			},
			GlobalPaths:         []string{"intent.yaml"},
			StructuralFilenames: []string{"component.yaml"},
			IgnoreDirs:          []string{".git", "node_modules"},
		},
	}
}

func detect(t *testing.T, cat *objcatalog.CatalogView, policy IntentImpact, src ChangeSource) Result {
	t.Helper()
	r, err := NewDetector(cat, policy).Detect(context.Background(), src)
	if err != nil {
		t.Fatalf("Detect: %v", err)
	}
	return r
}

func eq(t *testing.T, got, want []string, label string) {
	t.Helper()
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("%s = %v, want %v", label, got, want)
	}
}

func TestDetect_ComponentChangePropagatesToDependents(t *testing.T) {
	// A change to shared's input → shared directly changed; api & web are
	// dependents → affected.
	r := detect(t, sampleCatalog(), IntentImpactWatch, fakeSource{files: []string{"libs/shared/main.go"}})
	eq(t, r.DirectlyChanged, []string{"ns/repo/shared"}, "DirectlyChanged")
	eq(t, r.Dependents, []string{"ns/repo/api", "ns/repo/web"}, "Dependents")
	eq(t, r.Affected, []string{"ns/repo/api", "ns/repo/shared", "ns/repo/web"}, "Affected")
	eq(t, r.Dependencies, nil, "Dependencies")
	if r.Confidence != ConfidenceHigh || r.NeedsFullResolve {
		t.Errorf("confidence/needsFull = %s / %v", r.Confidence, r.NeedsFullResolve)
	}
	if r.IntentMode != IntentModeNone {
		t.Errorf("intent mode = %s", r.IntentMode)
	}
}

func TestDetect_ComponentChangeHasForwardDeps(t *testing.T) {
	// A change to api's input → api directly changed; its forward dependency is
	// shared; nothing depends on api so no dependents.
	r := detect(t, sampleCatalog(), IntentImpactWatch, fakeSource{files: []string{"apps/api/handler.go"}})
	eq(t, r.DirectlyChanged, []string{"ns/repo/api"}, "DirectlyChanged")
	eq(t, r.Dependencies, []string{"ns/repo/shared"}, "Dependencies")
	eq(t, r.Dependents, nil, "Dependents")
	eq(t, r.Affected, []string{"ns/repo/api"}, "Affected")
}

func TestDetect_IgnoredAndUnownedPaths(t *testing.T) {
	r := detect(t, sampleCatalog(), IntentImpactWatch, fakeSource{files: []string{
		"node_modules/x/index.js", // ignore dir
		"docs/readme.md",          // unowned → ignore
		".git/config",             // ignore dir
	}})
	eq(t, r.DirectlyChanged, nil, "DirectlyChanged")
	eq(t, r.Affected, nil, "Affected")
}

func TestDetect_StructuralChange(t *testing.T) {
	// A component.yaml edit is structural: low confidence + full resolve, and the
	// owning component is marked changed.
	r := detect(t, sampleCatalog(), IntentImpactWatch, fakeSource{files: []string{"apps/api/component.yaml"}})
	if r.Confidence != ConfidenceLow || !r.NeedsFullResolve {
		t.Fatalf("structural change must lower confidence: %s / %v", r.Confidence, r.NeedsFullResolve)
	}
	eq(t, r.DirectlyChanged, []string{"ns/repo/api"}, "DirectlyChanged")
}

func TestDetect_IntentGlobal_PolicyAll(t *testing.T) {
	r := detect(t, sampleCatalog(), IntentImpactAll, fakeSource{
		intent: IntentChange{Changed: true,
			Base: []byte("catalog:\n  defaults:\n    a: 1\n"),
			Head: []byte("catalog:\n  defaults:\n    a: 2\n")},
	})
	if r.IntentMode != IntentModeGlobal || !r.NeedsFullResolve {
		t.Fatalf("intent mode/needsFull = %s / %v", r.IntentMode, r.NeedsFullResolve)
	}
	eq(t, r.DirectlyChanged, []string{"ns/repo/api", "ns/repo/shared", "ns/repo/web"}, "DirectlyChanged (all)")
}

func TestDetect_IntentGlobal_PolicyNone(t *testing.T) {
	r := detect(t, sampleCatalog(), IntentImpactNone, fakeSource{
		intent: IntentChange{Changed: true,
			Base: []byte("catalog:\n  defaults:\n    a: 1\n"),
			Head: []byte("catalog:\n  defaults:\n    a: 2\n")},
	})
	if r.IntentMode != IntentModeGlobal {
		t.Fatalf("intent mode = %s", r.IntentMode)
	}
	eq(t, r.DirectlyChanged, nil, "DirectlyChanged (none)")
}

func TestDetect_UndiffableIntentIsGlobal(t *testing.T) {
	// Changed intent with no base/head bytes (e.g. --files) → conservative global.
	r := detect(t, sampleCatalog(), IntentImpactAll, fakeSource{
		intent: IntentChange{Changed: true},
	})
	if r.IntentMode != IntentModeGlobal {
		t.Fatalf("undiffable intent should be global, got %s", r.IntentMode)
	}
	eq(t, r.DirectlyChanged, []string{"ns/repo/api", "ns/repo/shared", "ns/repo/web"}, "DirectlyChanged")
}

func TestDetect_IntentGlobal_PolicyWatch(t *testing.T) {
	// A change in the "env" section → only components watching "env" (api).
	r := detect(t, sampleCatalog(), IntentImpactWatch, fakeSource{
		intent: IntentChange{Changed: true,
			Base: []byte("env:\n  A: 1\n"),
			Head: []byte("env:\n  A: 2\n")},
	})
	if r.IntentMode != IntentModeGlobal {
		t.Fatalf("intent mode = %s", r.IntentMode)
	}
	// api is directly selected (watch hit); shared is its forward dep, web is a
	// co-dependent of shared but not itself changed.
	eq(t, r.DirectlyChanged, []string{"ns/repo/api"}, "DirectlyChanged (watch)")
	eq(t, r.Dependencies, []string{"ns/repo/shared"}, "Dependencies")
}

func TestDetect_IntentComponentsMode(t *testing.T) {
	// Only a component block changed → that component (by name → key); not global.
	r := detect(t, sampleCatalog(), IntentImpactWatch, fakeSource{
		intent: IntentChange{Changed: true,
			Base: []byte("components:\n  - name: web\n    domain: a\n"),
			Head: []byte("components:\n  - name: web\n    domain: b\n")},
	})
	if r.IntentMode != IntentModeComponents {
		t.Fatalf("intent mode = %s, want components", r.IntentMode)
	}
	eq(t, r.DirectlyChanged, []string{"ns/repo/web"}, "DirectlyChanged (components)")
	if r.NeedsFullResolve {
		t.Errorf("inline components change should not force full resolve")
	}
}

func TestDetect_IntentFormattingOnlyIsNone(t *testing.T) {
	r := detect(t, sampleCatalog(), IntentImpactWatch, fakeSource{
		intent: IntentChange{Changed: true,
			Base: []byte("components:\n  - name: web\n    domain: a\n"),
			Head: []byte("components:\n  - name: web\n    domain: a\n")},
	})
	if r.IntentMode != IntentModeNone {
		t.Fatalf("formatting-only intent should be none, got %s", r.IntentMode)
	}
	eq(t, r.DirectlyChanged, nil, "DirectlyChanged")
}

func TestDetect_SelectionFollowsIncludeAlwaysOnly(t *testing.T) {
	// api --include:always--> shared ; api --if-selected--> lib
	// A change to api selects api + shared (always) but NOT lib.
	cat := &objcatalog.CatalogView{
		Components: []objcatalog.CatalogComponentView{
			{ComponentKey: "ns/repo/api", Name: "api"},
			{ComponentKey: "ns/repo/shared", Name: "shared"},
			{ComponentKey: "ns/repo/lib", Name: "lib"},
		},
		Graph: map[string]objcatalog.GraphView{"dependencies": {Edges: []objcatalog.GraphEdgeView{
			{From: "ns/repo/api", To: "ns/repo/shared", Type: "depends_on", Include: "always"},
			{From: "ns/repo/api", To: "ns/repo/lib", Type: "depends_on"}, // if-selected (empty)
		}}},
		Ownership: &objcatalog.OwnershipView{SchemaVersion: 1,
			Components: map[string]string{"apps/api": "ns/repo/api"}},
	}
	r := detect(t, cat, IntentImpactWatch, fakeSource{files: []string{"apps/api/x.go"}})
	eq(t, r.DirectlyChanged, []string{"ns/repo/api"}, "DirectlyChanged")
	// Selection pulls in the include:always dep (shared) but not the if-selected one (lib).
	eq(t, r.Selection, []string{"ns/repo/api", "ns/repo/shared"}, "Selection")
	// Dependencies (the full forward closure, for display) still includes both.
	eq(t, r.Dependencies, []string{"ns/repo/lib", "ns/repo/shared"}, "Dependencies")
}

func TestDetect_SourceError(t *testing.T) {
	_, err := NewDetector(sampleCatalog(), IntentImpactWatch).Detect(context.Background(),
		fakeSource{err: context.Canceled})
	if err == nil {
		t.Fatalf("expected source error to propagate")
	}
}

func TestNewDetector_DefaultsPolicyToWatch(t *testing.T) {
	d := NewDetector(nil, "bogus")
	if d.policy != IntentImpactWatch {
		t.Errorf("policy = %q, want watch", d.policy)
	}
}

func TestDetect_NilCatalogIsSafe(t *testing.T) {
	r := detect(t, nil, IntentImpactWatch, fakeSource{files: []string{"apps/api/x.go"}})
	eq(t, r.DirectlyChanged, nil, "DirectlyChanged")
	eq(t, r.Affected, nil, "Affected")
}
