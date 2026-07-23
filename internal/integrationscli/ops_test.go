package integrationscli

// ICL2: the invoke allowlist executing against a fake HTTP server through the
// real configsurface client — one path per plane (integrations + config),
// plus the confirm gate on mutating ops. Nothing on any path carries a secret
// value.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/configsurface"
)

// Plane is satisfied by the real client — the compile-time seam check.
var _ Plane = (*configsurface.Client)(nil)

type staticToken string

func (s staticToken) Token(context.Context) (string, error) { return string(s), nil }

func newPlaneClient(t *testing.T, handler http.HandlerFunc) *configsurface.Client {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	return configsurface.NewClient(srv.URL, "test", staticToken("tok_test"))
}

func newExecEnv(client Plane) (*ExecEnv, *bytes.Buffer, *bytes.Buffer) {
	var stdout, stderr bytes.Buffer
	return &ExecEnv{
		Client: client,
		Org:    "org_1",
		Stdout: &stdout,
		Stderr: &stderr,
		Stdin:  strings.NewReader(""),
	}, &stdout, &stderr
}

// Integrations plane: listConnections renders the table (and --json) from the
// wire read.
func TestExecuteListConnectionsIntegrationsPlane(t *testing.T) {
	connID := "int_" + strings.Repeat("ab", 16)
	var gotPath, gotQuery, gotAuth string
	client := newPlaneClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotQuery, gotAuth = r.URL.Path, r.URL.RawQuery, r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"connections": []map[string]any{
			{"id": connID, "provider": "cloudflare", "displayName": "prod account", "status": "active",
				"createdAt": "2026-07-01T00:00:00Z"},
		}}})
	})
	env, stdout, _ := newExecEnv(client)
	inv := &Invocation{Provider: "cloudflare", Op: "integrations.listConnections"}
	if err := ExecuteInvocation(context.Background(), env, inv); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotPath != "/v1/organizations/org_1/integrations" || gotQuery != "provider=cloudflare" {
		t.Errorf("request = %s?%s", gotPath, gotQuery)
	}
	if gotAuth != "Bearer tok_test" {
		t.Errorf("auth = %q", gotAuth)
	}
	out := stdout.String()
	for _, want := range []string{"ID", "NAME", "STATUS", connID, "prod account", "active"} {
		if !strings.Contains(out, want) {
			t.Errorf("table missing %q:\n%s", want, out)
		}
	}

	// --json emits the raw metadata rows.
	env, stdout, _ = newExecEnv(client)
	inv.JSON = true
	if err := ExecuteInvocation(context.Background(), env, inv); err != nil {
		t.Fatalf("execute --json: %v", err)
	}
	var rows []configsurface.IntegrationConnection
	if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil {
		t.Fatalf("json output: %v\n%s", err, stdout.String())
	}
	if len(rows) != 1 || rows[0].ID != connID {
		t.Errorf("json rows = %+v", rows)
	}
}

// Config plane: listSecretsByProvider filters workspace metadata to the
// provider's rotation-produced rows.
func TestExecuteListSecretsByProviderConfigPlane(t *testing.T) {
	var gotPath string
	client := newPlaneClient(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"secrets": []map[string]any{
			{"id": "sec_1", "secretKey": "CF_TOKEN", "scope": "organization", "status": "active",
				"rotation": map[string]any{"provider": "cloudflare", "connectionId": "int_x", "template": "workers-deploy"}},
			{"id": "sec_2", "secretKey": "OTHER", "scope": "organization", "status": "active"},
		}}})
	})
	env, stdout, _ := newExecEnv(client)
	inv := &Invocation{Provider: "cloudflare", Op: "config.listSecretsByProvider"}
	if err := ExecuteInvocation(context.Background(), env, inv); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if gotPath != "/v1/organizations/org_1/config/secrets" {
		t.Errorf("path = %q", gotPath)
	}
	out := stdout.String()
	if !strings.Contains(out, "CF_TOKEN") || !strings.Contains(out, "workers-deploy") {
		t.Errorf("output missing the rotated row:\n%s", out)
	}
	if strings.Contains(out, "OTHER") {
		t.Errorf("output must filter foreign rows:\n%s", out)
	}
}

