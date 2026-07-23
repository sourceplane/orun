package main

// saas-secrets-platform SP5: the integration-namespaced authoring command.
// These tests pin the positional grammar (`<provider> secret create <KEY>`),
// the local flag preflight, the capability-driven validation (SP-A7 — every
// rejection lists what IS declared), the deprecation replacement command, and
// the value-less wire shape of a brokered create.

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/configsurface"
	"github.com/spf13/cobra"
)

// ── positional grammar ───────────────────────────────────────────────────────

func TestParseIntegrationsSecretArgsHappyPath(t *testing.T) {
	provider, key, err := parseIntegrationsSecretArgs([]string{"cloudflare", "secret", "create", "CF_TOKEN"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if provider != "cloudflare" || key != "CF_TOKEN" {
		t.Fatalf("provider=%q key=%q", provider, key)
	}
}

func TestParseIntegrationsSecretArgsRejectsBadGrammar(t *testing.T) {
	cases := []struct {
		name          string
		args          []string
		wantErrSubstr string
	}{
		{"missing resource", []string{"cloudflare"}, "missing resource"},
		{"missing verb", []string{"cloudflare", "secret"}, "missing verb"},
		{"missing key", []string{"cloudflare", "secret", "create"}, "missing <KEY>"},
		{"unknown resource", []string{"cloudflare", "secrets", "create", "K"}, `unknown resource "secrets"`},
		{"unknown verb", []string{"cloudflare", "secret", "creat", "K"}, `unknown verb "creat"`},
		{"extra arg", []string{"cloudflare", "secret", "create", "K", "extra"}, `unexpected argument "extra"`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := parseIntegrationsSecretArgs(tc.args)
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErrSubstr) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.wantErrSubstr)
			}
		})
	}
}

// A typo'd static word must suggest the real one — the secrets tree's
// "did you mean" dialect extends to the new namespace (SP-A7).
func TestParseIntegrationsSecretArgsSuggestsOnTypo(t *testing.T) {
	_, _, err := parseIntegrationsSecretArgs([]string{"cloudflare", "secrt", "create", "K"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "did you mean:") || !strings.Contains(err.Error(), "secret") {
		t.Fatalf("expected a 'did you mean: secret' suggestion, got:\n%s", err)
	}
	_, _, err = parseIntegrationsSecretArgs([]string{"cloudflare", "secret", "craete", "K"})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "did you mean:") || !strings.Contains(err.Error(), "create") {
		t.Fatalf("expected a 'did you mean: create' suggestion, got:\n%s", err)
	}
}

// The registered tree surfaces grammar errors as non-nil Execute errors
// (non-zero exit), never silent help.
func TestIntegrationsTreeExecuteReturnsErrorOnBadGrammar(t *testing.T) {
	root := &cobra.Command{Use: "orun", SilenceUsage: true, SilenceErrors: true}
	registerIntegrationsCommand(root)
	root.SetArgs([]string{"integrations", "cloudflare", "secrt", "create", "KEY"})
	if err := root.Execute(); err == nil {
		t.Fatal("expected `integrations cloudflare secrt create` to return an error, got nil")
	}
}

// Required-flag failures surface before any auth or network: a missing
// --connection / --template fails fast through the registered tree.
func TestIntegrationsTreeExecuteRequiresConnectionAndTemplate(t *testing.T) {
	run := func(args ...string) error {
		root := &cobra.Command{Use: "orun", SilenceUsage: true, SilenceErrors: true}
		registerIntegrationsCommand(root)
		root.SetArgs(args)
		return root.Execute()
	}
	err := run("integrations", "cloudflare", "secret", "create", "CF_TOKEN")
	if err == nil || !strings.Contains(err.Error(), "--connection") {
		t.Fatalf("expected a missing --connection error, got: %v", err)
	}
	err = run("integrations", "cloudflare", "secret", "create", "CF_TOKEN",
		"--connection", "int_"+strings.Repeat("cd", 16))
	if err == nil || !strings.Contains(err.Error(), "--template") {
		t.Fatalf("expected a missing --template error, got: %v", err)
	}
}

// ── local preflight ──────────────────────────────────────────────────────────

