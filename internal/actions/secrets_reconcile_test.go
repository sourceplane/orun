package actions

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/configsurface"
)

// A product needs secrets under several templates. One hook declares them all
// with `secrets:`: the connections and the existing secrets are read once, each
// missing key is minted under its own template, and the creates run side by
// side.

func cloudflareWorkspace() *fakeConfig {
	return &fakeConfig{conns: []configsurface.Connection{{ID: "int_cf", Provider: "cloudflare", Status: "active"}}}
}

func threeSecrets() map[string]any {
	return map[string]any{
		"provider": "cloudflare",
		"secrets": []any{
			map[string]any{"key": "CLOUDFLARE_API_TOKEN", "template": "workers-deploy"},
			map[string]any{"key": "CLOUDFLARE_D1_TOKEN", "template": "d1-edit"},
			map[string]any{"key": "CLOUDFLARE_ACCOUNT_ID", "template": "account-id"},
		},
	}
}

func TestReconcileSecretsMintsEachUnderItsOwnTemplateInOneHook(t *testing.T) {
	f := cloudflareWorkspace()
	res, err := reconcileOn(context.Background(), f, "ws_1", reconcileInput(t, threeSecrets()))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	got := map[string]string{}
	for _, c := range f.created {
		if c.Binding == nil || c.Binding.ConnectionID != "int_cf" {
			t.Fatalf("every key mints from the connection: %+v", c)
		}
		got[c.SecretKey] = c.Binding.Template
	}
	want := map[string]string{
		"CLOUDFLARE_API_TOKEN": "workers-deploy", "CLOUDFLARE_D1_TOKEN": "d1-edit", "CLOUDFLARE_ACCOUNT_ID": "account-id",
	}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("each key under its own template: got %v, want %v", got, want)
	}
	if res.Outputs["created"] != "CLOUDFLARE_ACCOUNT_ID,CLOUDFLARE_API_TOKEN,CLOUDFLARE_D1_TOKEN" {
		t.Errorf("created = %q", res.Outputs["created"])
	}
	if f.listCalls != 1 || f.secretLists != 1 {
		t.Errorf("the connections and the secrets are read once for the hook: %d and %d reads", f.listCalls, f.secretLists)
	}
}

func TestReconcileCreatesTheMissingSecretsSideBySide(t *testing.T) {
	f := cloudflareWorkspace()
	f.createDelay = 50 * time.Millisecond
	if _, err := reconcileOn(context.Background(), f, "ws_1", reconcileInput(t, threeSecrets())); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if f.maxInFlight != 3 {
		t.Errorf("the three creates should run at once; at most %d were in flight", f.maxInFlight)
	}
}

func TestReconcileSecretsKeepsWhatExists(t *testing.T) {
	f := cloudflareWorkspace()
	f.secrets = []configsurface.SecretMeta{{SecretKey: "CLOUDFLARE_D1_TOKEN"}}
	res, err := reconcileOn(context.Background(), f, "ws_1", reconcileInput(t, threeSecrets()))
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Outputs["kept"] != "CLOUDFLARE_D1_TOKEN" || len(f.created) != 2 {
		t.Errorf("an existing key is kept, never rotated: %v, %d created", res.Outputs, len(f.created))
	}
}

// One create refused: the others still land, the outputs say which, and the
// error names the key that failed with the admin-floor explanation.
func TestReconcileSecretsReportsTheFailedKeyAndKeepsTheRest(t *testing.T) {
	f := cloudflareWorkspace()
	f.createFails = map[string]error{"CLOUDFLARE_D1_TOKEN": fmt.Errorf("not_found")}
	res, err := reconcileOn(context.Background(), f, "ws_1", reconcileInput(t, threeSecrets()))
	if err == nil {
		t.Fatal("a refused create must fail the hook")
	}
	if !strings.Contains(err.Error(), "CLOUDFLARE_D1_TOKEN") || !strings.Contains(err.Error(), "ADMIN") {
		t.Errorf("the error should name the key and the likely cause: %v", err)
	}
	if res.Outputs["created"] != "CLOUDFLARE_ACCOUNT_ID,CLOUDFLARE_API_TOKEN" {
		t.Errorf("the creates that worked are reported: %q", res.Outputs["created"])
	}
}

func TestReconcileTakesOneFormNotBoth(t *testing.T) {
	cases := map[string]map[string]any{
		"both forms": {"provider": "cloudflare", "keys": []any{"K"}, "template": "t",
			"secrets": []any{map[string]any{"key": "K2", "template": "t"}}},
		"neither":               {"provider": "cloudflare"},
		"keys without template": {"provider": "cloudflare", "keys": []any{"K"}},
		"item without template": {"provider": "cloudflare", "secrets": []any{map[string]any{"key": "K"}}},
		"item without key":      {"provider": "cloudflare", "secrets": []any{map[string]any{"template": "t"}}},
	}
	for name, with := range cases {
		if err := Validate("orun.integrations/reconcile@v1", with); err == nil {
			t.Errorf("%s: expected a refusal", name)
		}
	}
	for name, with := range map[string]map[string]any{
		"keys with template": {"provider": "cloudflare", "keys": []any{"K"}, "template": "t"},
		"secrets":            threeSecrets(),
	} {
		if err := Validate("orun.integrations/reconcile@v1", with); err != nil {
			t.Errorf("%s: %v", name, err)
		}
	}
}