// Mutating ops prompt unless --yes; a declined prompt sends nothing.
func TestExecuteRevokeConnectionConfirmGate(t *testing.T) {
	connID := "int_" + strings.Repeat("cd", 16)
	var deletes int
	client := newPlaneClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			deletes++
		}
		w.WriteHeader(http.StatusNoContent)
	})
	inv := &Invocation{
		Provider: "cloudflare",
		Op:       "integrations.revokeConnection",
		Bind:     map[string]string{"connection": "connectionId"},
		Values:   map[string]ArgValue{"connection": {Str: connID, Set: true}},
	}

	// Declined (default answer): aborted, nothing sent.
	env, _, stderr := newExecEnv(client)
	env.Stdin = strings.NewReader("\n")
	err := ExecuteInvocation(context.Background(), env, inv)
	if err == nil || !strings.Contains(err.Error(), "aborted") || !strings.Contains(err.Error(), "--yes") {
		t.Fatalf("expected the aborted error, got: %v", err)
	}
	if deletes != 0 {
		t.Fatal("declined confirmation must send nothing")
	}
	if !strings.Contains(stderr.String(), "revoke connection "+connID+"? [y/N]") {
		t.Errorf("prompt = %q", stderr.String())
	}

	// Confirmed interactively.
	env, stdout, _ := newExecEnv(client)
	env.Stdin = strings.NewReader("y\n")
	if err := ExecuteInvocation(context.Background(), env, inv); err != nil {
		t.Fatalf("confirmed execute: %v", err)
	}
	if deletes != 1 {
		t.Fatalf("deletes = %d", deletes)
	}
	if !strings.Contains(stdout.String(), "revoked connection "+connID) {
		t.Errorf("output = %q", stdout.String())
	}

	// --yes skips the prompt entirely.
	env, _, stderr = newExecEnv(client)
	yes := *inv
	yes.Yes = true
	if err := ExecuteInvocation(context.Background(), env, &yes); err != nil {
		t.Fatalf("--yes execute: %v", err)
	}
	if deletes != 2 || strings.Contains(stderr.String(), "[y/N]") {
		t.Errorf("--yes must skip the prompt (deletes=%d, stderr=%q)", deletes, stderr.String())
	}
}

// Health derives from the connections read (no dedicated endpoint in the IR0
// fixtures): projected health wins, else status maps.
func TestExecuteConnectionHealthDerivesFromStatus(t *testing.T) {
	client := newPlaneClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"connections": []map[string]any{
			{"id": "int_1", "provider": "cloudflare", "status": "active"},
			{"id": "int_2", "provider": "cloudflare", "status": "error", "health": "broken"},
		}}})
	})
	env, stdout, _ := newExecEnv(client)
	inv := &Invocation{Provider: "cloudflare", Op: "integrations.connectionHealth"}
	if err := ExecuteInvocation(context.Background(), env, inv); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := stdout.String()
	if !strings.Contains(out, "HEALTH") || !strings.Contains(out, "ok") || !strings.Contains(out, "broken") {
		t.Errorf("health table:\n%s", out)
	}
}

// The staleness note prints once to stderr, never stdout (pipes stay clean).
func TestExecuteStaleNoteGoesToStderr(t *testing.T) {
	client := newPlaneClient(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"connections": []any{}}})
	})
	env, stdout, stderr := newExecEnv(client)
	env.StaleNote = "note: stale"
	inv := &Invocation{Provider: "cloudflare", Op: "integrations.listConnections"}
	if err := ExecuteInvocation(context.Background(), env, inv); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if !strings.Contains(stderr.String(), "note: stale") {
		t.Errorf("stderr = %q", stderr.String())
	}
	if strings.Contains(stdout.String(), "note: stale") {
		t.Errorf("stdout must stay clean: %q", stdout.String())
	}
}

func TestExecuteUnknownOpIsTyped(t *testing.T) {
	env, _, _ := newExecEnv(nil)
	err := ExecuteInvocation(context.Background(), env, &Invocation{Op: "integrations.timeTravel"})
	if err == nil || !strings.Contains(err.Error(), "not supported by this orun build") {
		t.Errorf("expected the unsupported-op error, got: %v", err)
	}
}

// A server error surfaces as the typed APIError through the plane call — the
// uniform error decode, never a raw body.
func TestExecuteDecodesTypedServerError(t *testing.T) {
	client := newPlaneClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"error": map[string]any{"code": "not_found", "message": "Connection not found", "requestId": "req_7"},
		})
	})
	env, _, _ := newExecEnv(client)
	inv := &Invocation{
		Provider: "cloudflare",
		Op:       "integrations.getConnection",
		Values:   map[string]ArgValue{"connectionId": {Str: "int_gone", Set: true}},
	}
	err := ExecuteInvocation(context.Background(), env, inv)
	if err == nil {
		t.Fatal("expected error")
	}
	var apiErr *configsurface.APIError
	for e := err; e != nil; {
		if a, ok := e.(*configsurface.APIError); ok {
			apiErr = a
			break
		}
		u, ok := e.(interface{ Unwrap() error })
		if !ok {
			break
		}
		e = u.Unwrap()
	}
	if apiErr == nil || !apiErr.IsNotFound() || !strings.Contains(err.Error(), "req_7") {
		t.Fatalf("expected typed not-found with requestId, got %T: %v", err, err)
	}
}

// The secret-authoring ops never execute generically — they belong to the SP5
// delegate.
func TestSecretCreateOpsRedirect(t *testing.T) {
	env, _, _ := newExecEnv(nil)
	for _, op := range []string{"config.createBrokeredSecret", "config.createRotatedSecret"} {
		err := ExecuteInvocation(context.Background(), env, &Invocation{Provider: "cloudflare", Op: op})
		if err == nil || !strings.Contains(err.Error(), "orun integrations cloudflare secret create") {
			t.Errorf("op %s: expected the delegate redirect, got: %v", op, err)
		}
	}
}