func TestIntegrationsCreatePreflight(t *testing.T) {
	conn := "int_" + strings.Repeat("cd", 16)
	cases := []struct {
		name                string
		key, conn, template string
		mode                string
		grace               int
		deliver             string
		wantErrSubstr       string // empty = expect success
	}{
		{"happy brokered", "CF_TOKEN", conn, "workers-deploy", "brokered", 0, "", ""},
		{"happy rotated", "CF_TOKEN", conn, "workers-deploy", "rotated", 3600, "cloudflare-worker", ""},
		{"bad key", "9BAD", conn, "workers-deploy", "brokered", 0, "", "invalid key"},
		{"missing connection", "CF_TOKEN", "", "workers-deploy", "brokered", 0, "", "--connection"},
		{"malformed connection", "CF_TOKEN", "conn-123", "workers-deploy", "brokered", 0, "", "int_"},
		{"missing template", "CF_TOKEN", conn, "", "brokered", 0, "", "--template"},
		{"bad mode", "CF_TOKEN", conn, "workers-deploy", "rotating", 0, "", "--mode must be brokered or rotated"},
		{"negative grace", "CF_TOKEN", conn, "workers-deploy", "rotated", -1, "", "non-negative"},
		{"grace on brokered", "CF_TOKEN", conn, "workers-deploy", "brokered", 60, "", "--grace-seconds applies to --mode rotated"},
		{"deliver-target on brokered", "CF_TOKEN", conn, "workers-deploy", "brokered", 0, "cloudflare-worker", "--deliver-target applies to --mode rotated"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := integrationsCreatePreflight(tc.key, tc.conn, tc.template, tc.mode, tc.grace, tc.deliver)
			if tc.wantErrSubstr == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.wantErrSubstr) {
				t.Fatalf("error %q does not mention %q", err.Error(), tc.wantErrSubstr)
			}
		})
	}
}

