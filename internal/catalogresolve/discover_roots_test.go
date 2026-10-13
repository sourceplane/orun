package catalogresolve

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// rootsTestPaths returns the sorted workspace-relative source files of a
// DiscoverAndLoad result.
func rootsTestPaths(dr DiscoveryResult) []string {
	out := make([]string, 0, len(dr.Manifests))
	for _, m := range dr.Manifests {
		out = append(out, m.SourceFile)
	}
	return out
}

func TestDiscoverAndLoad_HonoursDiscoveryRoots(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/intent.yaml", "discovery:\n  roots: [apps/]\n")
	mustWrite(t, root+"/apps/api/component.yaml", validYAML("api"))
	// A same-named copy outside the roots: the planner never sees it, so the
	// catalog must not either.
	mustWrite(t, root+"/variations/a/overlay/apps/api/component.yaml", validYAML("api"))

	dr, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("DiscoverAndLoad: %v", err)
	}
	got := strings.Join(rootsTestPaths(dr), ",")
	if got != "apps/api/component.yaml" {
		t.Fatalf("manifests = %q, want only the one under apps/", got)
	}
}

func TestDiscoverAndLoad_NoRootsWalksWholeWorkspace(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/intent.yaml", "metadata:\n  name: demo\n")
	mustWrite(t, root+"/apps/api/component.yaml", validYAML("api"))
	mustWrite(t, root+"/infra/net/component.yaml", validYAML("net"))

	dr, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("DiscoverAndLoad: %v", err)
	}
	if got := strings.Join(rootsTestPaths(dr), ","); got != "apps/api/component.yaml,infra/net/component.yaml" {
		t.Fatalf("manifests = %q", got)
	}
}

func TestDiscoverAndLoad_RootsIgnoredWhenIntentExtendsPresets(t *testing.T) {
	// Presets may contribute roots the resolver cannot see; the walk stays
	// whole-workspace rather than hiding components the planner plans.
	root := t.TempDir()
	mustWrite(t, root+"/intent.yaml", "extends:\n  - source: stack\n    preset: standard\ndiscovery:\n  roots: [apps/]\n")
	mustWrite(t, root+"/apps/api/component.yaml", validYAML("api"))
	mustWrite(t, root+"/infra/net/component.yaml", validYAML("net"))

	dr, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("DiscoverAndLoad: %v", err)
	}
	if got := strings.Join(rootsTestPaths(dr), ","); got != "apps/api/component.yaml,infra/net/component.yaml" {
		t.Fatalf("manifests = %q, want both (presets in play)", got)
	}
}

func TestDiscoverAndLoad_OverlappingRootsDedupe(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/intent.yaml", "discovery:\n  roots: [\".\", apps, apps/]\n")
	mustWrite(t, root+"/apps/api/component.yaml", validYAML("api"))

	dr, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("DiscoverAndLoad: %v", err)
	}
	if got := strings.Join(rootsTestPaths(dr), ","); got != "apps/api/component.yaml" {
		t.Fatalf("manifests = %q, want one entry (no mixed-extension false positive)", got)
	}
}

func TestDiscoverAndLoad_RootOutsideWorkspaceIsNotScanned(t *testing.T) {
	outer := t.TempDir()
	root := outer + "/repo"
	mustWrite(t, outer+"/shared/lib/component.yaml", validYAML("lib"))
	mustWrite(t, root+"/intent.yaml", "discovery:\n  roots: [../shared, apps]\n")
	mustWrite(t, root+"/apps/api/component.yaml", validYAML("api"))

	dr, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("DiscoverAndLoad: %v", err)
	}
	if got := strings.Join(rootsTestPaths(dr), ","); got != "apps/api/component.yaml" {
		t.Fatalf("manifests = %q, want only the in-workspace root", got)
	}
}

func TestDiscoverAndLoad_MissingRootIsIntentInvalid(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/intent.yaml", "discovery:\n  roots: [services/]\n")
	mustWrite(t, root+"/apps/api/component.yaml", validYAML("api"))

	_, err := DiscoverAndLoad(context.Background(), Options{WorkspaceRoot: root})
	var ii *ErrIntentInvalid
	if !errors.As(err, &ii) {
		t.Fatalf("err = %v, want *ErrIntentInvalid", err)
	}
	if !strings.Contains(ii.Reason, "services/") {
		t.Errorf("reason should name the root: %q", ii.Reason)
	}
}

func TestManifestSetDigest_FollowsDiscoveryRoots(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/intent.yaml", "discovery:\n  roots: [apps/]\n")
	mustWrite(t, root+"/apps/api/component.yaml", validYAML("api"))
	ctx := context.Background()

	before, err := ManifestSetDigest(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	// A manifest outside the roots is invisible to the catalog, so it must
	// not move the digest the freshness gate keys on.
	mustWrite(t, root+"/variations/a/apps/api/component.yaml", validYAML("api"))
	outside, err := ManifestSetDigest(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if outside != before {
		t.Errorf("digest moved for a manifest outside discovery.roots")
	}
	// One inside does.
	mustWrite(t, root+"/apps/web/component.yaml", validYAML("web"))
	inside, err := ManifestSetDigest(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	if inside == before {
		t.Errorf("digest did not move for a manifest inside discovery.roots")
	}
}
