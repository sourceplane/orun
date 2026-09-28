package catalogresolve

import (
	"context"
	"reflect"
	"testing"
)

// TestResolve_InputsCarriedToManifest: spec.inputs from a component.yaml and
// from an inline intent component land verbatim on the resolved manifest (the
// catalog snapshot the --changed engine reads); a component without inputs
// keeps a nil slice so its manifest hash is unchanged.
func TestResolve_InputsCarriedToManifest(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/intent.yaml",
		"catalog:\n  namespace: ns\n"+
			"components:\n"+
			"  - name: lint\n    type: library\n    path: packages/lint\n    inputs:\n      - tooling/eslint/**\n")
	mustWrite(t, root+"/apps/web/component.yaml",
		"apiVersion: orun.io/v1alpha1\nkind: Component\nmetadata:\n  name: web\nspec:\n  type: app\n"+
			"  inputs:\n    - pnpm-lock.yaml\n    - turbo.json\n")
	mustWrite(t, root+"/apps/api/component.yaml",
		"apiVersion: orun.io/v1alpha1\nkind: Component\nmetadata:\n  name: api\nspec:\n  type: app\n")

	rc, issues, err := Resolve(context.Background(), Options{WorkspaceRoot: root, Repo: "r"})
	if err != nil {
		t.Fatalf("Resolve: %v (issues %v)", err, issues)
	}
	for _, i := range issues {
		if i.Code == "component.field.unknown" {
			t.Errorf("spec.inputs reported as unknown field: %+v", i)
		}
	}
	got := map[string][]string{}
	for _, m := range rc.Manifests {
		got[m.Identity.Name] = m.Spec.Inputs
	}
	want := map[string][]string{
		"web":  {"pnpm-lock.yaml", "turbo.json"},
		"lint": {"tooling/eslint/**"},
		"api":  nil,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("manifest inputs = %v, want %v", got, want)
	}
}

// TestResolve_InvalidInputsRejected: a malformed, absolute or escaping glob is
// a validation error in default (non-strict) mode, so the catalog fails closed.
func TestResolve_InvalidInputsRejected(t *testing.T) {
	for _, bad := range []string{"/etc/passwd", "../other/**", "tooling/[eslint", "./turbo.json"} {
		t.Run(bad, func(t *testing.T) {
			root := t.TempDir()
			mustWrite(t, root+"/intent.yaml", "catalog:\n  namespace: ns\n")
			mustWrite(t, root+"/apps/web/component.yaml",
				"apiVersion: orun.io/v1alpha1\nkind: Component\nmetadata:\n  name: web\nspec:\n  type: app\n"+
					"  inputs:\n    - turbo.json\n    - \""+bad+"\"\n")
			_, issues, err := Resolve(context.Background(), Options{WorkspaceRoot: root, Repo: "r"})
			if err == nil {
				t.Fatalf("Resolve accepted invalid input glob %q", bad)
			}
			var found bool
			for _, i := range issues {
				if i.Code == "component.spec.inputs.invalid" && i.Pointer == "/spec/inputs/1" && i.Severity == SeverityError {
					found = true
				}
			}
			if !found {
				t.Errorf("missing component.spec.inputs.invalid issue at /spec/inputs/1: %+v", issues)
			}
		})
	}
}

// TestResolve_LegacyInputsMappingStillLoads: before input globs, spec.inputs
// was the plan engine's old name for parameters (a mapping). That form must
// keep resolving — to no globs — and stay flagged as an uninterpreted field.
func TestResolve_LegacyInputsMappingStillLoads(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/intent.yaml", "catalog:\n  namespace: ns\n")
	mustWrite(t, root+"/apps/web/component.yaml",
		"apiVersion: orun.io/v1alpha1\nkind: Component\nmetadata:\n  name: web\nspec:\n  type: app\n"+
			"  inputs:\n    releaseName: web\n")
	rc, issues, err := Resolve(context.Background(), Options{WorkspaceRoot: root, Repo: "r"})
	if err != nil {
		t.Fatalf("Resolve: %v (issues %v)", err, issues)
	}
	if got := rc.Manifests[0].Spec.Inputs; got != nil {
		t.Errorf("legacy mapping produced globs: %v", got)
	}
	var flagged bool
	for _, i := range issues {
		if i.Code == "component.field.unknown" && i.Pointer == "/spec/inputs" {
			flagged = true
		}
	}
	if !flagged {
		t.Errorf("legacy spec.inputs mapping not flagged as uninterpreted: %+v", issues)
	}
}
