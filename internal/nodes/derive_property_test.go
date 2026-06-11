package nodes

import (
	"context"
	"strings"
	"testing"

	"pgregory.net/rapid"

	"github.com/sourceplane/orun/internal/objectstore"
)

// genEntityKey draws an adversarial entity-key-like string: it deliberately
// exercises the slash/at/uppercase/special-char space that real CODEOWNERS
// owners (@org/team), API refs (ns/repo/name), and resource keys live in — the
// space where the SC3 name-sanitization collision lurked.
func genEntityKey() *rapid.Generator[string] {
	return rapid.OneOf(
		rapid.StringMatching(`@[a-z]{1,3}-[a-z]{1,3}/[a-z]{1,4}`),    // @org-x/team
		rapid.StringMatching(`[a-z]{1,3}/[a-z]{1,3}/[a-z]{1,4}`),     // ns/repo/name
		rapid.StringMatching(`[a-z]{1,4}`),                           // bare (env-like)
		rapid.StringMatching(`[a-z]{1,3}[-_./]{1,2}[a-z]{1,3}`),      // fold-prone (a/b vs a-b)
		rapid.Just("edge"),                                          // forced collisions
		rapid.Just("auth"),
	)
}

// genManifest draws a ComponentManifest whose relations/contracts reference the
// adversarial keys above, so deriveEntities produces colliding-prone entities.
func genManifest(t *rapid.T, i int) ComponentManifest {
	name := rapid.StringMatching(`[a-z]{1,8}`).Draw(t, "name")
	key := "ns/repo/" + name + "-" + string(rune('a'+i%26))
	var rels []EntityRelation
	n := rapid.IntRange(0, 4).Draw(t, "nrels")
	for j := 0; j < n; j++ {
		kind := rapid.SampledFrom([]string{"Group", "System", "Domain", "Resource", "Environment", "Composition"}).Draw(t, "kind")
		typ := map[string]string{
			"Group": "ownedBy", "System": "partOf", "Domain": "partOf",
			"Resource": "dependsOn", "Environment": "deployedTo", "Composition": "composedBy",
		}[kind]
		rels = append(rels, EntityRelation{Type: typ, To: genEntityKey().Draw(t, "to"), ToKind: kind})
	}
	return ComponentManifest{
		Identity:  ComponentIdentity{ComponentKey: key, Name: name + "-" + string(rune('a'+i%26)), Namespace: "ns", Repo: "repo"},
		Relations: rels,
	}
}

// TestProperty_AssembleCatalogNeverCollides is the durable defense against the
// SC3 collision class: for ANY set of manifests whose derived entities share
// sanitized names, AssembleCatalog must (1) never error on a duplicate tree
// name, (2) be deterministic (two assembles → same id), and (3) write exactly
// the distinct (kind, entityKey) entities, no more, no fewer.
func TestProperty_AssembleCatalogNeverCollides(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		ctx := context.Background()
		count := rapid.IntRange(0, 6).Draw(t, "ncomponents")
		manifests := make([]ComponentManifest, count)
		for i := range manifests {
			manifests[i] = genManifest(t, i)
		}

		s1 := objectstore.NewMemStore("")
		cat := CatalogSnapshot{SourceID: "sha256:" + strings.Repeat("a", 64), ResolverVersion: 9}
		id1, err := AssembleCatalog(ctx, s1, cat, manifests, nil, ImpactOwnership{}, nil)
		if err != nil {
			t.Fatalf("AssembleCatalog errored on collision-prone input: %v", err)
		}

		// Determinism: a second assemble into a fresh store yields the same id.
		s2 := objectstore.NewMemStore("")
		id2, err := AssembleCatalog(ctx, s2, cat, manifests, nil, ImpactOwnership{}, nil)
		if err != nil {
			t.Fatalf("second AssembleCatalog: %v", err)
		}
		if id1 != id2 {
			t.Fatalf("AssembleCatalog not deterministic: %s vs %s", id1, id2)
		}

		// Exactness: the number of derived entities equals the distinct
		// (kind, entityKey) set the relations imply (Component is separate).
		wantByKind := map[string]map[string]bool{}
		for _, m := range manifests {
			for _, r := range m.Relations {
				if wantByKind[r.ToKind] == nil {
					wantByKind[r.ToKind] = map[string]bool{}
				}
				wantByKind[r.ToKind][r.To] = true
			}
		}
		entitiesTree, kind := findTreeEntry(t, s1, id1, dirEntities)
		if kind != objectstore.KindTree {
			t.Fatalf("entities/ not a tree")
		}
		kindDirs, _ := s1.GetTree(ctx, entitiesTree)
		gotByKind := map[string]int{}
		for _, kd := range kindDirs {
			if kd.Kind != objectstore.KindTree {
				continue
			}
			blobs, _ := s1.GetTree(ctx, kd.ID)
			// All entry names within a kind subtree must be unique (the store
			// enforces this, but assert the count maps to distinct entities).
			seen := map[string]bool{}
			for _, b := range blobs {
				if seen[b.Name] {
					t.Fatalf("duplicate filename %q in entities/%s", b.Name, kd.Name)
				}
				seen[b.Name] = true
			}
			gotByKind[kd.Name] = len(blobs)
		}
		for k, want := range wantByKind {
			if gotByKind[k] != len(want) {
				t.Fatalf("kind %s: wrote %d entities, want %d distinct", k, gotByKind[k], len(want))
			}
		}
	})
}

// findTreeEntry returns the id+kind of a named entry in a tree.
func findTreeEntry(t *rapid.T, s *objectstore.MemStore, tree objectstore.ObjectID, name string) (objectstore.ObjectID, objectstore.Kind) {
	entries, err := s.GetTree(context.Background(), tree)
	if err != nil {
		t.Fatalf("GetTree: %v", err)
	}
	for _, e := range entries {
		if e.Name == name {
			return e.ID, e.Kind
		}
	}
	t.Fatalf("entry %q not found", name)
	return "", ""
}
