package catalogresolve

import (
	"context"
	"reflect"
	"testing"
)

// TestResolve_ChangeInputsCarriedToManifest: spec.change.inputs from a
// component.yaml and change.inputs from an inline intent component land
// verbatim on the resolved manifest (the catalog snapshot the --changed engine
// reads), alongside watches; a component without inputs keeps a nil slice (and
// a nil change block when it has no watches either) so its hash is unchanged.
func TestResolve_ChangeInputsCarriedToManifest(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/intent.yaml",
		"catalog:\n  namespace: ns\n"+
			"components:\n"+
			"  - name: lint\n    type: library\n    path: packages/lint\n    change:\n      inputs:\n        - tooling/eslint/**\n")
	mustWrite(t, root+"/apps/web/component.yaml",
		"apiVersion: orun.io/v1alpha1\nkind: Component\nmetadata:\n  name: web\nspec:\n  type: app\n"+
			"  change:\n    watches: [env]\n    inputs:\n      - pnpm-lock.yaml\n      - turbo.json\n")
	mustWrite(t, root+"/apps/api/component.yaml",
		"apiVersion: orun.io/v1alpha1\nkind: Component\nmetadata:\n  name: api\nspec:\n  type: app\n")

	rc, issues, err := Resolve(context.Background(), Options{WorkspaceRoot: root, Repo: "r"})
	if err != nil {
		t.Fatalf("Resolve: %v (issues %v)", err, issues)
	}
	for _, i := range issues {
		if i.Code == "component.field.unknown" {
			t.Errorf("unexpected unknown-field issue: %+v", i)
		}
	}
	for _, m := range rc.Manifests {
		switch m.Identity.Name {
		case "web":
			if m.Spec.Change == nil ||
				!reflect.DeepEqual(m.Spec.Change.Inputs, []string{"pnpm-lock.yaml", "turbo.json"}) ||
				!reflect.DeepEqual(m.Spec.Change.Watches, []string{"env"}) {
				t.Errorf("web change = %+v", m.Spec.Change)
			}
		case "lint":
			if m.Spec.Change == nil || !reflect.DeepEqual(m.Spec.Change.Inputs, []string{"tooling/eslint/**"}) || m.Spec.Change.Watches != nil {
				t.Errorf("lint change = %+v", m.Spec.Change)
			}
		case "api":
			if m.Spec.Change != nil {
				t.Errorf("api change = %+v, want nil", m.Spec.Change)
			}
		}
	}
}

// TestResolve_InvalidChangeInputsRejected: a malformed, absolute or escaping
// glob is a validation error in default (non-strict) mode, so the catalog
// fails closed.
func TestResolve_InvalidChangeInputsRejected(t *testing.T) {
	for _, bad := range []string{"/etc/passwd", "../other/**", "tooling/[eslint", "./turbo.json"} {
		t.Run(bad, func(t *testing.T) {
			root := t.TempDir()
			mustWrite(t, root+"/intent.yaml", "catalog:\n  namespace: ns\n")
			mustWrite(t, root+"/apps/web/component.yaml",
				"apiVersion: orun.io/v1alpha1\nkind: Component\nmetadata:\n  name: web\nspec:\n  type: app\n"+
					"  change:\n    inputs:\n      - turbo.json\n      - \""+bad+"\"\n")
			_, issues, err := Resolve(context.Background(), Options{WorkspaceRoot: root, Repo: "r"})
			if err == nil {
				t.Fatalf("Resolve accepted invalid input glob %q", bad)
			}
			var found bool
			for _, i := range issues {
				if i.Code == "component.spec.change.inputs.invalid" && i.Pointer == "/spec/change/inputs/1" && i.Severity == SeverityError {
					found = true
				}
			}
			if !found {
				t.Errorf("missing component.spec.change.inputs.invalid issue at /spec/change/inputs/1: %+v", issues)
			}
		})
	}
}
