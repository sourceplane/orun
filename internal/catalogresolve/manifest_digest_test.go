package catalogresolve

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// TestManifestSetDigest asserts the digest witnesses every change that alters
// the discovered manifest set (add, edit, nested .yml, remove), ignores files
// discovery ignores, and is empty for a workspace with no inputs.
func TestManifestSetDigest(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	dir := t.TempDir()
	write := func(rel, body string) {
		t.Helper()
		p := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	digest := func() string {
		t.Helper()
		d, err := ManifestSetDigest(ctx, dir)
		if err != nil {
			t.Fatalf("ManifestSetDigest: %v", err)
		}
		return d
	}

	if d := digest(); d != "" {
		t.Fatalf("empty workspace should yield \"\", got %q", d)
	}
	write("intent.yaml", "apiVersion: orun.io/v1alpha1\nkind: Intent\nmetadata:\n  name: demo\ncatalog:\n  discovery:\n    exclude: [fixtures]\n")
	d0 := digest()
	if d0 == "" {
		t.Fatal("intent.yaml present but digest empty")
	}
	write("svc-a/component.yaml", "metadata:\n  name: svc-a\n")
	d1 := digest()
	if d1 == d0 {
		t.Error("adding a component should move the digest")
	}
	if digest() != d1 {
		t.Error("digest not deterministic")
	}
	write("svc-a/component.yaml", "metadata:\n  name: svc-a\nspec:\n  dependsOn:\n    - component: svc-b\n      input: true\n")
	d2 := digest()
	if d2 == d1 {
		t.Error("editing a manifest should move the digest")
	}
	write("svc-a/nested/component.yml", "metadata:\n  name: nested\n")
	d3 := digest()
	if d3 == d2 {
		t.Error("a nested component.yml should move the digest")
	}
	// Files discovery never reads leave the digest alone.
	write("svc-a/main.go", "package main\n")
	write("fixtures/x/component.yaml", "metadata:\n  name: fixture\n")
	write("node_modules/y/component.yaml", "metadata:\n  name: vendored\n")
	if digest() != d3 {
		t.Error("non-manifest files and excluded dirs must not move the digest")
	}
	if err := os.Remove(filepath.Join(dir, "svc-a", "nested", "component.yml")); err != nil {
		t.Fatal(err)
	}
	if digest() != d2 {
		t.Error("removing the nested manifest should restore the earlier digest")
	}
}
