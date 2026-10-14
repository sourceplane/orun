package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseAndWriteWeights(t *testing.T) {
	w, err := parseWeights([]byte(`{"api.stage.deploy": 42.4, "web.stage.deploy": 0.2, "skip": 0}`))
	if err != nil {
		t.Fatal(err)
	}
	if w["api.stage.deploy"] != 42 || w["web.stage.deploy"] != 1 || len(w) != 2 {
		t.Fatalf("parsed %v", w)
	}
	if _, err := parseWeights([]byte(`[1,2]`)); err == nil || !strings.Contains(err.Error(), "JSON object") {
		t.Fatalf("a list is refused: %v", err)
	}
	path := filepath.Join(t.TempDir(), "lanes.lock")
	if err := writeWeights(path, map[string]int{"b": 2, "a": 1}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != "{\n  \"a\": 1,\n  \"b\": 2\n}\n" {
		t.Fatalf("written:\n%s", b)
	}
	back, err := parseWeights(b)
	if err != nil || back["a"] != 1 || back["b"] != 2 {
		t.Fatalf("round trip: %v %v", err, back)
	}
}

func TestMedianWeights(t *testing.T) {
	got := medianWeights(map[string][]float64{
		"a": {10, 30, 20},
		"b": {4, 6},
		"c": {0.2},
		"d": {},
	})
	if got["a"] != 20 || got["b"] != 5 || got["c"] != 1 {
		t.Fatalf("medians = %v", got)
	}
	if _, ok := got["d"]; ok {
		t.Fatal("no samples, no weight")
	}
}

func TestResolveWeights_FileAndNone(t *testing.T) {
	if w, from, err := resolveWeights(t.Context(), ""); w != nil || from != "" || err != nil {
		t.Fatalf("no flag: %v %q %v", w, from, err)
	}
	path := filepath.Join(t.TempDir(), "w.json")
	if err := os.WriteFile(path, []byte(`{"x": 3}`), 0o644); err != nil {
		t.Fatal(err)
	}
	w, from, err := resolveWeights(t.Context(), path)
	if err != nil || from != "file" || w["x"] != 3 {
		t.Fatalf("file: %v %q %v", w, from, err)
	}
	if _, _, err := resolveWeights(t.Context(), filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Fatal("a missing file is an error")
	}
}
