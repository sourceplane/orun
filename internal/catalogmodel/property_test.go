package catalogmodel_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// T-IDK-1: CanonicalEncode is order-invariant for arbitrarily-shaped JSON.
// Two semantically-equal documents that differ only in map insertion order
// must produce byte-identical canonical encodings.
func TestProperty_CanonicalEncodeOrderInvariant(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		v := genJSONValue().Draw(t, "v")
		// Re-encode through Go's encoder twice with map keys shuffled by
		// re-decoding into a fresh map. Both outputs of CanonicalEncode must
		// match.
		raw, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		var a, b any
		if err := json.Unmarshal(raw, &a); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(raw, &b); err != nil {
			t.Fatal(err)
		}
		ca, err := catalogmodel.CanonicalEncode(a)
		if err != nil {
			t.Fatal(err)
		}
		cb, err := catalogmodel.CanonicalEncode(b)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(ca, cb) {
			t.Fatalf("canonical encode not order-invariant:\n  a=%s\n  b=%s", ca, cb)
		}
		// And: must contain no whitespace.
		if bytes.ContainsAny(ca, " \t\n\r") {
			// Allow whitespace inside strings — only the framing must be tight.
			// We only check there's no top-level pretty-printing: there should
			// be no indent characters between structural tokens. A simple
			// heuristic: encode/decode round-trip yields a single-line string
			// for non-string inputs. Strings inside `v` may legitimately
			// contain whitespace; skip the check when v itself contains a
			// string with whitespace.
			if !rapidJSONHasWhitespaceInString(v) {
				t.Fatalf("canonical contains whitespace: %s", ca)
			}
		}
	})
}

// T-IDK-3: ManifestHash is stable under provenance changes
// (`resolution.inheritedFrom` / `inferredFrom`) and unstable under any
// resolved-value change.
func TestProperty_ManifestHashProvenanceInvariant(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		base := genComponentManifest().Draw(t, "manifest")
		baseHash, err := catalogmodel.ManifestHash(base)
		if err != nil {
			t.Fatal(err)
		}

		// Tweak provenance only — must not change the hash.
		mut := base
		mut.Resolution = catalogmodel.ComponentResolution{
			InheritedFrom: map[string]string{
				"metadata.owner": "intent.yaml:catalog.defaults.owner",
				"spec.lifecycle": "component.yaml:spec.lifecycle",
			},
			InferredFrom: map[string][]string{
				"runtime.inferred.languages": {"path/a", "path/b"},
			},
		}
		mutHash, err := catalogmodel.ManifestHash(mut)
		if err != nil {
			t.Fatal(err)
		}
		if mutHash != baseHash {
			t.Fatalf("manifestHash changed under provenance-only mutation:\n  base=%s\n  mut=%s", baseHash, mutHash)
		}

		// Tweak Source — must not change the hash either (Source is excluded).
		mut2 := base
		mut2.Source.ManifestHash = "sha256:deadbeef"
		mut2.Source.Branch = "different-branch"
		mut2Hash, err := catalogmodel.ManifestHash(mut2)
		if err != nil {
			t.Fatal(err)
		}
		if mut2Hash != baseHash {
			t.Fatalf("manifestHash changed under source-only mutation:\n  base=%s\n  mut2=%s", baseHash, mut2Hash)
		}

		// Tweak a resolved value — MUST change the hash.
		mut3 := base
		mut3.Metadata.Owner = base.Metadata.Owner + "-changed-owner-x9"
		mut3Hash, err := catalogmodel.ManifestHash(mut3)
		if err != nil {
			t.Fatal(err)
		}
		if mut3Hash == baseHash {
			t.Fatalf("manifestHash unchanged under resolved-value mutation: %s", baseHash)
		}
	})
}

// T-IDK-5: Sanitizers are total — never panic on any input, and their output
// always satisfies the documented post-conditions.
func TestProperty_SanitizersTotal(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		s := rapid.String().Draw(t, "input")

		out := catalogmodel.SanitizeBranch(s)
		// Post-condition: only [a-z0-9-] characters, length <= 40.
		if len(out) > 40 {
			t.Fatalf("SanitizeBranch returned len=%d > 40 for %q -> %q", len(out), s, out)
		}
		for i := 0; i < len(out); i++ {
			c := out[i]
			ok := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-'
			if !ok {
				t.Fatalf("SanitizeBranch produced disallowed char %q in %q (input %q)", c, out, s)
			}
		}

		ckOut := catalogmodel.SanitizeComponentKey(s)
		if strings.ContainsRune(ckOut, '/') {
			t.Fatalf("SanitizeComponentKey left '/' in %q (input %q)", ckOut, s)
		}

		ekOut := catalogmodel.SanitizeEventKind(s)
		if strings.ContainsRune(ekOut, '.') {
			t.Fatalf("SanitizeEventKind left '.' in %q (input %q)", ekOut, s)
		}

		// ShortHex with random n in [-2, 64].
		n := rapid.IntRange(-2, 64).Draw(t, "n")
		_ = catalogmodel.ShortHex(s, n) // assert no panic
	})
}

// genJSONValue is a shrinking-friendly recursive generator that produces
// arbitrary JSON-shaped Go values: nil, bool, int64, float64, string,
// []any, map[string]any. Bounded depth so rapid converges.
func genJSONValue() *rapid.Generator[any] {
	return rapid.Custom(func(t *rapid.T) any {
		return drawJSONValue(t, 0)
	})
}

