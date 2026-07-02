package configsurface

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

func TestRevealSecretReturnsValueAndVersion(t *testing.T) {
	var gotPath, gotReason string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		var body struct {
			Reason string `json:"reason"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotReason = body.Reason
		_ = json.NewEncoder(w).Encode(map[string]any{
			"data": map[string]any{"secret": map[string]any{"value": "postgres://recovered", "version": 4}},
		})
	})

	revealed, err := client.RevealSecret(context.Background(),
		Scope{Kind: ScopeEnvironment, Org: "org_1", Project: "prj_2", EnvID: "env_3"},
		"sec_abc", "prod outage 2026-07-02")
	if err != nil {
		t.Fatalf("RevealSecret: %v", err)
	}
	if revealed.Value != "postgres://recovered" || revealed.Version != 4 {
		t.Errorf("got %+v", revealed)
	}
	if !strings.HasSuffix(gotPath, "/config/secrets/sec_abc/reveal") {
		t.Errorf("path = %q", gotPath)
	}
	if gotReason != "prod outage 2026-07-02" {
		t.Errorf("reason not forwarded: %q", gotReason)
	}
}

func TestRevealSecretForbiddenIsTyped(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "forbidden", "message": "secret.reveal not granted", "requestId": "req_9"},
		})
	})
	_, err := client.RevealSecret(context.Background(),
		Scope{Kind: ScopeEnvironment, Org: "org_1", Project: "prj_2", EnvID: "env_3"},
		"sec_abc", "why")
	if err == nil {
		t.Fatal("expected a forbidden error for an ungranted reveal")
	}
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || apiErr.Status != http.StatusForbidden {
		t.Fatalf("expected typed 403 APIError, got %T: %v", err, err)
	}
}
