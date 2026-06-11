package catalogmodel_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// roundtripCase pairs a Go zero-prototype with a fixture filename. The test
// loads the fixture, decodes it into a fresh instance of the prototype's
// type, re-encodes through PrettyEncode, and asserts byte-identical output.
type roundtripCase struct {
	name     string
	fixture  string
	prototype any // pointer to zero value of the type
}

func TestGoldenRoundtrip(t *testing.T) {
	cases := []roundtripCase{
		{"SourceSnapshot", "source_snapshot.json", &catalogmodel.SourceSnapshot{}},
		{"CatalogSnapshot", "catalog_snapshot.json", &catalogmodel.CatalogSnapshot{}},
		{"ComponentManifest", "component_manifest.json", &catalogmodel.ComponentManifest{}},
		{"CatalogGraph", "catalog_graph.json", &catalogmodel.CatalogGraph{}},
		{"ComponentYAML", "component_yaml.json", &catalogmodel.ComponentYAML{}},
		{"EntityEnvelope", "entity_envelope.json", &catalogmodel.EntityEnvelope{}},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join("testdata", "golden", tc.fixture)
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("read fixture %s: %v", path, err)
			}
			// Decode into a fresh instance of the prototype's element type.
			ptr := reflect.New(reflect.TypeOf(tc.prototype).Elem()).Interface()
			if err := json.Unmarshal(raw, ptr); err != nil {
				t.Fatalf("unmarshal %s: %v", path, err)
			}
			got, err := catalogmodel.PrettyEncode(ptr)
			if err != nil {
				t.Fatalf("PrettyEncode: %v", err)
			}
			// Fixture is committed with a trailing newline so editors don't
			// strip it; PrettyEncode does not emit one. Trim for comparison.
			want := bytes.TrimRight(raw, "\n")
			if !bytes.Equal(got, want) {
				if os.Getenv("UPDATE_GOLDEN") == "1" {
					if err := os.WriteFile(path, append(got, '\n'), 0o644); err != nil {
						t.Fatalf("update golden: %v", err)
					}
					t.Logf("UPDATED %s", path)
					return
				}
				t.Errorf("golden mismatch for %s\n--- got ---\n%s\n--- want ---\n%s",
					tc.name, got, want)
			}
		})
	}
}

// TestCanonicalDeterminism asserts the canonical encoder produces stable
// bytes regardless of map insertion order. Per test-plan.md T-IDK-1.
func TestCanonicalDeterminism(t *testing.T) {
	a := map[string]any{
		"z": 1,
		"a": map[string]any{"y": 2, "b": 3},
	}
	b := map[string]any{
		"a": map[string]any{"b": 3, "y": 2},
		"z": 1,
	}
	enc := catalogmodel.CanonicalEncode
	ab, err := enc(a)
	if err != nil {
		t.Fatal(err)
	}
	bb, err := enc(b)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(ab, bb) {
		t.Fatalf("canonical not deterministic:\na=%s\nb=%s", ab, bb)
	}
	want := `{"a":{"b":3,"y":2},"z":1}`
	if string(ab) != want {
		t.Fatalf("canonical shape: got %s want %s", ab, want)
	}
}

// TestPrettyEncodeShape asserts PrettyEncode produces 2-space indented,
// sorted-key output and is a superset (whitespace-only diff) of CanonicalEncode.
func TestPrettyEncodeShape(t *testing.T) {
	v := map[string]any{"b": 2, "a": 1}
	pretty, err := catalogmodel.PrettyEncode(v)
	if err != nil {
		t.Fatal(err)
	}
	want := "{\n  \"a\": 1,\n  \"b\": 2\n}"
	if string(pretty) != want {
		t.Fatalf("pretty mismatch:\ngot:  %q\nwant: %q", pretty, want)
	}
}
