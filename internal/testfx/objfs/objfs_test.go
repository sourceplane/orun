package objfs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestNewWorkspaceIsolatesAndCreatesOrunDir(t *testing.T) {
	t.Parallel()
	a := NewWorkspace(t)
	b := NewWorkspace(t)
	if a == b {
		t.Fatalf("expected distinct workspaces, got %q twice", a)
	}
	for _, root := range []string{a, b} {
		info, err := os.Stat(filepath.Join(root, ".orun"))
		if err != nil {
			t.Fatalf("stat .orun under %s: %v", root, err)
		}
		if !info.IsDir() {
			t.Fatalf(".orun under %s is not a directory", root)
		}
	}
}

func TestWriteFileCreatesParentsAndReturnsPath(t *testing.T) {
	t.Parallel()
	root := NewWorkspace(t)
	target := filepath.Join(root, ".orun", "nested", "deep", "x.json")
	got := WriteFile(t, target, []byte(`{"a":1}`))
	if got != target {
		t.Fatalf("WriteFile returned %q, want %q", got, target)
	}
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(data) != `{"a":1}` {
		t.Fatalf("content mismatch: %s", data)
	}
}

func TestReadJSONTyped(t *testing.T) {
	t.Parallel()
	type rec struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	root := NewWorkspace(t)
	p := WriteFile(t, filepath.Join(root, "r.json"), []byte(`{"name":"api-edge","count":7}`))
	got := ReadJSON[rec](t, p)
	if got.Name != "api-edge" || got.Count != 7 {
		t.Fatalf("ReadJSON got %+v", got)
	}
}

func TestAssertJSONFileToleratesKeyOrderAndWhitespace(t *testing.T) {
	t.Parallel()
	root := NewWorkspace(t)
	// File has keys in one order with indentation; expected struct in another.
	p := WriteFile(t, filepath.Join(root, "e.json"), []byte("{\n  \"b\": 2,\n  \"a\": 1\n}\n"))
	AssertJSONFile(t, p, map[string]any{"a": 1, "b": 2})
}

func TestNormalizeJSONSortsKeys(t *testing.T) {
	t.Parallel()
	out, err := normalizeJSON([]byte(`{"z":1,"a":2}`))
	if err != nil {
		t.Fatalf("normalizeJSON: %v", err)
	}
	if string(out) != `{"a":2,"z":1}` {
		t.Fatalf("normalizeJSON did not sort keys: %s", out)
	}
	// Round-trips back to an equal generic value.
	var v map[string]any
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("unmarshal normalized: %v", err)
	}
}
