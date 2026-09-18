package actions

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/configsurface"
)

// fakeConfig is a workspace's connections and secrets, in memory.
type fakeConfig struct {
	conns      []configsurface.Connection
	secrets    []configsurface.SecretMeta
	connErr    error
	secretErr  error
	createErr  error
	created    []configsurface.CreateSecretRequest
	listCalls  int
	connectsAt int // after this many list calls, the provider becomes active
	// lastScope records what the action ASKED FOR. Taking the Scope and
	// looking at nothing in it is how an unset Kind — which the real client
	// rejects outright — survived every test here.
	lastScope configsurface.Scope
	// envSecrets are the secrets a given environment rung resolves, keyed by
	// its env_… id. When set, ListSecrets answers by scope, the way the real
	// plane does: a workspace read does NOT see an environment's secrets.
	envSecrets map[string][]configsurface.SecretMeta
	scopes     []configsurface.Scope
}

func (f *fakeConfig) ResolveProjectID(_ context.Context, _, project string) (string, error) {
	if project == "" {
		return "", fmt.Errorf("no project")
	}
	return "prj_" + project, nil
}

// As strict as the plane: environments are listed by the project's prj_… id,
// and a slug is not_found. The first version of this fake took any string,
// and the slug it was handed failed the moment it met the real API.
func (f *fakeConfig) ResolveEnvironmentID(_ context.Context, _, project, env string) (string, error) {
	if !strings.HasPrefix(project, "prj_") {
		return "", fmt.Errorf("list environments: Not found (code: not_found)")
	}
	return "env_" + env, nil
}

func (f *fakeConfig) ListConnections(context.Context, string) ([]configsurface.Connection, error) {
	f.listCalls++
	if f.connErr != nil {
		return nil, f.connErr
	}
	if f.connectsAt > 0 && f.listCalls >= f.connectsAt {
		return []configsurface.Connection{{ID: "int_1", Provider: "cloudflare", Status: "active"}}, nil
	}
	return f.conns, nil
}

func (f *fakeConfig) ListSecrets(_ context.Context, scope configsurface.Scope, _ bool) ([]configsurface.SecretMeta, json.RawMessage, error) {
	f.lastScope = scope
	f.scopes = append(f.scopes, scope)
	if f.secretErr != nil {
		return nil, nil, f.secretErr
	}
	if f.envSecrets != nil {
		if scope.Kind == configsurface.ScopeEnvironment {
			return f.envSecrets[scope.EnvID], nil, nil
		}
		return f.secrets, nil, nil
	}
	return f.secrets, nil, nil
}

func (f *fakeConfig) CreateSecret(_ context.Context, scope configsurface.Scope, req configsurface.CreateSecretRequest) (*configsurface.SecretMeta, error) {
	f.lastScope = scope
	if f.createErr != nil {
		return nil, f.createErr
	}
	f.created = append(f.created, req)
	return &configsurface.SecretMeta{SecretKey: req.SecretKey}, nil
}

func doctorInput(t *testing.T, params map[string]any) Input {
	t.Helper()
	resolved, err := Resolve("orun.doctor/check@v1", params)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return Input{Params: resolved}
}

func noSleep(time.Duration) {}

func TestDoctorPassesWhenEveryProviderIsActive(t *testing.T) {
	f := &fakeConfig{conns: []configsurface.Connection{
		{Provider: "github", Status: "active"}, {Provider: "cloudflare", Status: "active"},
	}}
	in := doctorInput(t, map[string]any{"providers": []any{"github", "cloudflare"}})
	res, err := doctorOn(context.Background(), f, "ws_1", in, noSleep)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if res.Pending != nil {
		t.Errorf("everything is connected; nothing should be pending: %v", res.Pending)
	}
}

// A consent a person has not clicked yet is not a broken build.
func TestDoctorReportsPendingRatherThanFailing(t *testing.T) {
	f := &fakeConfig{conns: []configsurface.Connection{{Provider: "github", Status: "active"}}}
	in := doctorInput(t, map[string]any{"providers": []any{"github", "cloudflare"}})
	res, err := doctorOn(context.Background(), f, "ws_1", in, noSleep)
	if err != nil {
		t.Fatalf("a missing consent must not be an error: %v", err)
	}
	if res.Pending == nil {
		t.Fatal("expected pending")
	}
	if !strings.Contains(res.Pending.Reason, "cloudflare") {
		t.Errorf("the reason should name what is missing; got %q", res.Pending.Reason)
	}
	if res.Outputs["missing"] != "cloudflare" {
		t.Errorf("missing should be reported, got %v", res.Outputs)
	}
}