func drawJSONValue(t *rapid.T, depth int) any {
	if depth > 3 {
		// Force a leaf at the bottom.
		return rapid.OneOf(
			rapid.Just[any](nil),
			rapid.Bool().AsAny(),
			rapid.Int64().AsAny(),
			rapid.String().AsAny(),
		).Draw(t, "leaf")
	}
	choice := rapid.IntRange(0, 6).Draw(t, "kind")
	switch choice {
	case 0:
		return nil
	case 1:
		return rapid.Bool().Draw(t, "bool")
	case 2:
		return rapid.Int64().Draw(t, "i64")
	case 3:
		return rapid.String().Draw(t, "str")
	case 4:
		// array
		n := rapid.IntRange(0, 4).Draw(t, "len")
		out := make([]any, n)
		for i := 0; i < n; i++ {
			out[i] = drawJSONValue(t, depth+1)
		}
		return out
	default:
		// map[string]any
		n := rapid.IntRange(0, 4).Draw(t, "mapLen")
		out := make(map[string]any, n)
		for i := 0; i < n; i++ {
			k := rapid.StringMatching(`[a-z]{1,8}`).Draw(t, "key")
			out[k] = drawJSONValue(t, depth+1)
		}
		return out
	}
}

// genComponentManifest produces an arbitrary ComponentManifest with at least
// one populated field per top-level group, so mutations under test have
// something to perturb.
func genComponentManifest() *rapid.Generator[catalogmodel.ComponentManifest] {
	return rapid.Custom(func(t *rapid.T) catalogmodel.ComponentManifest {
		return catalogmodel.ComponentManifest{
			APIVersion: catalogmodel.APIVersionV1Alpha1,
			Kind:       catalogmodel.KindComponentManifest,
			Identity: catalogmodel.ComponentIdentity{
				ComponentID:  "cmp_" + rapid.StringMatching(`[A-Z0-9]{26}`).Draw(t, "id"),
				ComponentKey: rapid.StringMatching(`[a-z]{3,8}/[a-z]{3,8}/[a-z]{3,8}`).Draw(t, "key"),
				Name:         rapid.StringMatching(`[a-z]{3,8}`).Draw(t, "name"),
				Namespace:    rapid.StringMatching(`[a-z]{3,8}`).Draw(t, "ns"),
				Repo:         rapid.StringMatching(`[a-z]{3,8}/[a-z]{3,8}`).Draw(t, "repo"),
				Path:         rapid.StringMatching(`[a-z/]{3,16}`).Draw(t, "path"),
				SourceFile:   "component.yaml",
			},
			Source: catalogmodel.ComponentSource{
				SourceSnapshotKey:  "src-branch-main-cdef456a-t5ab21c3",
				CatalogSnapshotKey: "cat-c8e91d2a",
				Ref:                "refs/heads/main",
				Branch:             "main",
				HeadRevision:       "def456a1b2c3",
				TreeHash:           "5ab21c3",
				WorkingTree:        catalogmodel.WorkingTreeClean,
				ManifestHash:       "", // set post-hash, MUST be excluded
			},
			Metadata: catalogmodel.ComponentMetadata{
				Title:       rapid.String().Draw(t, "title"),
				Description: rapid.String().Draw(t, "desc"),
				Owner:       rapid.StringMatching(`team/[a-z]{3,8}`).Draw(t, "owner"),
				Maintainers: []string{"team/platform"},
				Contacts:    map[string]string{"slack": "#x"},
				Labels:      map[string]string{"k": "v"},
				Tags:        []string{"a", "b"},
				Annotations: map[string]string{"k": "v"},
			},
			Spec: catalogmodel.ComponentSpec{
				Type:      "cloudflare-worker",
				Lifecycle: rapid.SampledFrom([]string{"production", "experimental", "deprecated"}).Draw(t, "lifecycle"),
				System:    "sourceplane-control-plane",
				Domain:    "edge",
				Tier:      "critical",
				Composition: catalogmodel.CompositionRef{
					Source: "ghcr.io/x/y:1.0",
					Type:   "cloudflare-worker",
				},
				Parameters: map[string]string{},
				Environments: map[string]catalogmodel.ComponentEnvironment{
					"production": {Profile: "worker.release", Active: true},
				},
				Dependencies: catalogmodel.ComponentDependencies{
					Components: []catalogmodel.ComponentDependency{},
					APIs:       catalogmodel.APIDependencies{Provides: []string{}, Consumes: []string{}},
					Resources:  catalogmodel.ResourceDependencies{Uses: []string{}},
				},
			},
			Runtime: catalogmodel.ComponentRuntime{
				Inferred: catalogmodel.ComponentInferred{
					Languages:       []string{"go"},
					PackageManagers: []string{},
					Frameworks:      []string{},
					Infra:           []string{},
				},
				Files: catalogmodel.ComponentFiles{},
			},
			Resolution: catalogmodel.ComponentResolution{
				InheritedFrom: map[string]string{},
				InferredFrom:  map[string][]string{},
			},
		}
	})
}

func rapidJSONHasWhitespaceInString(v any) bool {
	switch x := v.(type) {
	case string:
		return strings.ContainsAny(x, " \t\n\r")
	case []any:
		for _, item := range x {
			if rapidJSONHasWhitespaceInString(item) {
				return true
			}
		}
	case map[string]any:
		for k, val := range x {
			if strings.ContainsAny(k, " \t\n\r") || rapidJSONHasWhitespaceInString(val) {
				return true
			}
		}
	}
	return false
}
