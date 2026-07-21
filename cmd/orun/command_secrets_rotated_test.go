package main

// provider-rotated-secrets RS4: the create-from-parent flag parsing. The CLI
// never reads a value for --from-broker; the server mints v1 from the
// connected parent. These tests pin the flag grammar and its guardrails.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/configsurface"
)

func TestBuildRotationBindingHappyPath(t *testing.T) {
	b, err := buildRotationBinding("cloudflare/workers-deploy", "int_"+strings.Repeat("cd", 16), 3600, "cloudflare-worker:api-prod")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.Template != "workers-deploy" {
		t.Fatalf("template = %q", b.Template)
	}
	if b.ConnectionID != "int_"+strings.Repeat("cd", 16) {
		t.Fatalf("connectionId = %q", b.ConnectionID)
	}
	if b.GraceSeconds == nil || *b.GraceSeconds != 3600 {
		t.Fatalf("graceSeconds = %v", b.GraceSeconds)
	}
	if b.DeliverTarget != "cloudflare-worker:api-prod" {
		t.Fatalf("deliverTarget = %q", b.DeliverTarget)
	}
}

func TestBuildRotationBindingOmitsZeroGrace(t *testing.T) {
	b, err := buildRotationBinding("cloudflare/workers-deploy", "int_"+strings.Repeat("ab", 16), 0, "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if b.GraceSeconds != nil {
		t.Fatalf("zero grace must be omitted (server default), got %v", *b.GraceSeconds)
	}
	// Wire shape: grace + deliverTarget absent, connection + template present.
	raw, err := json.Marshal(b)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	if strings.Contains(s, "graceSeconds") || strings.Contains(s, "deliverTarget") {
		t.Fatalf("optional fields must be omitted when unset: %s", s)
	}
	if !strings.Contains(s, `"template":"workers-deploy"`) {
		t.Fatalf("template missing: %s", s)
	}
}

func TestBuildRotationBindingRejectsBadInputs(t *testing.T) {
	cases := []struct {
		name          string
		fromBroker    string
		connection    string
		grace         int
		wantErrSubstr string
	}{
		{"missing template half", "cloudflare", "int_" + strings.Repeat("cd", 16), 0, "provider/template"},
		{"empty provider half", "/workers-deploy", "int_" + strings.Repeat("cd", 16), 0, "provider/template"},
		{"missing connection", "cloudflare/workers-deploy", "", 0, "--connection"},
		{"malformed connection", "cloudflare/workers-deploy", "conn-123", 0, "int_"},
		{"negative grace", "cloudflare/workers-deploy", "int_" + strings.Repeat("cd", 16), -1, "non-negative"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := buildRotationBinding(tc.fromBroker, tc.connection, tc.grace, "")
			if err == nil {
				t.Fatalf("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErrSubstr) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.wantErrSubstr)
			}
		})
	}
}

func TestCreateSecretRequestOmitsValueForRotatedCreate(t *testing.T) {
	// A rotated create must serialize with NO value key at all — the server
	// rejects rotation+value as mutually exclusive.
	req := configsurface.CreateSecretRequest{
		SecretKey: "CF_TOKEN",
		Rotation: &configsurface.SecretRotationBinding{
			ConnectionID: "int_" + strings.Repeat("cd", 16),
			Template:     "workers-deploy",
		},
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	if strings.Contains(s, `"value"`) {
		t.Fatalf("value key must be absent on a rotated create: %s", s)
	}
	if !strings.Contains(s, `"rotation"`) {
		t.Fatalf("rotation binding missing: %s", s)
	}
}
