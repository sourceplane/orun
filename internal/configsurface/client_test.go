package configsurface

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

func newTestClient(t *testing.T, handler http.HandlerFunc) (*Client, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return NewClient(srv.URL, "test", staticToken("tok_test")), srv
}

func TestScopePathConstruction(t *testing.T) {
	cases := []struct {
		scope Scope
		want  string
	}{
		{Scope{Kind: ScopeWorkspace, Org: "org_1"}, "/v1/organizations/org_1/config/secrets"},
		{Scope{Kind: ScopeProject, Org: "org_1", Project: "prj_2"}, "/v1/organizations/org_1/projects/prj_2/config/secrets"},
		{Scope{Kind: ScopeEnvironment, Org: "org_1", Project: "prj_2", EnvID: "env_3"}, "/v1/organizations/org_1/projects/prj_2/environments/env_3/config/secrets"},
	}
	for _, c := range cases {
		got, err := c.scope.secretsPath()
		if err != nil {
			t.Fatalf("secretsPath(%+v): %v", c.scope, err)
		}
		if got != c.want {
			t.Errorf("secretsPath(%+v) = %q, want %q", c.scope, got, c.want)
		}
	}
	// Missing pieces fail loudly.
	if _, err := (Scope{Kind: ScopeEnvironment, Org: "org_1", Project: "prj_2"}).secretsPath(); err == nil {
		t.Error("environment scope without env id must error")
	}
}

func TestListSecretsSendsBearerAndChainParam(t *testing.T) {
	var gotPath, gotAuth, gotQuery string
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth, gotQuery = r.URL.Path, r.Header.Get("Authorization"), r.URL.RawQuery
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"secrets": []any{}}})
	})
	_, _, err := client.ListSecrets(context.Background(), Scope{Kind: ScopeWorkspace, Org: "org_1"}, true)
	if err != nil {
		t.Fatalf("ListSecrets: %v", err)
	}
	if gotPath != "/v1/organizations/org_1/config/secrets" {
		t.Errorf("path = %q", gotPath)
	}
	if gotAuth != "Bearer tok_test" {
		t.Errorf("auth = %q", gotAuth)
	}
	if !strings.Contains(gotQuery, "chain=true") {
		t.Errorf("query = %q, want chain=true", gotQuery)
	}
}

func TestCreateSecretLockedConflictIsTypedAndValueFree(t *testing.T) {
	const secretValue = "postgres://super-secret-value"
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusConflict)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "conflict", "message": "Cannot override a locked secret", "requestId": "req_1"},
		})
	})
	_, err := client.CreateSecret(context.Background(),
		Scope{Kind: ScopeEnvironment, Org: "org_1", Project: "prj_2", EnvID: "env_3"},
		CreateSecretRequest{SecretKey: "DATABASE_URL", Value: secretValue})
	if err == nil {
		t.Fatal("expected locked-key conflict error")
	}
	var apiErr *APIError
	if !asAPIError(err, &apiErr) || !apiErr.IsLocked() {
		t.Fatalf("expected typed locked APIError, got %T: %v", err, err)
	}
	if strings.Contains(err.Error(), secretValue) {
		t.Fatalf("error must never embed the secret value: %v", err)
	}
}

func TestResolveEnvironmentID(t *testing.T) {
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/environments") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"environments": []map[string]any{
			{"id": "env_abc", "slug": "prod"},
			{"id": "env_def", "slug": "dev"},
		}}})
	})
	id, err := client.ResolveEnvironmentID(context.Background(), "org_1", "prj_2", "prod")
	if err != nil {
		t.Fatalf("ResolveEnvironmentID: %v", err)
	}
	if id != "env_abc" {
		t.Errorf("id = %q, want env_abc", id)
	}
	// Unknown slug errors name the available envs, never ids-only confusion.
	_, err = client.ResolveEnvironmentID(context.Background(), "org_1", "prj_2", "staging")
	if err == nil || !strings.Contains(err.Error(), "prod") {
		t.Errorf("unknown-env error should list available slugs, got: %v", err)
	}
}

