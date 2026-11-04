package catalogresolve

import (
	"context"
	"testing"
)

// The catalog schema knows spec.preview (saas-parallel-baselining PB-11): a
// well-formed block raises no component.field.unknown warning, a typo inside
// it does, and a bad port or path is a warning, never a failed catalog.
func TestResolve_SpecPreviewKnown(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/intent.yaml", "catalog:\n  namespace: ns\n")
	mustWrite(t, root+"/apps/web/component.yaml",
		"apiVersion: orun.io/v1alpha1\nkind: Component\nmetadata:\n  name: web\nspec:\n  type: app\n"+
			"  preview:\n    run: [pnpm, dev, --port, \"4173\"]\n    port: 4173\n    needs: [api]\n    path: /app\n    env: {MODE: dev}\n")
	mustWrite(t, root+"/apps/api/component.yaml",
		"apiVersion: orun.io/v1alpha1\nkind: Component\nmetadata:\n  name: api\nspec:\n  type: app\n"+
			"  preview:\n    run: [node, server.js]\n    port: 80\n    path: api\n    prot: 1\n")

	_, issues, err := Resolve(context.Background(), Options{WorkspaceRoot: root, Repo: "r"})
	if err != nil {
		t.Fatalf("Resolve: %v (issues %v)", err, issues)
	}
	var unknown []string
	invalid := map[string]int{}
	for _, i := range issues {
		switch i.Code {
		case "component.field.unknown":
			unknown = append(unknown, i.File+i.Pointer)
		case "component.spec.preview.invalid":
			if i.Severity != SeverityWarning {
				t.Errorf("preview issue severity = %v, want warning", i.Severity)
			}
			invalid[i.File]++
		}
	}
	if len(unknown) != 1 || unknown[0] != "apps/api/component.yaml/spec/preview/prot" {
		t.Errorf("unknown-field issues = %v, want only the api typo", unknown)
	}
	if invalid["apps/api/component.yaml"] != 2 || invalid["apps/web/component.yaml"] != 0 {
		t.Errorf("preview issues = %v, want two (port, path) on api only", invalid)
	}
}