func TestParseTemplateParams(t *testing.T) {
	params, err := parseTemplateParams([]string{"zoneIds=z1,z2", "accountId=acc_1"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if params["zoneIds"] != "z1,z2" || params["accountId"] != "acc_1" {
		t.Fatalf("params = %v", params)
	}
	// Values may contain '=' (split on the first only).
	params, err = parseTemplateParams([]string{"token=a=b"})
	if err != nil || params["token"] != "a=b" {
		t.Fatalf("params = %v, err = %v", params, err)
	}
	if p, err := parseTemplateParams(nil); err != nil || p != nil {
		t.Fatalf("no flags must yield nil map, got %v, %v", p, err)
	}
	for _, bad := range []string{"zoneIds", "=v", " =v"} {
		if _, err := parseTemplateParams([]string{bad}); err == nil {
			t.Errorf("expected error for %q", bad)
		}
	}
	if _, err := parseTemplateParams([]string{"a=1", "a=2"}); err == nil || !strings.Contains(err.Error(), "more than once") {
		t.Errorf("expected duplicate-param error, got: %v", err)
	}
}

// ── capability validation (SP-A7) ────────────────────────────────────────────

func testCapability() *configsurface.SecretsCapability {
	return &configsurface.SecretsCapability{
		Provider: "cloudflare",
		ScopeTemplates: []configsurface.ScopeTemplate{
			{ID: "workers-deploy", Provider: "cloudflare", Version: 1, DisplayName: "Workers deploy", Params: []string{}, MaxTTLSeconds: 3600},
			{ID: "pages-deploy", Provider: "cloudflare", Version: 1, DisplayName: "Pages deploy", Params: []string{"projectName"}, MaxTTLSeconds: 3600},
			{ID: "zone-dns", Provider: "cloudflare", Version: 2, DisplayName: "Zone DNS", Params: []string{"zoneIds"}, MaxTTLSeconds: 1800, Origin: "custom", Status: "retired"},
		},
		SupportedModes:  []string{"brokered", "rotated"},
		DeliveryTargets: []string{"cloudflare-worker"},
		Authoring:       "custom",
	}
}

func TestFindSecretsCapability(t *testing.T) {
	caps := []configsurface.SecretsCapability{*testCapability(), {Provider: "supabase"}}
	got, err := findSecretsCapability(caps, "cloudflare")
	if err != nil || got == nil || got.Provider != "cloudflare" {
		t.Fatalf("got %v, err %v", got, err)
	}
	// Unknown provider: names the declared sources and suggests a near-miss.
	_, err = findSecretsCapability(caps, "cloudfare")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	for _, want := range []string{`provider "cloudfare" is not a declared secret source`, "did you mean:", "cloudflare", "declared secret sources:", "supabase"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error missing %q:\n%s", want, msg)
		}
	}
	// No capabilities at all: points at connecting an integration.
	_, err = findSecretsCapability(nil, "cloudflare")
	if err == nil || !strings.Contains(err.Error(), "no integration declares a secrets capability") {
		t.Errorf("expected the empty-capabilities hint, got: %v", err)
	}
}

func TestValidateAgainstCapabilityHappyPaths(t *testing.T) {
	cap := testCapability()
	tpl, err := validateAgainstCapability(cap, "brokered", "workers-deploy", nil, "")
	if err != nil || tpl == nil || tpl.ID != "workers-deploy" {
		t.Fatalf("brokered: tpl=%v err=%v", tpl, err)
	}
	tpl, err = validateAgainstCapability(cap, "rotated", "pages-deploy",
		map[string]string{"projectName": "site"}, "cloudflare-worker")
	if err != nil || tpl == nil || tpl.ID != "pages-deploy" {
		t.Fatalf("rotated: tpl=%v err=%v", tpl, err)
	}
	// A rotated create without a deliver target is valid (per-run consumers).
	if _, err = validateAgainstCapability(cap, "rotated", "workers-deploy", nil, ""); err != nil {
		t.Fatalf("rotated without target: %v", err)
	}
}

func TestValidateAgainstCapabilityRejections(t *testing.T) {
	cap := testCapability()
	cases := []struct {
		name          string
		mode, tpl     string
		params        map[string]string
		deliver       string
		wantErrSubstr []string
	}{
		{"unsupported mode", "dynamic", "workers-deploy", nil, "",
			[]string{`does not support --mode dynamic`, "supported modes: brokered, rotated"}},
		{"unknown template", "brokered", "workers-deplo", nil, "",
			[]string{`template "workers-deplo" is not declared`, "did you mean:", "--template workers-deploy", "cloudflare templates: pages-deploy, workers-deploy"}},
		{"retired template rejected for create", "brokered", "zone-dns", map[string]string{"zoneIds": "z1"}, "",
			[]string{`template "zone-dns" is retired`, "existing bindings keep resolving", "cloudflare templates: pages-deploy, workers-deploy"}},
		{"missing required param", "brokered", "pages-deploy", nil, "",
			[]string{`requires params: projectName`, "--param <key>=<value>", "missing: projectName"}},
		{"undeclared param", "brokered", "workers-deploy", map[string]string{"zoneIds": "z1"}, "",
			[]string{`does not declare param(s): zoneIds`, "accepted params: none"}},
		{"bad deliver target", "rotated", "workers-deploy", nil, "supabase-db",
			[]string{`deliver target "supabase-db" is not declared`, "declared targets: cloudflare-worker"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := validateAgainstCapability(cap, tc.mode, tc.tpl, tc.params, tc.deliver)
			if err == nil {
				t.Fatal("expected error")
			}
			for _, want := range tc.wantErrSubstr {
				if !strings.Contains(err.Error(), want) {
					t.Errorf("error missing %q:\n%s", want, err)
				}
			}
		})
	}
	// A provider with no delivery targets says so instead of listing nothing.
	noTargets := testCapability()
	noTargets.DeliveryTargets = nil
	_, err := validateAgainstCapability(noTargets, "rotated", "workers-deploy", nil, "cloudflare-worker")
	if err == nil || !strings.Contains(err.Error(), "declares no delivery targets") {
		t.Errorf("expected the no-targets hint, got: %v", err)
	}
}

// A template with an absent status is active (SP-A6: missing status = active).
func TestScopeTemplateActiveDefaults(t *testing.T) {
	if !(configsurface.ScopeTemplate{ID: "t"}).Active() {
		t.Error("absent status must mean active")
	}
	if (configsurface.ScopeTemplate{ID: "t", Status: "retired"}).Active() {
		t.Error("retired status must not be active")
	}
	if !(configsurface.ScopeTemplate{ID: "t", Status: "active"}).Active() {
		t.Error("explicit active status must be active")
	}
}

// ── deprecation replacement (SP-A7) ──────────────────────────────────────────

func TestBuildReplacementCommandRepresentativeInvocation(t *testing.T) {
	conn := "int_" + strings.Repeat("cd", 16)
	got := buildReplacementCommand(replacementSpec{
		Key:           "CF_TOKEN",
		Provider:      "cloudflare",
		Template:      "workers-deploy",
		Connection:    conn,
		Rotation:      "30d",
		GraceSeconds:  3600,
		DeliverTarget: "cloudflare-worker",
		DisplayName:   "CF deploy token",
		Env:           "prod",
	})
	want := "orun integrations cloudflare secret create CF_TOKEN" +
		" --connection " + conn +
		" --template workers-deploy --mode rotated" +
		" --rotation 30d --grace-seconds 3600 --deliver-target cloudflare-worker" +
		" --display-name 'CF deploy token' --env prod"
	if got != want {
		t.Fatalf("replacement command mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildReplacementCommandOmitsUnpassedFlags(t *testing.T) {
	conn := "int_" + strings.Repeat("ab", 16)
	got := buildReplacementCommand(replacementSpec{
		Key: "CF_TOKEN", Provider: "cloudflare", Template: "workers-deploy",
		Connection: conn, Workspace: true,
	})
	want := "orun integrations cloudflare secret create CF_TOKEN" +
		" --connection " + conn + " --template workers-deploy --mode rotated --workspace"
	if got != want {
		t.Fatalf("replacement command mismatch:\n got: %s\nwant: %s", got, want)
	}
	for _, absent := range []string{"--rotation", "--grace-seconds", "--deliver-target", "--display-name", "--env"} {
		if strings.Contains(got, absent) {
			t.Errorf("unpassed flag %s must not appear: %s", absent, got)
		}
	}
}

// The notice line for a representative legacy invocation carries the exact
// runnable substitute (SP-A7: prints the precise replacement).
func TestFromBrokerDeprecationNoticeExactReplacement(t *testing.T) {
	conn := "int_" + strings.Repeat("cd", 16)
	got := fromBrokerDeprecationNotice(replacementSpec{
		Key: "CF_TOKEN", Provider: "cloudflare", Template: "workers-deploy",
		Connection: conn, GraceSeconds: 3600, DeliverTarget: "cloudflare-worker", Env: "prod",
	})
	want := "deprecated: --from-broker moves to the integration namespace; use " +
		"'orun integrations cloudflare secret create CF_TOKEN --connection " + conn +
		" --template workers-deploy --mode rotated --grace-seconds 3600 --deliver-target cloudflare-worker --env prod'"
	if got != want {
		t.Fatalf("notice mismatch:\n got: %s\nwant: %s", got, want)
	}
}

// ── wire shape ───────────────────────────────────────────────────────────────

// A brokered create must serialize with NO value key at all — value-less
// semantics are structural, mirroring the rotated-create invariant.
func TestCreateSecretRequestOmitsValueForBrokeredCreate(t *testing.T) {
	req := configsurface.CreateSecretRequest{
		SecretKey: "CF_TOKEN",
		Binding: &configsurface.SecretBrokerBinding{
			ConnectionID: "int_" + strings.Repeat("cd", 16),
			Template:     "workers-deploy",
			Params:       map[string]string{"zoneIds": "z1"},
		},
	}
	raw, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(raw)
	if strings.Contains(s, `"value"`) {
		t.Fatalf("value key must be absent on a brokered create: %s", s)
	}
	if !strings.Contains(s, `"binding"`) || !strings.Contains(s, `"template":"workers-deploy"`) {
		t.Fatalf("broker binding missing: %s", s)
	}
	if strings.Contains(s, `"rotation"`) {
		t.Fatalf("rotation must be absent on a brokered create: %s", s)
	}
}