func TestImportSecretsChunksAt100(t *testing.T) {
	var batches []int
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Secrets []ImportSecret `json:"secrets"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		batches = append(batches, len(body.Secrets))
		results := make([]map[string]any, len(body.Secrets))
		for i, s := range body.Secrets {
			results[i] = map[string]any{"secretKey": s.SecretKey, "status": "created"}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"results": results}})
	})

	items := make([]ImportSecret, 150)
	for i := range items {
		items[i] = ImportSecret{SecretKey: "KEY_" + strings.Repeat("x", 1) + string(rune('A'+i%26)) + itoa(i), Value: "v"}
	}
	results, err := client.ImportSecrets(context.Background(), Scope{Kind: ScopeEnvironment, Org: "org_1", Project: "prj_2", EnvID: "env_3"}, items)
	if err != nil {
		t.Fatalf("ImportSecrets: %v", err)
	}
	if len(results) != 150 {
		t.Errorf("results = %d, want 150", len(results))
	}
	if len(batches) != 2 || batches[0] != 100 || batches[1] != 50 {
		t.Errorf("batches = %v, want [100 50]", batches)
	}
}

func TestSecretPoliciesPathConstruction(t *testing.T) {
	ws, err := (Scope{Kind: ScopeWorkspace, Org: "org_1"}).secretPoliciesPath()
	if err != nil || ws != "/v1/organizations/org_1/config/secret-policies" {
		t.Fatalf("workspace path = %q, err %v", ws, err)
	}
	prj, err := (Scope{Kind: ScopeProject, Org: "org_1", Project: "prj_2"}).secretPoliciesPath()
	if err != nil || prj != "/v1/organizations/org_1/projects/prj_2/config/secret-policies" {
		t.Fatalf("project path = %q, err %v", prj, err)
	}
	// Environment scope is invalid for the policy surface.
	if _, err := (Scope{Kind: ScopeEnvironment, Org: "org_1", Project: "prj_2", EnvID: "env_3"}).secretPoliciesPath(); err == nil {
		t.Error("environment scope must be rejected for secret-policies")
	}
}

func TestPutSecretPolicy(t *testing.T) {
	var gotPath, gotMethod string
	var gotBody PutSecretPolicyRequest
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotMethod = r.URL.Path, r.Method
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"policy": map[string]any{
			"name": "prod-secrets", "tier": "stack", "source": "stack:acme@1.0.0",
			"scope": "organization", "documentHash": "abc123", "updated": true,
		}}})
	})
	res, err := client.PutSecretPolicy(context.Background(),
		Scope{Kind: ScopeWorkspace, Org: "org_1"},
		PutSecretPolicyRequest{Name: "prod-secrets", Tier: "stack", Source: "stack:acme@1.0.0", Document: json.RawMessage(`{"rules":[]}`)})
	if err != nil {
		t.Fatalf("PutSecretPolicy: %v", err)
	}
	if gotMethod != http.MethodPut {
		t.Errorf("method = %q, want PUT", gotMethod)
	}
	if gotPath != "/v1/organizations/org_1/config/secret-policies" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody.Tier != "stack" || gotBody.Name != "prod-secrets" {
		t.Errorf("body = %+v", gotBody)
	}
	if !res.Updated || res.DocumentHash != "abc123" {
		t.Errorf("result = %+v", res)
	}
}

func TestEvaluateSecretPolicy(t *testing.T) {
	var gotPath string
	var gotBody EvaluateSecretPolicyRequest
	client, _ := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{
			"layer1":   map[string]any{"action": "secret.value.use", "allow": true, "reason": "granted"},
			"layer2":   map[string]any{"allow": false, "ruleId": "laptops-never-prod", "reason": "laptops-never-prod"},
			"decision": map[string]any{"allow": false},
		}})
	})
	res, err := client.EvaluateSecretPolicy(context.Background(),
		Scope{Kind: ScopeProject, Org: "org_1", Project: "prj_2"},
		EvaluateSecretPolicyRequest{Key: "STRIPE_KEY", Env: "prod", Platform: "local-cli",
			Subject: EvalSubject{Teams: []string{"platform-admins"}, Kind: "user"}})
	if err != nil {
		t.Fatalf("EvaluateSecretPolicy: %v", err)
	}
	if gotPath != "/v1/organizations/org_1/projects/prj_2/config/secret-policies/evaluate" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody.Key != "STRIPE_KEY" || gotBody.Platform != "local-cli" {
		t.Errorf("body = %+v", gotBody)
	}
	if !res.Layer1.Allow || res.Layer2.Allow || res.Layer2.RuleID != "laptops-never-prod" || res.Decision.Allow {
		t.Errorf("result = %+v", res)
	}
}

func itoa(i int) string {
	return string(rune('0'+i/100)) + string(rune('0'+(i/10)%10)) + string(rune('0'+i%10))
}

// asAPIError mirrors errors.As without importing errors twice in tests.
func asAPIError(err error, target **APIError) bool {
	for err != nil {
		if e, ok := err.(*APIError); ok {
			*target = e
			return true
		}
		type unwrapper interface{ Unwrap() error }
		u, ok := err.(unwrapper)
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
