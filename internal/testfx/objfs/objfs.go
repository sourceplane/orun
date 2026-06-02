// Package objfs provides test helpers for the orun-object-model milestones.
//
// All helpers in this package are intended for use from *_test.go files only.
// They MUST NOT import any other internal/ package — objfs sits beneath every
// object-model package (objectstore, refstore, nodes, nodewriter, …) in the
// dependency graph and exists to give those packages a stable, dependency-light
// testing surface. Object-store-aware helpers (asserting an object's kind and
// body by id) are added in a sibling file once internal/objectstore lands; this
// file holds only filesystem/JSON primitives with no internal dependencies.
//
// Helpers are safe for callers using t.Parallel(): every NewWorkspace returns a
// distinct temp directory created via t.TempDir, and the package keeps no
// shared global state.
package objfs

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// NewWorkspace returns the absolute path to an isolated workspace root for the
// calling test, with an empty `.orun/` directory already created beneath it.
// The directory lives inside t.TempDir() and is removed automatically when the
// test (and its subtests) complete; callers do not need to register cleanup.
func NewWorkspace(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	orunDir := filepath.Join(root, ".orun")
	if err := os.MkdirAll(orunDir, 0o755); err != nil {
		t.Fatalf("objfs.NewWorkspace: mkdir %s: %v", orunDir, err)
	}
	return root
}

// WriteFile writes data to path (creating parent directories) and fails the
// test on error. It returns path so callers can inline it into assertions.
func WriteFile(t *testing.T, path string, data []byte) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("objfs.WriteFile: mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("objfs.WriteFile: write %s: %v", path, err)
	}
	return path
}

// ReadJSON reads path and unmarshals it into a value of type T, failing the
// test on any error. It is the typed read primitive for object-model fixtures.
func ReadJSON[T any](t *testing.T, path string) T {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("objfs.ReadJSON: read %s: %v", path, err)
	}
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("objfs.ReadJSON: unmarshal %s: %v", path, err)
	}
	return v
}

// AssertJSONFile asserts that the JSON document at path is semantically equal to
// expected. The comparison is schema-tolerant: both sides are marshaled to
// compact JSON with sorted keys (via encoding/json's map handling) so field
// order and insignificant whitespace do not cause spurious failures.
func AssertJSONFile(t *testing.T, path string, expected any) {
	t.Helper()
	gotRaw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("objfs.AssertJSONFile: read %s: %v", path, err)
	}
	got, err := normalizeJSON(gotRaw)
	if err != nil {
		t.Fatalf("objfs.AssertJSONFile: normalize file %s: %v", path, err)
	}
	wantRaw, err := json.Marshal(expected)
	if err != nil {
		t.Fatalf("objfs.AssertJSONFile: marshal expected: %v", err)
	}
	want, err := normalizeJSON(wantRaw)
	if err != nil {
		t.Fatalf("objfs.AssertJSONFile: normalize expected: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("objfs.AssertJSONFile: %s mismatch\n got: %s\nwant: %s", path, got, want)
	}
}

// normalizeJSON round-trips raw through a generic decode/encode so that two
// documents differing only in key order or whitespace compare equal. Object
// keys are emitted in sorted order because encoding/json marshals maps that
// way.
func normalizeJSON(raw []byte) ([]byte, error) {
	var g any
	if err := json.Unmarshal(raw, &g); err != nil {
		return nil, err
	}
	return json.Marshal(g)
}
