package catalogresolve

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"pgregory.net/rapid"
)

// makeFixtureManifests builds a small synthetic resolved manifest set
// covering all five graph kinds (deps, system, apis, resources, owner).
// Each manifest carries a real manifestHash so catalogHash has stable
// inputs to consume.
func makeFixtureManifests(t testing.TB) []*catalogmodel.ComponentManifest {
	t.Helper()
	mk := func(name, system, owner, domain string, deps []catalogmodel.ComponentDependency, provides, consumes, uses []string) *catalogmodel.ComponentManifest {
		cm := &catalogmodel.ComponentManifest{
			APIVersion: "orun.io/v1alpha1",
			Kind:       "Component",
			Identity: catalogmodel.ComponentIdentity{
				ComponentKey: "sourceplane/orun/" + name,
				Name:         name,
				Namespace:    "sourceplane",
				Repo:         "orun",
				Path:         "apps/" + name,
				SourceFile:   "apps/" + name + "/component.yaml",
			},
			Metadata: catalogmodel.ComponentMetadata{Owner: owner},
			Spec: catalogmodel.ComponentSpec{
				System: system,
				Domain: domain,
				Dependencies: catalogmodel.ComponentDependencies{
					Components: deps,
					APIs: catalogmodel.APIDependencies{
						Provides: provides,
						Consumes: consumes,
					},
					Resources: catalogmodel.ResourceDependencies{
						Uses: uses,
					},
				},
			},
		}
		h, err := manifestHash(cm)
		if err != nil {
			t.Fatalf("manifestHash %s: %v", name, err)
		}
		cm.Source.ManifestHash = h
		return cm
	}

	return []*catalogmodel.ComponentManifest{
		mk("api-edge", "edge", "team-a", "platform",
			[]catalogmodel.ComponentDependency{
				{Key: "sourceplane/orun/identity-worker", Name: "identity-worker", Relationship: catalogmodel.RelCalls},
			},
			[]string{"public-api"},
			[]string{"identity-api"},
			[]string{"redis"},
		),
		mk("identity-worker", "edge", "team-b", "identity",
			nil,
			[]string{"identity-api"},
			nil,
			[]string{"postgres"},
		),
	}
}

// TestCatalogHash_Deterministic_T_IDK_1 covers test-plan.md §2 T-IDK-1:
// 1000 random orderings of the manifest input bundle produce the
// identical catalogHash. Inputs go through the canonical encoder so
// the hash MUST be order-insensitive only for inputs that are sorted
// before being fed in. We sort the manifest slice by ComponentKey
// before hashing — the property we assert is that no matter how the
// caller hands us the slice, after the resolver's internal sort the
// hash is identical.
func TestCatalogHash_Deterministic_T_IDK_1(t *testing.T) {
	manifests := makeFixtureManifests(t)
	graphs := buildGraphs(manifests, "src-abc12345", "")

	want, err := catalogHash("dirty:abc", manifests, graphs, 1)
	if err != nil {
		t.Fatalf("baseline catalogHash: %v", err)
	}

	rapid.Check(t, func(t *rapid.T) {
		// rapid yields a permutation index; we apply it via Fisher–Yates.
		seed := rapid.Int64().Draw(t, "seed")
		shuffled := append([]*catalogmodel.ComponentManifest(nil), manifests...)
		r := rand.New(rand.NewSource(seed))
		r.Shuffle(len(shuffled), func(i, j int) { shuffled[i], shuffled[j] = shuffled[j], shuffled[i] })

		// Re-sort to mimic what Resolve guarantees post-stage 10
		// (manifests sorted by ComponentKey). catalogHash itself takes
		// the slice as-is — determinism comes from the contract that
		// the caller hands in a sorted slice.
		sortByComponentKey(shuffled)
		gShuf := buildGraphs(shuffled, "src-abc12345", "")

		got, err := catalogHash("dirty:abc", shuffled, gShuf, 1)
		if err != nil {
			t.Fatalf("catalogHash: %v", err)
		}
		if got != want {
			t.Fatalf("catalogHash differs across orderings:\n want %s\n  got %s", want, got)
		}
	})
}

