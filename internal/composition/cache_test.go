package composition

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/fslock"
	"github.com/sourceplane/orun/internal/model"
)

// A dir source is loaded where it is: the digest still validates it and
// feeds the lock, but nothing is copied into ~/.orun/cache/compositions
// (specs/orun-cache-efficiency F5: one full copy per edit, never removed).
func TestResolveDirectorySource_LoadsThePackageInPlace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	rootDir := t.TempDir()
	packageDir := filepath.Join(rootDir, "core")
	writeCompositionPackage(t, packageDir, "core", map[string]packageJob{
		"helm": {defaultJob: "deploy", runsOn: "ubuntu-latest", stepRun: "echo in place"},
	})

	source := model.CompositionSource{Name: "core", Kind: "dir", Path: "./core"}
	pkg, err := resolveSourcePackage(rootDir, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	if pkg.resolvedRoot != packageDir {
		t.Fatalf("the package is read from its own directory, got %q", pkg.resolvedRoot)
	}
	if pkg.resolvedMetadata.ResolvedDigest == "" {
		t.Fatal("the digest still feeds the lock")
	}
	if entries, err := os.ReadDir(filepath.Join(home, ".orun", "cache", "compositions")); err == nil && len(entries) > 0 {
		t.Fatalf("nothing is copied into the composition cache: %v", entries)
	}

	// An explicit digest that disagrees still fails.
	source.Digest = "sha256:0000"
	if _, err := resolveSourcePackage(rootDir, source, 0); err == nil {
		t.Fatal("a digest mismatch is reported")
	}
}

func TestFillCacheEntry_NeverReplacesAPassingEntry(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "abc")
	writeCompositionPackage(t, cacheDir, "live", map[string]packageJob{
		"helm": {defaultJob: "deploy", runsOn: "ubuntu-latest", stepRun: "echo live"},
	})
	fills := 0
	got, err := fillCacheEntry(root, cacheDir, func(string) error { fills++; return nil })
	if err != nil || got != cacheDir || fills != 0 {
		t.Fatalf("a passing entry is returned without a fill: %v %q fills=%d", err, got, fills)
	}
	if _, err := os.Stat(cacheDir + ".last-used"); err != nil {
		t.Fatal("a hit is stamped as used")
	}

	// A leftover that fails the hit test is replaced.
	leftover := filepath.Join(root, "def")
	if err := os.MkdirAll(filepath.Join(leftover, "half"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err = fillCacheEntry(root, leftover, func(tempDir string) error {
		fills++
		return os.WriteFile(filepath.Join(tempDir, "stack.yaml"), []byte("filled: true\n"), 0o644)
	})
	if err != nil || got != leftover || fills != 1 {
		t.Fatalf("a leftover is filled: %v %q fills=%d", err, got, fills)
	}
	if _, err := os.Stat(filepath.Join(leftover, "half")); !os.IsNotExist(err) {
		t.Fatal("the leftover's contents are gone")
	}
	if !cachedEntryHit(leftover) {
		t.Fatal("the filled entry passes the hit test")
	}
	if entries, _ := os.ReadDir(root); len(entries) != 5 { // abc, abc.last-used, def, def.last-used, def.lock
		t.Fatalf("no temp directory is left behind: %v", names(entries))
	}
}

func names(entries []os.DirEntry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// Two processes filling one digest: the second waits for the first's lock
// and then finds the entry filled, so it fills nothing and deletes nothing.
func TestFillCacheEntry_WaitsForAnotherProcessAndKeepsItsFill(t *testing.T) {
	root := t.TempDir()
	cacheDir := filepath.Join(root, "shared")
	holder, ok, err := fslock.TryAcquire(cacheDir + ".lock")
	if err != nil || !ok {
		t.Fatal(err)
	}

	fills := 0
	done := make(chan error, 1)
	go func() {
		_, err := fillCacheEntry(root, cacheDir, func(tempDir string) error {
			fills++
			return os.WriteFile(filepath.Join(tempDir, "stack.yaml"), []byte("second\n"), 0o644)
		})
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("the fill ran while another process held the lock: %v", err)
	case <-time.After(150 * time.Millisecond):
	}

	// The holder fills the entry, then releases.
	if err := os.MkdirAll(cacheDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(cacheDir, "stack.yaml"), []byte("first\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_ = holder.Release()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the waiter did not proceed after the lock was released")
	}
	if fills != 0 {
		t.Fatalf("the waiter found the entry filled and filled nothing: %d", fills)
	}
	if data, _ := os.ReadFile(filepath.Join(cacheDir, "stack.yaml")); string(data) != "first\n" {
		t.Fatalf("the first process's entry stays: %q", data)
	}
}

// An archive source is extracted once; the second resolve of the same
// digest is a hit.
func TestResolveArchiveSource_ExtractsOncePerDigest(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	rootDir := t.TempDir()
	packageDir := filepath.Join(rootDir, "package")
	archivePath := filepath.Join(rootDir, "dist", "core.tgz")
	writeCompositionPackage(t, packageDir, "core", map[string]packageJob{
		"helm": {defaultJob: "deploy", runsOn: "ubuntu-latest", stepRun: "echo archive"},
	})
	writeTarGz(t, packageDir, archivePath)
	source := model.CompositionSource{Name: "core", Kind: "archive", Path: "./dist/core.tgz"}

	first, err := resolveSourcePackage(rootDir, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(first.resolvedRoot, "extracted-at-first-resolve")
	if err := os.WriteFile(marker, []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := resolveSourcePackage(rootDir, source, 0)
	if err != nil {
		t.Fatal(err)
	}
	if second.resolvedRoot != first.resolvedRoot {
		t.Fatalf("one entry per digest: %q vs %q", first.resolvedRoot, second.resolvedRoot)
	}
	if _, err := os.Stat(marker); err != nil {
		t.Fatal("the second resolve is a hit: the entry was not rebuilt")
	}
}