// An inactive connection is not a connection.
func TestDoctorIgnoresANonActiveConnection(t *testing.T) {
	f := &fakeConfig{conns: []configsurface.Connection{{Provider: "cloudflare", Status: "revoked"}}}
	in := doctorInput(t, map[string]any{"providers": []any{"cloudflare"}})
	res, _ := doctorOn(context.Background(), f, "ws_1", in, noSleep)
	if res.Pending == nil {
		t.Error("a revoked connection must not count as connected")
	}
}

func TestDoctorWaitsForAConsentToArrive(t *testing.T) {
	f := &fakeConfig{connectsAt: 3}
	in := doctorInput(t, map[string]any{
		"providers": []any{"cloudflare"}, "waitSeconds": 600, "pollSeconds": 1,
	})
	res, err := doctorOn(context.Background(), f, "ws_1", in, noSleep)
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if res.Pending != nil {
		t.Errorf("the consent arrived while waiting; should have passed: %v", res.Pending)
	}
	if f.listCalls < 3 {
		t.Errorf("expected to poll until connected, polled %d", f.listCalls)
	}
}

// Never mistake "the command did not work" for "not connected yet" — that
// polls forever against a broken credential.
func TestDoctorFailsLoudlyWhenTheReadItselfFails(t *testing.T) {
	f := &fakeConfig{connErr: fmt.Errorf("401 unauthorized")}
	in := doctorInput(t, map[string]any{"providers": []any{"github"}, "waitSeconds": 600})
	_, err := doctorOn(context.Background(), f, "ws_1", in, noSleep)
	if err == nil {
		t.Fatal("a failing read must be an error, never a wait")
	}
	if f.listCalls != 1 {
		t.Errorf("it must not poll on a broken read; polled %d", f.listCalls)
	}
}

