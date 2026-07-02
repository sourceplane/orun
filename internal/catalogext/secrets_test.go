package catalogext

import (
	"encoding/json"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestValidateSecretsFacet_AcceptsWellFormedRequirements(t *testing.T) {
	block := map[string]any{
		"requirements": []map[string]any{
			{"key": "DATABASE_URL", "profile": "worker-deploy", "required": true},
			{"key": "STRIPE_KEY", "profile": "worker-deploy", "required": true},
		},
	}
	if err := ValidateSecretsFacet(block); err != nil {
		t.Fatalf("well-formed requirements block should validate: %v", err)
	}
}

func TestValidateSecretsFacet_AcceptsLiveKeys(t *testing.T) {
	// The block is shaped so the live half (bindings/rotation/syncs) validates too.
	block := map[string]any{
		"requirements": []map[string]any{{"key": "DATABASE_URL", "profile": "p", "required": true}},
		"bindings":     []map[string]any{{"key": "DATABASE_URL", "env": "prod", "status": "bound", "version": 9, "servesFrom": "environment"}},
		"rotation":     []map[string]any{{"key": "DATABASE_URL", "env": "prod", "rotatedAt": "2026-04-02", "ageDays": 91}},
		"syncs":        []map[string]any{{"key": "DATABASE_URL", "env": "prod", "target": "cloudflare-worker", "entityRef": "Resource/x", "version": 9, "status": "synced"}},
	}
	if err := ValidateSecretsFacet(block); err != nil {
		t.Fatalf("well-formed full block should validate: %v", err)
	}
}

func TestValidateSecretsFacet_RejectsValueShapedField(t *testing.T) {
	block := map[string]any{
		"requirements": []map[string]any{
			{"key": "DATABASE_URL", "profile": "p", "required": true, "value": "postgres://leaked"},
		},
	}
	if err := ValidateSecretsFacet(block); err == nil {
		t.Fatal("a value-shaped field must be rejected (Invariant 1)")
	}
}

func TestValidateSecretsFacet_RejectsUnknownTopLevelKey(t *testing.T) {
	block := map[string]any{"ciphertext": "AAAA"}
	if err := ValidateSecretsFacet(block); err == nil {
		t.Fatal("an unknown top-level key must be rejected")
	}
}

func TestDeriveComponentSecretRequirements_UnionOfTwoProfiles(t *testing.T) {
	profiles := map[string]model.ExecutionProfile{
		"worker-deploy": {
			SecretBindings: map[string]model.SecretBinding{
				"DATABASE_URL": {Required: true},
				"STRIPE_KEY":   {Required: true},
			},
		},
		"worker-preview": {
			SecretBindings: map[string]model.SecretBinding{
				"DATABASE_URL": {Required: false},
				"SENTRY_DSN":   {Required: false},
			},
		},
	}
	got := DeriveComponentSecretRequirements(profiles)

	want := []SecretRequirement{
		{Key: "DATABASE_URL", Profile: "worker-deploy", Required: true},
		{Key: "STRIPE_KEY", Profile: "worker-deploy", Required: true},
		{Key: "DATABASE_URL", Profile: "worker-preview", Required: false},
		{Key: "SENTRY_DSN", Profile: "worker-preview", Required: false},
	}
	if len(got) != len(want) {
		t.Fatalf("expected %d requirements, got %d: %+v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("requirement[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}

	// The derived facet round-trips through its own validator.
	facet := AttachSecretsFacet(nil, got)
	raw, _ := json.Marshal(facet[SecretsExtensionKey])
	var block any
	if err := json.Unmarshal(raw, &block); err != nil {
		t.Fatalf("unmarshal facet: %v", err)
	}
	if err := ValidateSecretsFacet(block); err != nil {
		t.Fatalf("derived facet should validate: %v", err)
	}
}

func TestDeriveComponentSecretRequirements_NoBindingsYieldsNil(t *testing.T) {
	profiles := map[string]model.ExecutionProfile{"p": {Jobs: map[string]model.ProfileJobSpec{"deploy": {}}}}
	if got := DeriveComponentSecretRequirements(profiles); got != nil {
		t.Errorf("expected nil for binding-less profiles, got %+v", got)
	}
}

func TestRegisterStandard_RegistersSecretsValidator(t *testing.T) {
	r := NewRegistry()
	RegisterStandard(r)
	if !r.Known(SecretsExtensionKey) {
		t.Fatal("RegisterStandard should register x-orun-secrets")
	}
	// A value-shaped block surfaces as a validation error through the registry.
	errs := r.Validate(map[string]any{
		SecretsExtensionKey: map[string]any{"requirements": []map[string]any{{"key": "K", "value": "leak"}}},
	})
	if len(errs) == 0 {
		t.Fatal("registry should surface the value-leak validation error")
	}
}
