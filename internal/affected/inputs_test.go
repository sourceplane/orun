package affected

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/objcatalog"
)

// inputsCatalog models a turborepo-style workspace (OR2): root files
// (pnpm-lock.yaml, turbo.json, tooling/eslint/**) are owned by no component.
//
//	web     (apps/web)      inputs: pnpm-lock.yaml, turbo.json
//	lint    (packages/lint) inputs: tooling/eslint/**
//	console (apps/console)  dependsOn lint input:true
//	api     (apps/api)      no inputs, no input edges
func inputsCatalog() *objcatalog.CatalogView {
	return &objcatalog.CatalogView{
		Components: []objcatalog.CatalogComponentView{
			{ComponentKey: "ns/repo/web", Name: "web", Spec: map[string]any{
				"inputs": []any{"pnpm-lock.yaml", "turbo.json"},
			}},
			{ComponentKey: "ns/repo/lint", Name: "lint", Spec: map[string]any{
				"inputs": []any{"tooling/eslint/**"},
			}},
			{ComponentKey: "ns/repo/console", Name: "console"},
			{ComponentKey: "ns/repo/api", Name: "api"},
		},
		Relations: []objcatalog.RelationEdgeView{
			{From: "ns/repo/console", FromKind: "Component", Type: "dependsOn", To: "ns/repo/lint", ToKind: "Component", Input: true},
		},
		Ownership: &objcatalog.OwnershipView{
			SchemaVersion: 1,
			Components: map[string]string{
				"apps/web":      "ns/repo/web",
				"packages/lint": "ns/repo/lint",
				"apps/console":  "ns/repo/console",
				"apps/api":      "ns/repo/api",
			},
			GlobalPaths:         []string{"intent.yaml"},
			StructuralFilenames: []string{"component.yaml"},
			IgnoreDirs:          []string{".git", "node_modules"},
		},
	}
}

func TestDetect_InputGlob_RootFileSelectsComponent(t *testing.T) {
	r := detect(t, inputsCatalog(), IntentImpactWatch, fakeSource{files: []string{"pnpm-lock.yaml"}})
	eq(t, r.DirectlyChanged, []string{"ns/repo/web"}, "DirectlyChanged")
	eq(t, r.Selection, []string{"ns/repo/web"}, "Selection")
	var found bool
	for _, e := range r.Explain {
		if e.Component == "ns/repo/web" && strings.Contains(e.Reason, "input glob pnpm-lock.yaml matched: pnpm-lock.yaml") {
			found = true
		}
	}
	if !found {
		t.Errorf("missing input-glob explain entry: %v", r.Explain)
	}
}

func TestDetect_InputGlob_NonMatchingFileSelectsNothing(t *testing.T) {
	for _, f := range []string{"README.md", "tooling/prettier/index.js", "apps/web-legacy/pnpm-lock.yaml"} {
		r := detect(t, inputsCatalog(), IntentImpactWatch, fakeSource{files: []string{f}})
		eq(t, r.DirectlyChanged, nil, "DirectlyChanged for "+f)
		eq(t, r.Selection, nil, "Selection for "+f)
	}
}

func TestDetect_InputGlob_DoubleStarMatchesNested(t *testing.T) {
	r := detect(t, inputsCatalog(), IntentImpactWatch, fakeSource{files: []string{"tooling/eslint/rules/deep/no-foo.js"}})
	// lint via its glob; console via its input:true edge onto lint.
	eq(t, r.DirectlyChanged, []string{"ns/repo/console", "ns/repo/lint"}, "DirectlyChanged")
}

func TestDetect_InputGlob_PropagatesOverInputEdges(t *testing.T) {
	r := detect(t, inputsCatalog(), IntentImpactWatch, fakeSource{files: []string{"tooling/eslint/index.js"}})
	eq(t, r.Selection, []string{"ns/repo/console", "ns/repo/lint"}, "Selection")
	var rescoped bool
	for _, e := range r.Explain {
		if e.Component == "ns/repo/console" && e.Reason == "build input changed (dependsOn input:true)" {
			rescoped = true
		}
	}
	if !rescoped {
		t.Errorf("console not rescoped via input edge: %v", r.Explain)
	}
}

func TestDetect_InputGlob_OwnershipUnchanged(t *testing.T) {
	// An owned file still selects its owner only; inputs add, never replace.
	r := detect(t, inputsCatalog(), IntentImpactWatch, fakeSource{files: []string{"apps/api/main.go"}})
	eq(t, r.DirectlyChanged, []string{"ns/repo/api"}, "DirectlyChanged")
	r = detect(t, inputsCatalog(), IntentImpactWatch, fakeSource{files: []string{"apps/web/page.tsx", "turbo.json"}})
	eq(t, r.DirectlyChanged, []string{"ns/repo/web"}, "DirectlyChanged")
}

func TestDetect_InputGlob_NoInputsIsUnchanged(t *testing.T) {
	// The pre-OR2 catalog (no inputs anywhere): a root file selects nothing,
	// exactly as before.
	for _, f := range []string{"pnpm-lock.yaml", "turbo.json", "tooling/eslint/index.js"} {
		r := detect(t, sampleCatalogRelations(), IntentImpactWatch, fakeSource{files: []string{f}})
		eq(t, r.DirectlyChanged, nil, "DirectlyChanged for "+f)
	}
}

func TestDetect_InputGlob_InvalidStoredPatternIgnored(t *testing.T) {
	c := inputsCatalog()
	c.Components[0].Spec = map[string]any{"inputs": []any{"../pnpm-lock.yaml", "/turbo.json", 7}}
	r := detect(t, c, IntentImpactWatch, fakeSource{files: []string{"pnpm-lock.yaml", "turbo.json"}})
	eq(t, r.DirectlyChanged, nil, "DirectlyChanged")
}
