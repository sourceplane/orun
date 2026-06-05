package catalogresolve

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

func writeFile(t *testing.T, root, rel, body string) {
	t.Helper()
	abs := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(body), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
}

func manifest(key, name, path string) *catalogmodel.ComponentManifest {
	return &catalogmodel.ComponentManifest{
		Identity: catalogmodel.ComponentIdentity{ComponentKey: key, Name: name, SourceFile: path},
	}
}

func TestComputeFingerprints(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "apps/api/component.yaml", "name: api")
	writeFile(t, root, "apps/api/package.json", `{"name":"api"}`)
	writeFile(t, root, "apps/api/main.go", "package main") // not a candidate → excluded
	writeFile(t, root, "apps/api/infra.tf", "resource {}") // *.tf candidate
	writeFile(t, root, "libs/shared/component.yaml", "name: shared")

	manifests := []*catalogmodel.ComponentManifest{
		manifest("ns/repo/api", "api", "apps/api/component.yaml"),
		manifest("ns/repo/shared", "shared", "libs/shared/component.yaml"),
		manifest("ns/repo/nopath", "nopath", ""), // skipped
		nil,
	}
	fps := computeFingerprints(root, manifests, "sha256:gd")
	if len(fps) != 2 {
		t.Fatalf("got %d fingerprints, want 2", len(fps))
	}
	// Ordered by componentKey: api < shared.
	api := fps[0]
	if api.ComponentKey != "ns/repo/api" || api.Dir != "apps/api" {
		t.Fatalf("api fp = %+v", api)
	}
	if _, ok := api.Files["apps/api/component.yaml"]; !ok {
		t.Errorf("component.yaml missing from read-set: %v", api.Files)
	}
	if _, ok := api.Files["apps/api/package.json"]; !ok {
		t.Errorf("package.json missing: %v", api.Files)
	}
	if _, ok := api.Files["apps/api/infra.tf"]; !ok {
		t.Errorf("*.tf missing: %v", api.Files)
	}
	if _, ok := api.Files["apps/api/main.go"]; ok {
		t.Errorf("main.go must not be in the read-set: %v", api.Files)
	}
	if api.Subtree == "" || api.GlobalDigest != "sha256:gd" {
		t.Errorf("subtree/global = %q / %q", api.Subtree, api.GlobalDigest)
	}
}

func TestComputeFingerprints_Deterministic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "apps/api/component.yaml", "name: api")
	writeFile(t, root, "apps/api/package.json", `{"name":"api"}`)
	m := []*catalogmodel.ComponentManifest{manifest("ns/repo/api", "api", "apps/api/component.yaml")}

	a := computeFingerprints(root, m, "g")
	b := computeFingerprints(root, m, "g")
	if a[0].Subtree != b[0].Subtree {
		t.Fatalf("non-deterministic subtree: %q vs %q", a[0].Subtree, b[0].Subtree)
	}
}

func TestComputeFingerprints_ChangeSensitiveAndReversible(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "apps/api/component.yaml", "name: api")
	m := []*catalogmodel.ComponentManifest{manifest("ns/repo/api", "api", "apps/api/component.yaml")}

	clean := computeFingerprints(root, m, "g")[0].Subtree

	writeFile(t, root, "apps/api/component.yaml", "name: api-edited")
	dirty := computeFingerprints(root, m, "g")[0].Subtree
	if dirty == clean {
		t.Fatalf("subtree unchanged after edit: %q", clean)
	}

	writeFile(t, root, "apps/api/component.yaml", "name: api")
	back := computeFingerprints(root, m, "g")[0].Subtree
	if back != clean {
		t.Fatalf("clean→dirty→clean did not return to %q (got %q)", clean, back)
	}
}

func TestComputeFingerprints_GlobalDigestFlipsSubtree(t *testing.T) {
	root := t.TempDir()
	writeFile(t, root, "apps/api/component.yaml", "name: api")
	m := []*catalogmodel.ComponentManifest{manifest("ns/repo/api", "api", "apps/api/component.yaml")}
	if computeFingerprints(root, m, "g1")[0].Subtree == computeFingerprints(root, m, "g2")[0].Subtree {
		t.Fatalf("subtree must fold in the global digest")
	}
}

func TestComputeFingerprints_MissingDir(t *testing.T) {
	root := t.TempDir()
	// Manifest points at a dir that does not exist on disk.
	m := []*catalogmodel.ComponentManifest{manifest("ns/repo/gone", "gone", "apps/gone/component.yaml")}
	fps := computeFingerprints(root, m, "g")
	if len(fps) != 1 || fps[0].Files != nil || fps[0].Subtree == "" {
		t.Fatalf("missing-dir fp = %+v", fps)
	}
}

func TestIsFingerprintCandidate(t *testing.T) {
	for name, want := range map[string]bool{
		"component.yaml":    true,
		"component.yml":     true,
		"package.json":      true,
		"pnpm-lock.yaml":    true,
		"Dockerfile":        true,
		"Chart.yaml":        true,
		"README.md":         true,
		"main.tf":           true,
		"terraform.tf.json": true,
		"main.go":           false,
		"random.txt":        false,
	} {
		if got := isFingerprintCandidate(name); got != want {
			t.Errorf("isFingerprintCandidate(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestComputeGlobalDigest(t *testing.T) {
	root := t.TempDir()
	intent := filepath.Join(root, "intent.yaml")
	if d := computeGlobalDigest(intent); d != "" {
		t.Errorf("absent intent digest = %q, want empty", d)
	}
	if err := os.WriteFile(intent, []byte("catalog: {}"), 0o644); err != nil {
		t.Fatalf("write intent: %v", err)
	}
	if d := computeGlobalDigest(intent); d == "" {
		t.Errorf("present intent digest empty")
	}
	if computeGlobalDigest("") != "" {
		t.Errorf("empty path digest should be empty")
	}
}