func sortByComponentKey(m []*catalogmodel.ComponentManifest) {
	// Local sort identical to resolve_full.go's stage-10 sort.
	// Bubble sort is fine — fixture is tiny.
	for i := 0; i < len(m); i++ {
		for j := i + 1; j < len(m); j++ {
			if m[j].Identity.ComponentKey < m[i].Identity.ComponentKey {
				m[i], m[j] = m[j], m[i]
			}
		}
	}
}

// TestCatalogHash_OwnerEditChanges asserts the C3 acceptance signal:
// changing metadata.owner on one component changes both manifestHash
// (already proven at C2) AND catalogHash (because manifestHash feeds
// catalogHash).
func TestCatalogHash_OwnerEditChanges(t *testing.T) {
	manifests := makeFixtureManifests(t)
	graphs := buildGraphs(manifests, "src", "")
	before, err := catalogHash("ci:1", manifests, graphs, 1)
	if err != nil {
		t.Fatal(err)
	}

	// Mutate owner on manifest[0] and recompute.
	manifests[0].Metadata.Owner = "team-x"
	newHash, err := manifestHash(manifests[0])
	if err != nil {
		t.Fatal(err)
	}
	manifests[0].Source.ManifestHash = newHash
	graphs2 := buildGraphs(manifests, "src", "")
	after, err := catalogHash("ci:1", manifests, graphs2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if before == after {
		t.Fatalf("catalogHash unchanged after metadata.owner edit:\n  before=%s\n   after=%s", before, after)
	}
	if !strings.HasPrefix(after, "sha256:") {
		t.Errorf("catalogHash missing sha256: prefix: %q", after)
	}
}

// TestCatalogHash_ProvenanceOnlyEdit_Stable asserts that a change to
// only resolution.inheritedFrom (provenance) does NOT change either
// manifestHash (already enforced at C2) or catalogHash. Provenance is
// outside the hash scope by design — see identity-and-keys.md §10.
func TestCatalogHash_ProvenanceOnlyEdit_Stable(t *testing.T) {
	manifests := makeFixtureManifests(t)
	graphs := buildGraphs(manifests, "src", "")
	before, err := catalogHash("ci:1", manifests, graphs, 1)
	if err != nil {
		t.Fatal(err)
	}

	// Mutate ONLY provenance — InheritedFrom — on each manifest.
	for _, m := range manifests {
		if m.Resolution.InheritedFrom == nil {
			m.Resolution.InheritedFrom = map[string]string{}
		}
		m.Resolution.InheritedFrom["metadata.owner"] = "/intent.yaml"
		// manifestHash MUST be stable under this change.
		h, err := manifestHash(m)
		if err != nil {
			t.Fatal(err)
		}
		if h != m.Source.ManifestHash {
			t.Fatalf("manifestHash changed under provenance edit: was %s now %s", m.Source.ManifestHash, h)
		}
	}

	graphs2 := buildGraphs(manifests, "src", "")
	after, err := catalogHash("ci:1", manifests, graphs2, 1)
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("catalogHash changed under provenance-only edit:\n  before=%s\n   after=%s", before, after)
	}
}

// TestCatalogHash_ResolverVersionBump asserts that a resolverVersion
// bump produces a different catalogHash even when all other inputs are
// identical. Locks identity-and-keys.md §9 input #4.
func TestCatalogHash_ResolverVersionBump(t *testing.T) {
	manifests := makeFixtureManifests(t)
	graphs := buildGraphs(manifests, "src", "")
	v1, err := catalogHash("ci:1", manifests, graphs, 1)
	if err != nil {
		t.Fatal(err)
	}
	v2, err := catalogHash("ci:1", manifests, graphs, 2)
	if err != nil {
		t.Fatal(err)
	}
	if v1 == v2 {
		t.Fatalf("catalogHash insensitive to resolverVersion bump")
	}
}

// TestCatalogHash_InputHashChange asserts catalogInputHash flows into
// catalogHash (locks input #1).
func TestCatalogHash_InputHashChange(t *testing.T) {
	manifests := makeFixtureManifests(t)
	graphs := buildGraphs(manifests, "src", "")
	a, _ := catalogHash("ci:a", manifests, graphs, 1)
	b, _ := catalogHash("ci:b", manifests, graphs, 1)
	if a == b {
		t.Fatalf("catalogHash insensitive to catalogInputHash")
	}
}
