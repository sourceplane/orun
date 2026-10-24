package catalogresolve

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

const manualIntentBase = `catalog:
  namespace: sourceplane
  defaults:
    lifecycle: experimental
    owner: team/platform
`

const manualAutomation = `automation:
  triggerBindings:
    github-push-main:
      on: { provider: github, event: push, branches: [main] }
    github-dispatch:
      on: { provider: github, event: workflow_dispatch, branches: [main] }
      plan:
        scope: selected
        components: inputs.component
        environments: inputs.environment
        requireRule: true
`

// web opts dev (verify) and stage (deploy) in; prod deploys on main only.
const manualWeb = `apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: web
spec:
  type: cloudflare-pages-turbo
  subscribe:
    environments:
      - name: dev
        profile: verify
        profileRules:
          - profile: skip
            when: { triggerRef: github-push-main }
          - profile: verify
            when: { triggerRef: github-dispatch }
      - name: stage
        profile: verify
        profileRules:
          - profile: deploy
            when: { triggerRef: github-push-main }
          - profile: deploy
            when: { triggerRef: github-dispatch }
      - name: prod
        profile: verify
        profileRules:
          - profile: deploy
            when: { triggerRef: github-push-main }
`

// lib opts nothing in.
const manualLib = `apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: lib
spec:
  type: turbo-package
  subscribe:
    environments:
      - name: dev
        profile: verify
`

func writeManualWorkspace(t *testing.T, intent string) string {
	t.Helper()
	root := t.TempDir()
	for path, body := range map[string]string{
		"intent.yaml":             intent,
		"apps/web/component.yaml": manualWeb,
		"apps/lib/component.yaml": manualLib,
	} {
		full := filepath.Join(root, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func resolveManual(t *testing.T, intent string) map[string]*catalogmodel.ComponentManifest {
	t.Helper()
	rc, issues, err := Resolve(context.Background(), Options{WorkspaceRoot: writeManualWorkspace(t, intent), Repo: "demo"})
	if err != nil {
		t.Fatalf("Resolve: %v (issues=%v)", err, issues)
	}
	out := map[string]*catalogmodel.ComponentManifest{}
	for _, m := range rc.Manifests {
		out[m.Identity.Name] = m
	}
	return out
}

func TestResolve_ManualLanes(t *testing.T) {
	got := resolveManual(t, manualIntentBase+manualAutomation)
	web := got["web"].Spec.Environments

	want := map[string]*catalogmodel.ComponentManual{
		"dev":   {Trigger: "github-dispatch", Profile: "verify"},
		"stage": {Trigger: "github-dispatch", Profile: "deploy"},
		"prod":  nil,
	}
	for env, w := range want {
		m := web[env].Manual
		switch {
		case w == nil && m != nil:
			t.Errorf("web.%s: manual = %+v, want none (no rule names the requireRule trigger)", env, m)
		case w != nil && (m == nil || *m != *w):
			t.Errorf("web.%s: manual = %+v, want %+v", env, m, w)
		}
	}
	if web["stage"].Profile != "verify" || !web["stage"].Active {
		t.Errorf("web.stage keeps its base profile and active flag, got %+v", web["stage"])
	}
	if m := got["lib"].Spec.Environments["dev"].Manual; m != nil {
		t.Errorf("lib.dev: manual = %+v, want none", m)
	}
}

func TestResolve_ManualLanes_OnlyRequireRuleTriggers(t *testing.T) {
	// The same rules, but the dispatch binding does not set requireRule: no
	// lane is manual, and every manifest equals one resolved with no
	// automation block at all.
	withoutRequire := manualIntentBase + `automation:
  triggerBindings:
    github-push-main:
      on: { provider: github, event: push, branches: [main] }
    github-dispatch:
      on: { provider: github, event: workflow_dispatch, branches: [main] }
`
	plain := resolveManual(t, manualIntentBase)
	got := resolveManual(t, withoutRequire)
	for name, m := range got {
		for env, e := range m.Spec.Environments {
			if e.Manual != nil {
				t.Errorf("%s.%s: manual = %+v without a requireRule binding", name, env, e.Manual)
			}
		}
		if m.Source.ManifestHash != plain[name].Source.ManifestHash {
			t.Errorf("%s: manifest hash changed by an automation block that opts nothing in", name)
		}
	}
}

func TestResolve_ManualLanes_HashOnlyMovesForOptIns(t *testing.T) {
	plain := resolveManual(t, manualIntentBase)
	manual := resolveManual(t, manualIntentBase+manualAutomation)
	if plain["lib"].Source.ManifestHash != manual["lib"].Source.ManifestHash {
		t.Errorf("lib opts nothing in; its manifest hash must not change")
	}
	if plain["web"].Source.ManifestHash == manual["web"].Source.ManifestHash {
		t.Errorf("web gained manual lanes; its manifest hash should change")
	}
}
