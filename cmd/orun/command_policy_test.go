package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/configsurface"
)

func TestParseAsSubject(t *testing.T) {
	cases := []struct {
		in       string
		wantID   string
		wantKind string
		wantTeam []string
		wantErr  bool
	}{
		{"user:u_123", "u_123", "user", nil, false},
		{"service_principal:sk_9", "sk_9", "service_principal", nil, false},
		{"workflow", "", "workflow", nil, false},
		{"team:platform-admins", "", "user", []string{"platform-admins"}, false},
		{"team:a,team:b", "", "user", []string{"a", "b"}, false},
		{"*authenticated", "", "user", nil, false},
		{"", "", "", nil, true},
		{"group:admins", "", "", nil, true},
		{"user:", "", "", nil, true},
	}
	for _, c := range cases {
		got, err := parseAsSubject(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("parseAsSubject(%q) expected error", c.in)
			}
			continue
		}
		if err != nil {
			t.Errorf("parseAsSubject(%q): %v", c.in, err)
			continue
		}
		if got.ID != c.wantID || got.Kind != c.wantKind || !equalStrings(got.Teams, c.wantTeam) {
			t.Errorf("parseAsSubject(%q) = %+v, want id=%q kind=%q teams=%v", c.in, got, c.wantID, c.wantKind, c.wantTeam)
		}
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRenderTestDecision(t *testing.T) {
	res := &configsurface.EvaluateResult{}
	res.Layer1 = configsurface.LayerDecision{Action: "secret.value.use", Allow: true, Reason: "granted"}
	res.Layer2 = configsurface.LayerDecision{Allow: false, RuleID: "laptops-never-prod", Reason: "laptops-never-prod"}
	res.Decision.Allow = false
	out := renderTestDecision("secret://acme/api/prod/DB_URL", "team:platform-admins", res)
	for _, want := range []string{
		"secret://acme/api/prod/DB_URL as team:platform-admins",
		"Layer 1 (RBAC)   allow  action=secret.value.use reason=granted",
		"Layer 2 (policy) deny  ruleId=laptops-never-prod",
		"=> DENY",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("render missing %q in:\n%s", want, out)
		}
	}

	// A Layer-2 allow with no matching rule renders (none) and ALLOW.
	res2 := &configsurface.EvaluateResult{}
	res2.Layer1 = configsurface.LayerDecision{Allow: true, Reason: "granted"}
	res2.Layer2 = configsurface.LayerDecision{Allow: true, Reason: "rbac-only"}
	res2.Decision.Allow = true
	out2 := renderTestDecision("secret://acme/api/dev/DB_URL", "user:u1", res2)
	if !strings.Contains(out2, "ruleId=(none)") || !strings.Contains(out2, "=> ALLOW") {
		t.Errorf("rbac-only render unexpected:\n%s", out2)
	}
}

func TestPolicyLintExitCode(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "policies"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A vocabulary-invalid overlay must make lint exit non-zero.
	bad := `
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: bad }
spec:
  rules:
    - id: r
      effect: allow
      scope: { env: "*", key: "*" }
      when: [ "trigger.branch ~ main" ]
`
	if err := os.WriteFile(filepath.Join(dir, "policies", "bad.SecretPolicy.yaml"), []byte(bad), 0o644); err != nil {
		t.Fatal(err)
	}

	saveFile, saveRoot := intentFile, intentRoot
	intentFile, intentRoot = "", dir
	defer func() { intentFile, intentRoot = saveFile, saveRoot }()

	if err := runPolicyLint(); err == nil {
		t.Fatal("runPolicyLint should return an error on a vocabulary-invalid document")
	}

	// A clean overlay exits zero.
	good := `
apiVersion: orun.io/v1
kind: SecretPolicy
metadata: { name: good }
spec:
  rules:
    - id: deny-laptop
      effect: deny
      scope: { env: prod, key: "*" }
      when: [ 'platform == "local-cli"' ]
`
	if err := os.WriteFile(filepath.Join(dir, "policies", "bad.SecretPolicy.yaml"), []byte(good), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := runPolicyLint(); err != nil {
		t.Fatalf("runPolicyLint on a clean overlay: %v", err)
	}
}
