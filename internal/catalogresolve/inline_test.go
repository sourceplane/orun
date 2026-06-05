package catalogresolve

import (
	"context"
	"testing"
)

func names(rc *ResolvedCatalog) map[string]bool {
	m := map[string]bool{}
	for _, c := range rc.Manifests {
		m[c.Identity.Name] = true
	}
	return m
}

func TestResolve_IngestsInlineComponents(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/intent.yaml",
		"catalog:\n  namespace: ns\n"+
			"components:\n"+
			"  - name: foundation\n    type: terraform\n"+
			"  - name: api\n    type: helm\n    path: apps/api\n    dependsOn:\n      - component: foundation\n        include: always\n")
	// A real input file under the inline component's dir, for fingerprinting.
	mustWrite(t, root+"/apps/api/package.json", `{"name":"api"}`)

	rc, _, err := Resolve(context.Background(), Options{WorkspaceRoot: root, Repo: "r"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	n := names(rc)
	if !n["foundation"] || !n["api"] {
		t.Fatalf("inline components not ingested: %v", n)
	}
	// The path-carrying inline component fingerprints its dir; the path-less one
	// is in the catalog but has no fingerprint.
	fpDirs := map[string]bool{}
	for _, fp := range rc.Fingerprints {
		fpDirs[fp.Dir] = true
	}
	if !fpDirs["apps/api"] {
		t.Errorf("inline component with path should fingerprint apps/api: %v", fpDirs)
	}
	// api's dependency on foundation is resolved.
	for _, m := range rc.Manifests {
		if m.Identity.Name == "api" {
			if len(m.Spec.Dependencies.Components) != 1 || m.Spec.Dependencies.Components[0].Include != "always" {
				t.Errorf("api inline dependency not carried: %+v", m.Spec.Dependencies.Components)
			}
		}
	}
}

func TestResolve_InlineEnvironmentsFromSubscribe(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/intent.yaml",
		"catalog:\n  namespace: ns\n"+
			"components:\n  - name: api\n    type: helm\n    subscribe:\n      environments: [dev, stage]\n    change:\n      watches: [env]\n")

	rc, _, err := Resolve(context.Background(), Options{WorkspaceRoot: root, Repo: "r"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for _, m := range rc.Manifests {
		if m.Identity.Name != "api" {
			continue
		}
		if len(m.Spec.Environments) != 2 || !m.Spec.Environments["dev"].Active || !m.Spec.Environments["stage"].Active {
			t.Errorf("inline subscribe envs not resolved: %+v", m.Spec.Environments)
		}
		if m.Spec.Change == nil || len(m.Spec.Change.Watches) != 1 || m.Spec.Change.Watches[0] != "env" {
			t.Errorf("inline change.watches not carried: %+v", m.Spec.Change)
		}
	}
}

func TestResolve_InlineSubscribeMapForm(t *testing.T) {
	// The map form `- {name: dev, profile: smoke}` must parse without breaking the
	// whole intent load (the regression that hid the catalog from --changed).
	root := t.TempDir()
	mustWrite(t, root+"/intent.yaml",
		"catalog:\n  namespace: ns\n"+
			"components:\n  - name: api\n    type: helm\n    subscribe:\n      environments:\n        - name: dev\n          profile: smoke\n        - stage\n")

	rc, _, err := Resolve(context.Background(), Options{WorkspaceRoot: root, Repo: "r"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	for _, m := range rc.Manifests {
		if m.Identity.Name != "api" {
			continue
		}
		if len(m.Spec.Environments) != 2 || !m.Spec.Environments["dev"].Active || !m.Spec.Environments["stage"].Active {
			t.Errorf("mixed string/map subscribe envs not resolved: %+v", m.Spec.Environments)
		}
		if m.Spec.Environments["dev"].Profile != "smoke" {
			t.Errorf("dev profile = %q, want smoke", m.Spec.Environments["dev"].Profile)
		}
	}
}

func TestResolve_DiscoveredWinsOverInline(t *testing.T) {
	root := t.TempDir()
	mustWrite(t, root+"/intent.yaml",
		"catalog:\n  namespace: ns\n"+
			"components:\n  - name: api\n    type: helm\n") // inline api (type helm)
	mustWrite(t, root+"/apps/api/component.yaml",
		"apiVersion: orun.io/v1alpha1\nkind: Component\nmetadata:\n  name: api\nspec:\n  type: worker\n") // discovered api (type worker)

	rc, _, err := Resolve(context.Background(), Options{WorkspaceRoot: root, Repo: "r"})
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	// Exactly one "api" — the discovered one (type worker) wins; no duplicate-key error.
	count, gotType := 0, ""
	for _, m := range rc.Manifests {
		if m.Identity.Name == "api" {
			count++
			gotType = m.Spec.Type
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly one api manifest, got %d", count)
	}
	if gotType != "worker" {
		t.Errorf("discovered api (worker) should win over inline (helm), got %q", gotType)
	}
}

func TestInlineManifests_SkipsEmptyAndDuplicateNames(t *testing.T) {
	intent := &intentFile{Components: []inlineComponent{
		{Name: ""},          // skipped (no name)
		{Name: "a"},
		{Name: "a"},         // dup skipped
		{Name: "discovered"}, // skipped (already discovered)
	}}
	got := inlineManifests(intent, map[string]bool{"discovered": true})
	if len(got) != 1 || got[0].Component.Metadata.Name != "a" {
		t.Fatalf("inlineManifests = %v, want [a]", got)
	}
}

func TestInlineToAuthored_PathlessHasNoSourceFile(t *testing.T) {
	am := inlineToAuthored(inlineComponent{Name: "x", Type: "t"})
	if am.SourceFile != "" {
		t.Errorf("path-less inline → empty SourceFile, got %q", am.SourceFile)
	}
	withPath := inlineToAuthored(inlineComponent{Name: "x", Path: "apps/x"})
	if withPath.SourceFile != "apps/x/component.yaml" {
		t.Errorf("synthetic SourceFile = %q", withPath.SourceFile)
	}
}