func TestSecretsExistsReportsMissingAsPending(t *testing.T) {
	f := &fakeConfig{secrets: []configsurface.SecretMeta{{SecretKey: "WIRING_D1"}}}
	resolved, err := Resolve("orun.secrets/exists@v1", map[string]any{"keys": []any{"WIRING_D1", "WIRING_KV"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	res, err := secretsExistOn(context.Background(), f, "ws_1", Input{Params: resolved})
	if err != nil {
		t.Fatalf("a not-yet-published secret is not an error: %v", err)
	}
	if res.Pending == nil || !strings.Contains(res.Pending.Reason, "WIRING_KV") {
		t.Errorf("expected pending naming the missing key, got %+v", res)
	}
	if res.Outputs["present"] != "WIRING_D1" {
		t.Errorf("present should list what is there, got %v", res.Outputs)
	}
}

func TestSecretsExistsPassesWhenAllPresent(t *testing.T) {
	f := &fakeConfig{secrets: []configsurface.SecretMeta{{SecretKey: "A"}, {SecretKey: "B"}}}
	resolved, _ := Resolve("orun.secrets/exists@v1", map[string]any{"keys": []any{"A", "B"}})
	res, err := secretsExistOn(context.Background(), f, "ws_1", Input{Params: resolved})
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if res.Pending != nil {
		t.Errorf("everything is present; nothing pending: %v", res.Pending)
	}
}

func reconcileInput(t *testing.T, params map[string]any) Input {
	t.Helper()
	resolved, err := Resolve("orun.integrations/reconcile@v1", params)
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	return Input{Params: resolved}
}

func TestReconcileCreatesOnlyWhatIsMissing(t *testing.T) {
	f := &fakeConfig{
		conns:   []configsurface.Connection{{ID: "int_1", Provider: "cloudflare", Status: "active"}},
		secrets: []configsurface.SecretMeta{{SecretKey: "CLOUDFLARE_API_TOKEN"}},
	}
	in := reconcileInput(t, map[string]any{
		"provider": "cloudflare", "template": "workers-deploy",
		"keys": []any{"CLOUDFLARE_API_TOKEN", "CLOUDFLARE_D1_TOKEN"},
	})
	res, err := reconcileOn(context.Background(), f, "ws_1", in)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if res.Outputs["kept"] != "CLOUDFLARE_API_TOKEN" {
		t.Errorf("an existing key must be kept, not rotated: %v", res.Outputs)
	}
	if res.Outputs["created"] != "CLOUDFLARE_D1_TOKEN" {
		t.Errorf("the missing key should be created: %v", res.Outputs)
	}
	if len(f.created) != 1 {
		t.Fatalf("expected exactly one create, got %d", len(f.created))
	}
}

// A brokered secret is a pointer, never a value. This action must be incapable
// of carrying credential material.
func TestReconcileCreatesBrokeredPointersWithNoValue(t *testing.T) {
	f := &fakeConfig{conns: []configsurface.Connection{{ID: "int_9", Provider: "cloudflare", Status: "active"}}}
	in := reconcileInput(t, map[string]any{
		"provider": "cloudflare", "template": "d1-edit", "keys": []any{"CLOUDFLARE_D1_TOKEN"},
	})
	if _, err := reconcileOn(context.Background(), f, "ws_1", in); err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	req := f.created[0]
	if req.Value != "" {
		t.Error("a brokered create must never carry a value")
	}
	if req.Binding == nil || req.Binding.ConnectionID != "int_9" || req.Binding.Template != "d1-edit" {
		t.Errorf("the binding should point at the connection and template, got %+v", req.Binding)
	}
}

func TestReconcileWaitsWhenTheProviderIsNotConnected(t *testing.T) {
	f := &fakeConfig{}
	in := reconcileInput(t, map[string]any{
		"provider": "cloudflare", "template": "workers-deploy", "keys": []any{"K"},
	})
	res, err := reconcileOn(context.Background(), f, "ws_1", in)
	if err != nil {
		t.Fatalf("not connected is a wait, not a failure: %v", err)
	}
	if res.Pending == nil {
		t.Fatal("expected pending")
	}
}

// Resource-hiding masks authorization as not-found; when the reads worked and
// the write did not, say what that almost certainly means.
func TestReconcileExplainsAWriteRefusalAfterSuccessfulReads(t *testing.T) {
	f := &fakeConfig{
		conns:     []configsurface.Connection{{ID: "int_1", Provider: "cloudflare", Status: "active"}},
		createErr: fmt.Errorf("not_found"),
	}
	in := reconcileInput(t, map[string]any{
		"provider": "cloudflare", "template": "workers-deploy", "keys": []any{"K"},
	})
	_, err := reconcileOn(context.Background(), f, "ws_1", in)
	if err == nil {
		t.Fatal("expected the create failure to surface")
	}
	if !strings.Contains(err.Error(), "ADMIN") {
		t.Errorf("the error should name the likely cause; got %v", err)
	}
}

func strptr(s string) *string { return &s }

// The platform sees a repository's pushes and PRs only through the App
// installation on the account that owns it. A workspace whose one GitHub
// connection is a user account's must not pass for a product built under an
// org: found live, where every PR of sourceplane/altocumulus stayed invisible
// to a workspace connected only to `adampullely`.
func TestDoctorGithubCountsOnlyThroughTheOwnersConnection(t *testing.T) {
	f := &fakeConfig{conns: []configsurface.Connection{
		{ID: "int_gh", Provider: "github", Status: "active", ExternalAccountLogin: strptr("adampullely")},
		{ID: "int_cf", Provider: "cloudflare", Status: "active"},
	}}
	in := doctorInput(t, map[string]any{"providers": []any{"github", "cloudflare"}, "githubOwner": "sourceplane"})
	res, err := doctorOn(context.Background(), f, "ws_1", in, func(time.Duration) {})
	if err != nil {
		t.Fatalf("doctor: %v", err)
	}
	if res.Pending == nil {
		t.Fatalf("a connection to another account satisfied github: %v", res.Outputs)
	}
	for _, want := range []string{"github for sourceplane", "connected: adampullely"} {
		if !strings.Contains(res.Pending.Reason, want) {
			t.Errorf("the reason should name %q; got %q", want, res.Pending.Reason)
		}
	}
}

func TestDoctorGithubPassesThroughTheOwnersConnection(t *testing.T) {
	f := &fakeConfig{conns: []configsurface.Connection{
		{ID: "int_user", Provider: "github", Status: "active", ExternalAccountLogin: strptr("adampullely")},
		{ID: "int_org", Provider: "github", Status: "active", ExternalAccountLogin: strptr("SourcePlane")},
	}}
	in := doctorInput(t, map[string]any{"providers": []any{"github"}, "githubOwner": "sourceplane"})
	res, err := doctorOn(context.Background(), f, "ws_1", in, func(time.Duration) {})
	if err != nil || res.Pending != nil {
		t.Fatalf("the owner's connection should satisfy github; err=%v pending=%v", err, res.Pending)
	}
}

// Without githubOwner, any active GitHub connection still counts, as before.
func TestDoctorGithubWithoutAnOwnerIsUnchanged(t *testing.T) {
	f := &fakeConfig{conns: []configsurface.Connection{
		{ID: "int_user", Provider: "github", Status: "active", ExternalAccountLogin: strptr("adampullely")},
	}}
	in := doctorInput(t, map[string]any{"providers": []any{"github"}})
	res, err := doctorOn(context.Background(), f, "ws_1", in, func(time.Duration) {})
	if err != nil || res.Pending != nil {
		t.Fatalf("any github connection should count without an owner; err=%v pending=%v", err, res.Pending)
	}
}

func metas(keys ...string) []configsurface.SecretMeta {
	out := make([]configsurface.SecretMeta, 0, len(keys))
	for _, k := range keys {
		out = append(out, configsurface.SecretMeta{SecretKey: k})
	}
	return out
}

// A job's output secrets are published to the project's ENVIRONMENT rungs.
// Reading the workspace scope never saw them: cirrus's 03-infrastructure parked
// on "secret(s) not published yet: WIRING_CLOUDFLARE_D1, WIRING_CLOUDFLARE_KV"
// with both published on stage and prod.
func TestSecretsExistReadsEachNamedEnvironment(t *testing.T) {
	f := &fakeConfig{envSecrets: map[string][]configsurface.SecretMeta{
		"env_stage": metas("WIRING_CLOUDFLARE_D1", "WIRING_CLOUDFLARE_KV"),
		"env_prod":  metas("WIRING_CLOUDFLARE_D1", "WIRING_CLOUDFLARE_KV"),
	}}
	in := secretsInput(t, map[string]any{
		"keys": []any{"WIRING_CLOUDFLARE_D1", "WIRING_CLOUDFLARE_KV"}, "project": "altocumulus",
		"environments": []any{"stage", "prod"},
	})
	res, err := secretsExistOn(context.Background(), f, "ws_1", in)
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if res.Pending != nil {
		t.Fatalf("published environment secrets read as missing: %s", res.Pending.Reason)
	}
	if len(f.scopes) != 2 || f.scopes[0].Kind != configsurface.ScopeEnvironment || f.scopes[1].EnvID != "env_prod" {
		t.Errorf("should read each environment's rung, read %+v", f.scopes)
	}
}

func TestSecretsExistNamesTheEnvironmentAKeyIsMissingFrom(t *testing.T) {
	f := &fakeConfig{envSecrets: map[string][]configsurface.SecretMeta{
		"env_stage": metas("WIRING_CLOUDFLARE_D1"),
		"env_prod":  metas(),
	}}
	in := secretsInput(t, map[string]any{
		"keys": []any{"WIRING_CLOUDFLARE_D1"}, "project": "altocumulus", "environments": []any{"stage", "prod"},
	})
	res, err := secretsExistOn(context.Background(), f, "ws_1", in)
	if err != nil {
		t.Fatalf("exists: %v", err)
	}
	if res.Pending == nil || !strings.Contains(res.Pending.Reason, "WIRING_CLOUDFLARE_D1 (prod)") || strings.Contains(res.Pending.Reason, "(stage)") {
		t.Fatalf("want pending naming only prod, got %+v", res.Pending)
	}
}

func TestSecretsExistEnvironmentsNeedAProject(t *testing.T) {
	in := secretsInput(t, map[string]any{"keys": []any{"K"}, "environments": []any{"stage"}})
	if _, err := secretsExistOn(context.Background(), &fakeConfig{}, "ws_1", in); err == nil {
		t.Fatal("environments without a project should be refused")
	}
}
