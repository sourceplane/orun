package catalogmodel

// Tests added by the Task 0023 verifier pass to lift internal/catalogmodel
// coverage above the spec-mandated 90 % floor (test-plan.md §1). They exercise
// the convenience surface (CanonicalEncodeString, CanonicalEqual,
// CatalogInputHash) and a handful of error / edge paths in CanonicalEncode,
// PrettyEncode, ManifestHash, and FormatSourceSnapshotKey that the implementer
// fixtures didn't reach.

import (
	"math"
	"strings"
	"testing"
)

func TestCanonicalEncodeStringMatchesBytes(t *testing.T) {
	v := map[string]any{"b": 2, "a": []any{1, "x", true}}
	b, err := CanonicalEncode(v)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	s, err := CanonicalEncodeString(v)
	if err != nil {
		t.Fatalf("CanonicalEncodeString: %v", err)
	}
	if string(b) != s {
		t.Fatalf("string form (%q) != bytes form (%q)", s, string(b))
	}
}

func TestCanonicalEncodeStringErrorsPropagate(t *testing.T) {
	// math.NaN cannot be JSON-marshalled — surface the error.
	if _, err := CanonicalEncodeString(math.NaN()); err == nil {
		t.Fatal("CanonicalEncodeString(NaN) returned nil error")
	}
	if _, err := CanonicalEncode(math.NaN()); err == nil {
		t.Fatal("CanonicalEncode(NaN) returned nil error")
	}
	if _, err := PrettyEncode(math.NaN()); err == nil {
		t.Fatal("PrettyEncode(NaN) returned nil error")
	}
	if _, err := CatalogInputHash(math.NaN()); err == nil {
		t.Fatal("CatalogInputHash(NaN) returned nil error")
	}
}

func TestCanonicalEqualTrueAndFalse(t *testing.T) {
	a := map[string]any{"a": 1, "b": 2}
	b := map[string]any{"b": 2, "a": 1}
	eq, err := CanonicalEqual(a, b)
	if err != nil {
		t.Fatalf("CanonicalEqual: %v", err)
	}
	if !eq {
		t.Fatal("expected reordered maps to canonical-equal")
	}

	c := map[string]any{"a": 1, "b": 3}
	eq, err = CanonicalEqual(a, c)
	if err != nil {
		t.Fatalf("CanonicalEqual: %v", err)
	}
	if eq {
		t.Fatal("expected differing maps to NOT canonical-equal")
	}
}

func TestCanonicalEqualErrorsPropagate(t *testing.T) {
	if _, err := CanonicalEqual(math.NaN(), 1); err == nil {
		t.Fatal("CanonicalEqual(NaN, 1) returned nil error")
	}
	if _, err := CanonicalEqual(1, math.NaN()); err == nil {
		t.Fatal("CanonicalEqual(1, NaN) returned nil error")
	}
}

func TestCatalogInputHashIsDeterministicAndOrderInvariant(t *testing.T) {
	a := map[string]any{"x": 1, "y": []any{"q", "r"}, "z": true}
	b := map[string]any{"z": true, "y": []any{"q", "r"}, "x": 1}

	ha, err := CatalogInputHash(a)
	if err != nil {
		t.Fatalf("CatalogInputHash(a): %v", err)
	}
	hb, err := CatalogInputHash(b)
	if err != nil {
		t.Fatalf("CatalogInputHash(b): %v", err)
	}
	if ha != hb {
		t.Fatalf("CatalogInputHash not order-invariant: %s vs %s", ha, hb)
	}
	if !strings.HasPrefix(ha, "sha256:") {
		t.Fatalf("expected sha256: prefix, got %q", ha)
	}
	if len(ha) != len("sha256:")+64 {
		t.Fatalf("expected 64-hex digest, got len=%d (%q)", len(ha), ha)
	}

	// Differing input must shift the hash.
	c := map[string]any{"x": 2, "y": []any{"q", "r"}, "z": true}
	hc, err := CatalogInputHash(c)
	if err != nil {
		t.Fatalf("CatalogInputHash(c): %v", err)
	}
	if hc == ha {
		t.Fatal("CatalogInputHash collided on different inputs")
	}
}

func TestManifestHashIsDeterministic(t *testing.T) {
	m := ComponentManifest{
		APIVersion: "orun.dev/v1",
		Kind:       "Component",
		Identity: ComponentIdentity{
			ComponentID:  "cmp_01HZX",
			ComponentKey: "ns/repo/name",
		},
		Spec: ComponentSpec{Type: "service", Lifecycle: "production"},
	}
	h1, err := ManifestHash(m)
	if err != nil {
		t.Fatalf("ManifestHash: %v", err)
	}
	h2, err := ManifestHash(m)
	if err != nil {
		t.Fatalf("ManifestHash: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("ManifestHash not deterministic: %s vs %s", h1, h2)
	}
	if !strings.HasPrefix(h1, "sha256:") {
		t.Fatalf("expected sha256: prefix, got %q", h1)
	}
}

func TestFormatSourceSnapshotKeyLocalNoGitEmptyDirty(t *testing.T) {
	out := FormatSourceSnapshotKey(SourceKeyParts{
		Scope:      SourceScopeLocalNoGit,
		DirtyShort: "",
	})
	if !strings.HasPrefix(out, SourceKeyPrefix) {
		t.Fatalf("expected key to start with %q, got %q", SourceKeyPrefix, out)
	}
	if strings.Contains(out, "-d") {
		t.Fatalf("expected no -d suffix when DirtyShort empty, got %q", out)
	}
	// And the populated dirty path:
	out2 := FormatSourceSnapshotKey(SourceKeyParts{
		Scope:      SourceScopeLocalNoGit,
		DirtyShort: "abcdef0123",
	})
	if !strings.Contains(out2, "-d") {
		t.Fatalf("expected -d suffix when DirtyShort populated, got %q", out2)
	}
}

func TestValidateSourceSnapshotKeyOverLength(t *testing.T) {
	// The pattern matches but the length check should still fail. Build a key
	// that satisfies the regex but exceeds SourceKeyMaxLen.
	if SourceKeyMaxLen <= 0 {
		t.Skip("SourceKeyMaxLen non-positive; skip")
	}
	long := SourceKeyPrefix + "git" + "-c" + strings.Repeat("a", SourceKeyMaxLen)
	if err := ValidateSourceSnapshotKey(long); err == nil {
		t.Fatal("expected over-length key to fail validation")
	}
}
