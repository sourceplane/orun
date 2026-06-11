package catalogmodel_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

func TestNormalizeEntityKind(t *testing.T) {
	cases := map[string]string{
		catalogmodel.EntityKindOwner:     catalogmodel.EntityKindGroup, // legacy alias
		catalogmodel.EntityKindComponent: catalogmodel.EntityKindComponent,
		catalogmodel.EntityKindGroup:     catalogmodel.EntityKindGroup,
		"Whatever":                       "Whatever", // unknown passes through
	}
	for in, want := range cases {
		if got := catalogmodel.NormalizeEntityKind(in); got != want {
			t.Errorf("NormalizeEntityKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsEntityKind(t *testing.T) {
	for _, k := range catalogmodel.AllEntityKinds() {
		if !catalogmodel.IsEntityKind(k) {
			t.Errorf("IsEntityKind(%q) = false, want true", k)
		}
	}
	// Legacy alias is accepted; junk is not.
	if !catalogmodel.IsEntityKind(catalogmodel.EntityKindOwner) {
		t.Error("IsEntityKind(Owner) = false, want true (legacy alias)")
	}
	if catalogmodel.IsEntityKind("NotAKind") {
		t.Error("IsEntityKind(NotAKind) = true, want false")
	}
}

func TestAllEntityKindsIsCopy(t *testing.T) {
	a := catalogmodel.AllEntityKinds()
	if len(a) == 0 {
		t.Fatal("AllEntityKinds empty")
	}
	a[0] = "mutated"
	b := catalogmodel.AllEntityKinds()
	if b[0] == "mutated" {
		t.Error("AllEntityKinds returned a shared backing slice")
	}
	// Owner alias must NOT appear in the canonical set.
	for _, k := range b {
		if k == catalogmodel.EntityKindOwner {
			t.Error("AllEntityKinds includes the legacy Owner alias")
		}
	}
}

func TestInverseRelation(t *testing.T) {
	// Every declared forward edge has an inverse, and the inverse's inverse is
	// the original (the relation is an involution).
	pairs := [][2]string{
		{catalogmodel.RelTypeOwnedBy, catalogmodel.RelTypeOwns},
		{catalogmodel.RelTypePartOf, catalogmodel.RelTypeHasPart},
		{catalogmodel.RelTypeDependsOn, catalogmodel.RelTypeDependencyOf},
		{catalogmodel.RelTypeProvidesAPI, catalogmodel.RelTypeAPIProvidedBy},
		{catalogmodel.RelTypeConsumesAPI, catalogmodel.RelTypeAPIConsumedBy},
		{catalogmodel.RelTypeRunsOn, catalogmodel.RelTypeHosts},
		{catalogmodel.RelTypeDeployedTo, catalogmodel.RelTypeHostsDeployment},
		{catalogmodel.RelTypeComposedBy, catalogmodel.RelTypeComposes},
	}
	for _, p := range pairs {
		inv, ok := catalogmodel.InverseRelation(p[0])
		if !ok || inv != p[1] {
			t.Errorf("InverseRelation(%q) = %q,%v want %q,true", p[0], inv, ok, p[1])
		}
		back, ok := catalogmodel.InverseRelation(p[1])
		if !ok || back != p[0] {
			t.Errorf("InverseRelation(%q) = %q,%v want %q,true (involution)", p[1], back, ok, p[0])
		}
	}
	if _, ok := catalogmodel.InverseRelation("notARelation"); ok {
		t.Error("InverseRelation(notARelation) ok = true, want false")
	}
}

func TestUpConvertAPIVersion(t *testing.T) {
	cases := []struct {
		in         string
		want       string
		recognized bool
	}{
		{catalogmodel.APIVersionV1Alpha1, catalogmodel.APIVersionV1, true},
		{catalogmodel.APIVersionV1, catalogmodel.APIVersionV1, true},
		{"orun.io/v2", "orun.io/v2", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, rec := catalogmodel.UpConvertAPIVersion(c.in)
		if got != c.want || rec != c.recognized {
			t.Errorf("UpConvertAPIVersion(%q) = %q,%v want %q,%v", c.in, got, rec, c.want, c.recognized)
		}
	}
	if !catalogmodel.IsCurrentAPIVersion(catalogmodel.APIVersionV1) {
		t.Error("IsCurrentAPIVersion(v1) = false")
	}
	if catalogmodel.IsCurrentAPIVersion(catalogmodel.APIVersionV1Alpha1) {
		t.Error("IsCurrentAPIVersion(v1alpha1) = true")
	}
}

func TestEntityKeyRoundTrip(t *testing.T) {
	key := catalogmodel.FormatEntityKey("default", "orun", "identity-worker")
	if key != "default/orun/identity-worker" {
		t.Fatalf("FormatEntityKey = %q", key)
	}
	if err := catalogmodel.ValidateEntityKey(key); err != nil {
		t.Fatalf("ValidateEntityKey(%q): %v", key, err)
	}
	parts, err := catalogmodel.ParseEntityKey(key)
	if err != nil {
		t.Fatalf("ParseEntityKey(%q): %v", key, err)
	}
	if parts.Tenant != "" || parts.Namespace != "default" || parts.Repo != "orun" || parts.Name != "identity-worker" {
		t.Fatalf("ParseEntityKey = %+v", parts)
	}
}

func TestParseEntityKeyTenant(t *testing.T) {
	// The reserved 4-segment tenant form (S-8) parses with the tenant split out.
	parts, err := catalogmodel.ParseEntityKey("acme/default/orun/identity-worker")
	if err != nil {
		t.Fatalf("ParseEntityKey tenant form: %v", err)
	}
	if parts.Tenant != "acme" || parts.Namespace != "default" || parts.Repo != "orun" || parts.Name != "identity-worker" {
		t.Fatalf("tenant parse = %+v", parts)
	}
}

func TestParseEntityKeyInvalid(t *testing.T) {
	for _, bad := range []string{"", "only-one", "two/segs", "UPPER/orun/x", "a/b/c/d/e"} {
		if _, err := catalogmodel.ParseEntityKey(bad); !errors.Is(err, catalogmodel.ErrInvalidKey) {
			t.Errorf("ParseEntityKey(%q) err = %v, want ErrInvalidKey", bad, err)
		}
		if err := catalogmodel.ValidateEntityKey(bad); !errors.Is(err, catalogmodel.ErrInvalidKey) {
			t.Errorf("ValidateEntityKey(%q) err = %v, want ErrInvalidKey", bad, err)
		}
	}
	if catalogmodel.EntityKeyPattern() == nil {
		t.Error("EntityKeyPattern() = nil")
	}
}

func TestNormalizeOwnerRef(t *testing.T) {
	cases := []struct {
		in       string
		wantKey  string
		wantKind string
	}{
		{"@org/team", "group:org/team", catalogmodel.EntityKindGroup},
		{"team/platform", "group:team/platform", catalogmodel.EntityKindGroup},
		{"platform", "group:platform", catalogmodel.EntityKindGroup},
		{"@alice", "user:alice", catalogmodel.EntityKindUser},
		{"alice@corp.com", "user:alice@corp.com", catalogmodel.EntityKindUser},
		{"group:already", "group:already", catalogmodel.EntityKindGroup},
		{"user:already", "user:already", catalogmodel.EntityKindUser},
		{"  @org/team  ", "group:org/team", catalogmodel.EntityKindGroup},
		{"", "", ""},
	}
	for _, c := range cases {
		k, kind := catalogmodel.NormalizeOwnerRef(c.in)
		if k != c.wantKey || kind != c.wantKind {
			t.Errorf("NormalizeOwnerRef(%q) = %q,%q want %q,%q", c.in, k, kind, c.wantKey, c.wantKind)
		}
	}
}

func TestQualifyEntityKey(t *testing.T) {
	cases := []struct {
		ns, repo, val, want string
	}{
		{"default", "orun", "production", "default/orun/production"},   // bare → qualified
		{"default", "orun", "edge-gateway", "default/orun/edge-gateway"},
		{"default", "orun", "ns2/repo2/api", "ns2/repo2/api"},          // already 3-segment → passthrough
		{"default", "orun", "group:org/team", "group:org/team"},        // typed ref → passthrough
		{"default", "orun", "x-vendor:thing", "x-vendor:thing"},        // any typed ref
		{"", "orun", "production", "default/orun/production"},          // empty ns defaults
		{"default", "orun", "  ", ""},                                  // blank → empty
	}
	for _, c := range cases {
		if got := catalogmodel.QualifyEntityKey(c.ns, c.repo, c.val); got != c.want {
			t.Errorf("QualifyEntityKey(%q,%q,%q) = %q, want %q", c.ns, c.repo, c.val, got, c.want)
		}
	}
}

// TestEntityEnvelopeSerialize asserts a fully-populated envelope canonically
// encodes deterministically and round-trips back to an equal envelope — the
// SC0 "types compile + serialize" gate.
func TestEntityEnvelopeSerialize(t *testing.T) {
	maturity := "gold"
	env := catalogmodel.EntityEnvelope{
		APIVersion: catalogmodel.APIVersionV1,
		Kind:       catalogmodel.EntityKindComponent,
		Identity: catalogmodel.EntityIdentity{
			EntityKey: "default/orun/identity-worker",
			Kind:      catalogmodel.EntityKindComponent,
			Name:      "identity-worker",
			Namespace: "default",
			Repo:      "orun",
			Path:      "apps/identity-worker/component.yaml",
		},
		Metadata: catalogmodel.EntityMetadata{
			Title:  "Identity Worker",
			Labels: map[string]string{"team": "platform"},
			Tags:   []string{"edge"},
		},
		Ownership: catalogmodel.EntityOwnership{
			Owner:    "group:platform-edge",
			Contacts: []catalogmodel.EntityContact{{Type: "slack", Value: "#edge", Primary: true}},
			Source:   catalogmodel.OwnershipSourceCODEOWNERS,
		},
		Lifecycle: catalogmodel.EntityLifecycle{
			Stage:    catalogmodel.LifecycleStageProduction,
			Tier:     "tier-1",
			Maturity: &maturity,
		},
		Spec: catalogmodel.ComponentSpecV1{Type: "worker"},
		Relations: []catalogmodel.EntityRelation{
			{Type: catalogmodel.RelTypeDependsOn, To: "default/orun/auth-svc", ToKind: catalogmodel.EntityKindComponent},
		},
		Contracts: &catalogmodel.EntityContracts{
			Provides: []catalogmodel.APIContract{{API: "default/orun/identity-api", Definition: "openapi"}},
		},
		Integrations: map[string]any{"datadog": map[string]any{"service": "identity-worker"}},
		Docs:         &catalogmodel.EntityDocs{TechDocs: "docs/"},
		Links:        []catalogmodel.EntityLink{{Title: "Dashboard", URL: "https://x"}},
		Provenance:   catalogmodel.EntityProvenance{ManifestHash: "sha256:abc"},
	}

	// Canonical encoding is deterministic across two calls.
	a, err := catalogmodel.CanonicalEncode(env)
	if err != nil {
		t.Fatalf("CanonicalEncode: %v", err)
	}
	b, _ := catalogmodel.CanonicalEncode(env)
	if string(a) != string(b) {
		t.Fatal("CanonicalEncode not deterministic")
	}

	// Round-trips through JSON back into an equal envelope.
	var back catalogmodel.EntityEnvelope
	if err := json.Unmarshal(a, &back); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if back.Identity.EntityKey != env.Identity.EntityKey ||
		back.Ownership.Source != env.Ownership.Source ||
		back.Lifecycle.Maturity == nil || *back.Lifecycle.Maturity != "gold" {
		t.Fatalf("round-trip mismatch: %+v", back)
	}
}
