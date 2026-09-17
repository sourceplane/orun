package actions

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/configsurface"
)

// secretsInput resolves against the action under test — orun.secrets/exists@v1,
// which is what declares `keys` and `project`. Resolving against a different
// spec drops them, and the Input arrives empty.
func secretsInput(t *testing.T, params map[string]any) Input {
	t.Helper()
	resolved, err := Resolve("orun.secrets/exists@v1", params)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return Input{Params: resolved}
}

// THE SCOPE HAS TO NAME A RUNG. configsurface builds its request path by
// switching on Scope.Kind and refuses an empty one, so the Scope these actions
// used to assemble — Org and Project, never Kind — addressed nothing:
//
//	✕ phase "04-workers" precondition "wiring" is not met: hook "wiring"
//	  (orun.secrets/exists@v1): reading secrets for ws_79BDXAZQ:
//	  configsurface: unknown scope kind ""
//
// Neither secrets action could ever have run against a real backend. What let
// that survive was a fake that accepted the Scope and read nothing out of it,
// so these tests read it.
func TestSecretsExistAsksForTheWorkspaceRung(t *testing.T) {
	f := &fakeConfig{secrets: []configsurface.SecretMeta{{SecretKey: "WIRING_D1"}}}
	in := secretsInput(t, map[string]any{"keys": []any{"WIRING_D1"}})

	if _, err := secretsExistOn(context.Background(), f, "ws_79BDXAZQ", in); err != nil {
		t.Fatalf("secretsExistOn = %v", err)
	}
	if f.lastScope.Kind != configsurface.ScopeWorkspace {
		t.Fatalf("scope Kind = %q, want %q", f.lastScope.Kind, configsurface.ScopeWorkspace)
	}
	if f.lastScope.Org != "ws_79BDXAZQ" {
		t.Fatalf("scope Org = %q", f.lastScope.Org)
	}
	if f.lastScope.Project != "" {
		t.Fatalf("no project was named, yet the scope carries %q", f.lastScope.Project)
	}
}

// A hook that names a project means the project rung — and the project has to
// travel with the Kind, because configsurface refuses a project scope with no
// project just as firmly as an empty kind.
func TestSecretsExistAsksForTheProjectRungWhenNamed(t *testing.T) {
	f := &fakeConfig{secrets: []configsurface.SecretMeta{{SecretKey: "WIRING_D1"}}}
	in := secretsInput(t, map[string]any{"keys": []any{"WIRING_D1"}, "project": "prj_acme"})

	if _, err := secretsExistOn(context.Background(), f, "ws_79BDXAZQ", in); err != nil {
		t.Fatalf("secretsExistOn = %v", err)
	}
	if f.lastScope.Kind != configsurface.ScopeProject {
		t.Fatalf("scope Kind = %q, want %q", f.lastScope.Kind, configsurface.ScopeProject)
	}
	if f.lastScope.Project != "prj_acme" {
		t.Fatalf("scope Project = %q, want prj_acme", f.lastScope.Project)
	}
}

// The reconcile action writes with the same scope it reads with; it carried
// the identical defect, so it gets the identical proof.
func TestSecretScopeIsTheRungBothActionsShare(t *testing.T) {
	ws := secretScope("ws_1", secretsInput(t, map[string]any{"keys": []any{"K"}}))
	if ws.Kind != configsurface.ScopeWorkspace || ws.Org != "ws_1" {
		t.Fatalf("workspace rung = %+v", ws)
	}
	pr := secretScope("ws_1", secretsInput(t, map[string]any{"keys": []any{"K"}, "project": "prj_1"}))
	if pr.Kind != configsurface.ScopeProject || pr.Project != "prj_1" {
		t.Fatalf("project rung = %+v", pr)
	}
	// Whitespace is not a project: it must not promote the rung, or the
	// client refuses "project scope is missing the project".
	blank := secretScope("ws_1", secretsInput(t, map[string]any{"keys": []any{"K"}, "project": "   "}))
	if blank.Kind != configsurface.ScopeWorkspace {
		t.Fatalf("a blank project promoted the rung: %+v", blank)
	}
}
