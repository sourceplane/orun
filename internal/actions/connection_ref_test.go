package actions

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/configsurface"
)

// saas-multi-connections MCX-D7: an account may hold several connections of
// one provider. A blueprint can pin which one it mints from; without a pin,
// nothing changes.

func twoCloudflareAccounts() *fakeConfig {
	return &fakeConfig{conns: []configsurface.Connection{
		{ID: "int_prod", Provider: "cloudflare", Status: "active", DisplayName: strptr("Production"), ExternalAccountLogin: strptr("Acme Infra")},
		{ID: "int_labs", Provider: "cloudflare", Status: "active", DisplayName: strptr("Labs"), ExternalAccountLogin: strptr("Acme Labs")},
		{ID: "int_old", Provider: "cloudflare", Status: "revoked", DisplayName: strptr("Old")},
	}}
}

func TestReconcileWithoutAPinKeepsTheFirstActiveConnection(t *testing.T) {
	f := twoCloudflareAccounts()
	in := reconcileInput(t, map[string]any{"provider": "cloudflare", "template": "workers-deploy", "keys": []any{"K"}})
	res, err := reconcileOn(context.Background(), f, "ws_1", in)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Outputs["connection"] != "int_prod" || f.created[0].Binding.ConnectionID != "int_prod" {
		t.Errorf("an unpinned reconcile must keep the first active connection: %v", res.Outputs)
	}
}

func TestReconcileMintsFromThePinnedConnection(t *testing.T) {
	for _, ref := range []string{"int_labs", "labs", "ACME LABS"} {
		f := twoCloudflareAccounts()
		in := reconcileInput(t, map[string]any{
			"provider": "cloudflare", "template": "workers-deploy", "keys": []any{"K"}, "connection": ref,
		})
		res, err := reconcileOn(context.Background(), f, "ws_1", in)
		if err != nil {
			t.Fatalf("reconcile %q: %v", ref, err)
		}
		if res.Outputs["connection"] != "int_labs" || f.created[0].Binding.ConnectionID != "int_labs" {
			t.Errorf("pin %q should bind int_labs: %v", ref, res.Outputs)
		}
	}
}

func TestReconcileWaitsForAPinnedConnectionNotConnectedYet(t *testing.T) {
	for _, ref := range []string{"Staging", "Old"} {
		f := twoCloudflareAccounts()
		in := reconcileInput(t, map[string]any{
			"provider": "cloudflare", "template": "workers-deploy", "keys": []any{"K"}, "connection": ref,
		})
		res, err := reconcileOn(context.Background(), f, "ws_1", in)
		if err != nil {
			t.Fatalf("a pin not connected yet is a wait, not a failure: %v", err)
		}
		if res.Pending == nil || !strings.Contains(res.Pending.Reason, "Production (int_prod)") {
			t.Errorf("pending should list the active candidates; got %+v", res.Pending)
		}
		if len(f.created) != 0 {
			t.Error("nothing may be minted while the pin is unmatched")
		}
	}
}

func TestReconcileRefusesAnAmbiguousPin(t *testing.T) {
	f := &fakeConfig{conns: []configsurface.Connection{
		{ID: "int_a", Provider: "cloudflare", Status: "active", DisplayName: strptr("Acme")},
		{ID: "int_b", Provider: "cloudflare", Status: "active", ExternalAccountLogin: strptr("acme")},
	}}
	in := reconcileInput(t, map[string]any{
		"provider": "cloudflare", "template": "workers-deploy", "keys": []any{"K"}, "connection": "acme",
	})
	_, err := reconcileOn(context.Background(), f, "ws_1", in)
	if err == nil || !strings.Contains(err.Error(), "pin one by id") {
		t.Fatalf("an ambiguous pin must be refused, never guessed; got %v", err)
	}
	if len(f.created) != 0 {
		t.Error("nothing may be minted on an ambiguous pin")
	}
}

func TestReconcileIgnoresAPinOnAnotherProvider(t *testing.T) {
	f := &fakeConfig{conns: []configsurface.Connection{
		{ID: "int_gh", Provider: "github", Status: "active", ExternalAccountLogin: strptr("labs")},
	}}
	in := reconcileInput(t, map[string]any{
		"provider": "cloudflare", "template": "workers-deploy", "keys": []any{"K"}, "connection": "int_gh",
	})
	res, err := reconcileOn(context.Background(), f, "ws_1", in)
	if err != nil || res.Pending == nil {
		t.Fatalf("a pin naming another provider's connection matches nothing; err=%v pending=%v", err, res.Pending)
	}
}

func TestDoctorRequiresNamedConnections(t *testing.T) {
	f := twoCloudflareAccounts()
	in := doctorInput(t, map[string]any{"providers": []any{"cloudflare"}, "connections": []any{"Labs", "Staging"}})
	res, err := doctorOn(context.Background(), f, "ws_1", in, func(time.Duration) {})
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if res.Pending == nil || res.Outputs["missing"] != "connection Staging" {
		t.Fatalf("an unmatched named connection is missing; got %+v %v", res.Pending, res.Outputs)
	}

	in = doctorInput(t, map[string]any{"providers": []any{"cloudflare"}, "connections": []any{"int_labs", "acme infra"}})
	res, err = doctorOn(context.Background(), f, "ws_1", in, func(time.Duration) {})
	if err != nil || res.Pending != nil {
		t.Fatalf("matched connections should pass; err=%v pending=%v", err, res.Pending)
	}
}
