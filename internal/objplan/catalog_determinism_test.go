package objplan

// catalog_determinism_test.go is the CS8 determinism gate (test-plan.md §4, P-2):
// assembling a catalog must be byte-deterministic regardless of the order
// components are presented in — so two resolves of one source produce identical
// impact/ownership.json and impact/fingerprints/ artifacts (and the same catalog
// Merkle id). The content-addressed catalog id is the Merkle root over the whole
// tree (components + graph + impact/{ownership,fingerprints}), so asserting the
// id is stable across random orderings proves every persisted artifact under it
// is byte-identical.

import (
	"context"
	"math/rand"
	"testing"

	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
)

func TestAssembleCatalog_DeterministicAcrossOrderings(t *testing.T) {
	ctx := context.Background()

	manifests := []nodes.ComponentManifest{
		{Kind: nodes.KindComponentManifest, Type: "library",
			Identity: nodes.ComponentIdentity{ComponentKey: "ns/repo/shared", Name: "shared", Namespace: "ns", Repo: "repo", Path: "libs/shared/component.yaml"},
			Spec:     map[string]any{"type": "library", "domain": "platform"}},
		{Kind: nodes.KindComponentManifest, Type: "worker",
			Identity: nodes.ComponentIdentity{ComponentKey: "ns/repo/api", Name: "api", Namespace: "ns", Repo: "repo", Path: "apps/api/component.yaml"},
			Spec:     map[string]any{"type": "worker", "domain": "edge"}},
		{Kind: nodes.KindComponentManifest, Type: "worker",
			Identity: nodes.ComponentIdentity{ComponentKey: "ns/repo/web", Name: "web", Namespace: "ns", Repo: "repo", Path: "apps/web/frontend/component.yaml"},
			Spec:     map[string]any{"type": "worker", "domain": "edge"}},
	}
	graphs := []nodes.CatalogGraph{{Kind: nodes.KindCatalogGraph, EdgeKind: "dependencies",
		Edges: []nodes.GraphEdge{
			{From: "ns/repo/api", To: "ns/repo/shared", Type: "depends_on"},
			{From: "ns/repo/web", To: "ns/repo/api", Type: "depends_on"},
		}}}
	ownership := nodes.ImpactOwnership{Kind: nodes.KindImpactOwnership, SchemaVersion: 1,
		Components: map[string]string{
			"libs/shared":       "ns/repo/shared",
			"apps/api":          "ns/repo/api",
			"apps/web/frontend": "ns/repo/web",
		},
		GlobalPaths:         []string{"intent.yaml"},
		StructuralFilenames: []string{"component.yaml"},
		IgnoreDirs:          []string{".git"}}
	fps := []nodes.ComponentFingerprint{
		{ComponentKey: "ns/repo/shared", Dir: "libs/shared", Subtree: "sha256:1111", GlobalDigest: "sha256:gd"},
		{ComponentKey: "ns/repo/api", Dir: "apps/api", Subtree: "sha256:2222", GlobalDigest: "sha256:gd"},
		{ComponentKey: "ns/repo/web", Dir: "apps/web/frontend", Subtree: "sha256:3333", GlobalDigest: "sha256:gd"},
	}
	cat := nodes.CatalogSnapshot{Kind: nodes.KindCatalogSnapshot, SourceID: "sha256:" + rep64('a'), ResolverVersion: 1}

	assemble := func(ms []nodes.ComponentManifest, fs []nodes.ComponentFingerprint) string {
		store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: t.TempDir()})
		if err != nil {
			t.Fatalf("store: %v", err)
		}
		id, err := nodes.AssembleCatalog(ctx, store, cat, ms, graphs, ownership, fs)
		if err != nil {
			t.Fatalf("assemble: %v", err)
		}
		return string(id)
	}

	want := assemble(manifests, fps)

	r := rand.New(rand.NewSource(1))
	for i := 0; i < 128; i++ {
		ms := append([]nodes.ComponentManifest(nil), manifests...)
		r.Shuffle(len(ms), func(a, b int) { ms[a], ms[b] = ms[b], ms[a] })
		fs := append([]nodes.ComponentFingerprint(nil), fps...)
		r.Shuffle(len(fs), func(a, b int) { fs[a], fs[b] = fs[b], fs[a] })

		if got := assemble(ms, fs); got != want {
			t.Fatalf("catalog id differs across orderings (iter %d):\n want %s\n  got %s", i, want, got)
		}
	}
}

func rep64(c byte) string {
	b := make([]byte, 64)
	for i := range b {
		b[i] = c
	}
	return string(b)
}
