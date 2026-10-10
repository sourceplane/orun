package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeLockTestWorkspace(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	files := map[string]string{
		"intent.yaml": `apiVersion: sourceplane.io/v1
kind: Intent
metadata:
  name: lock-test
compositions:
  sources:
    # the platform compositions
    - name: platform
      kind: dir
      path: ./compositions
components: []
`,
		"compositions/orun.yaml": `apiVersion: sourceplane.io/v1alpha1
kind: CompositionPackage
metadata:
  name: platform
spec:
  version: 1.0.0
  exports:
    - composition: helm
      path: compositions/helm.yaml
`,
		"compositions/compositions/helm.yaml": `apiVersion: sourceplane.io/v1alpha1
kind: Composition
metadata:
  name: helm
spec:
  type: helm
  defaultJob: deploy
  parameterSchema:
    type: object
  jobs:
    - name: deploy
      runsOn: ubuntu-latest
      steps:
        - name: deploy
          run: echo deploy
`,
	}
	for name, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	prevIntent, prevConfig := intentFile, configDir
	intentFile, configDir = filepath.Join(dir, "intent.yaml"), ""
	t.Cleanup(func() { intentFile, configDir = prevIntent, prevConfig })
	return dir
}

func TestCompositionsLockWriteIntentPinsEnforcedDigest(t *testing.T) {
	dir := writeLockTestWorkspace(t)

	captureStdout(t, func() error { return lockCompositions(true, false) })

	data, err := os.ReadFile(intentFile)
	if err != nil {
		t.Fatal(err)
	}
	intent := string(data)
	if !strings.Contains(intent, "# the platform compositions") {
		t.Fatalf("comment lost:\n%s", intent)
	}
	if !strings.Contains(intent, "      path: ./compositions\n      digest: sha256:") {
		t.Fatalf("digest not pinned after path:\n%s", intent)
	}

	// Re-running is a no-op on the intent.
	captureStdout(t, func() error { return lockCompositions(true, false) })
	again, _ := os.ReadFile(intentFile)
	if string(again) != intent {
		t.Fatalf("second --write-intent rewrote the intent:\n%s", again)
	}

	// The pin is enforced at resolution.
	if err := os.WriteFile(filepath.Join(dir, "compositions", "drift.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := resolveAndCacheCompositionsRegistry(false); err == nil || !strings.Contains(err.Error(), "digest mismatch") {
		t.Fatalf("expected digest mismatch after drift, got %v", err)
	}
}

func TestCompositionsLockCheckDetectsDrift(t *testing.T) {
	dir := writeLockTestWorkspace(t)

	captureStdout(t, func() error { return lockCompositions(false, false) })
	out := captureStdout(t, func() error { return lockCompositions(false, true) })
	if !strings.Contains(out, "match the lock") {
		t.Fatalf("expected clean check, got:\n%s", out)
	}

	lockPath := filepath.Join(dir, ".orun", "compositions.lock.yaml")
	before, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "compositions", "drift.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := captureStdoutErr(t, func() error { return lockCompositions(false, true) }); err == nil || !strings.Contains(err.Error(), "drifted") {
		t.Fatalf("expected drift error, got %v", err)
	}

	after, err := os.ReadFile(lockPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != string(before) {
		t.Fatalf("--check must not rewrite the lock")
	}
}
