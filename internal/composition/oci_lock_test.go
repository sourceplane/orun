package composition

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

// A source the lockfile pins is loaded at its locked digest from the cache,
// without resolving the tag against a registry (specs/orun-sync-efficiency
// F12): the ref here points nowhere reachable.
func TestResolveOCISource_PinnedByTheLockfileNeedsNoRegistry(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	digest := "sha256:" + "ab12" + "0000000000000000000000000000000000000000000000000000000000"
	cacheDir := filepath.Join(home, ".orun", "cache", "compositions", digest[len("sha256:"):])
	writeCompositionPackage(t, cacheDir, "pinned", map[string]packageJob{
		"service": {defaultJob: "build", runsOn: "ubuntu-latest", stepRun: "echo pinned"},
	})

	baseDir := t.TempDir()
	lock := "apiVersion: orun.io/v1\nkind: CompositionLock\nsources:\n  - name: platform\n    kind: oci\n    ref: oci://registry.invalid/acme/platform:v1\n    resolvedDigest: " + digest + "\n"
	if err := os.MkdirAll(filepath.Join(baseDir, ".orun"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(baseDir, ".orun", "compositions.lock.yaml"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}

	source := model.CompositionSource{Name: "platform", Kind: "oci", Ref: "oci://registry.invalid/acme/platform:v1"}
	if got := lockedOCIDigest(baseDir, source); got != digest {
		t.Fatalf("locked digest = %q, want %q", got, digest)
	}
	pkg, err := resolveSourcePackage(baseDir, source, 0)
	if err != nil {
		t.Fatalf("a pinned, cached source must not need the registry: %v", err)
	}
	if pkg.manifest.Metadata.Name != "pinned" || pkg.resolvedRoot != cacheDir {
		t.Fatalf("loaded %q from %s", pkg.manifest.Metadata.Name, pkg.resolvedRoot)
	}

	// A different ref, or a source the lock does not name, is not pinned.
	if got := lockedOCIDigest(baseDir, model.CompositionSource{Name: "platform", Kind: "oci", Ref: "oci://registry.invalid/acme/platform:v2"}); got != "" {
		t.Fatalf("a moved ref must not be pinned: %q", got)
	}
	if got := lockedOCIDigest(baseDir, model.CompositionSource{Name: "other", Kind: "oci", Ref: "oci://registry.invalid/acme/platform:v1"}); got != "" {
		t.Fatalf("an unlisted source must not be pinned: %q", got)
	}
	if got := lockedOCIDigest(t.TempDir(), source); got != "" {
		t.Fatalf("no lockfile, no pin: %q", got)
	}
	// An explicit digest that disagrees with the lock keeps the resolving path.
	if _, err := resolveSourcePackage(baseDir, model.CompositionSource{Name: "platform", Kind: "oci", Ref: source.Ref, Digest: "sha256:" + "ff" + "00000000000000000000000000000000000000000000000000000000000000"}, 0); err == nil {
		t.Fatal("a conflicting explicit digest must resolve, and fail against an unreachable registry")
	}
}

func TestRefAtDigest(t *testing.T) {
	d := "sha256:" + "ab" + "00000000000000000000000000000000000000000000000000000000000000"
	for _, in := range []string{"ghcr.io/acme/platform:v1", "ghcr.io/acme/platform@" + d, "oci://ghcr.io/acme/platform:v1", "localhost:5000/acme/platform:v1"} {
		got := refAtDigest(in, d)
		if filepath.Ext(got) == "" && got != stripRefTagOrDigest(in)+"@"+d {
			t.Fatalf("%s → %s", in, got)
		}
	}
}
