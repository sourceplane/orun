package main

// orun-integrations-cli ICL1–ICL3, cmd wiring: the dynamic layer mounts
// rendered provider trees from a cached registry without touching the static
// SP5 surface (whose golden tests in command_integrations_test.go run
// unchanged against the no-cache path).

import (
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/configsurface"
	"github.com/sourceplane/orun/internal/integrationscli"
	"github.com/spf13/cobra"
)

func dynamicTestCache() *integrationscli.CachedRegistry {
	entitled := true
	return &integrationscli.CachedRegistry{
		Org:       "org_1",
		FetchedAt: time.Now(),
		Registry: []configsurface.IntegrationDescriptor{
			{
				Provider:     "cloudflare",
				DisplayName:  "Cloudflare",
				Category:     "infrastructure",
				Capabilities: []string{"secrets", "credential-broker"},
				Connected:    2,
				Status:       "live",
				Entitled:     &entitled,
			},
			{
				Provider:     "github",
				DisplayName:  "GitHub",
				Category:     "scm",
				Capabilities: []string{"credential-broker"},
				Status:       "live",
			},
			{
				Provider:    "aws",
				DisplayName: "Amazon Web Services",
				Category:    "infrastructure",
				Status:      "dormant",
			},
		},
	}
}

func newDynamicTestRoot(t *testing.T) *cobra.Command {
	t.Helper()
	root := &cobra.Command{Use: "orun", SilenceUsage: true, SilenceErrors: true}
	integrationsCmd, state := newIntegrationsCommand()
	root.AddCommand(integrationsCmd)
	mountDynamicIntegrations(integrationsCmd, state, dynamicTestCache())
	return root
}

// The mounted tree's `secret create` is the SP5 delegate: the same preflight
// failures fire before any auth or network, byte-identical to the static
// grammar path.
func TestDynamicSecretCreateDelegatesToSP5(t *testing.T) {
	root := newDynamicTestRoot(t)
	root.SetArgs([]string{"integrations", "cloudflare", "secret", "create", "CF_TOKEN"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--connection") {
		t.Fatalf("expected the SP5 missing---connection preflight error, got: %v", err)
	}

	root = newDynamicTestRoot(t)
	root.SetArgs([]string{"integrations", "cloudflare", "secret", "create", "CF_TOKEN",
		"--connection", "int_" + strings.Repeat("cd", 16)})
	err = root.Execute()
	if err == nil || !strings.Contains(err.Error(), "--template") {
		t.Fatalf("expected the SP5 missing---template preflight error, got: %v", err)
	}

	// The static-path invalid-key dialect too.
	root = newDynamicTestRoot(t)
	root.SetArgs([]string{"integrations", "cloudflare", "secret", "create", "9BAD",
		"--connection", "int_" + strings.Repeat("cd", 16), "--template", "workers-deploy"})
	err = root.Execute()
	if err == nil || !strings.Contains(err.Error(), "invalid key") {
		t.Fatalf("expected the SP5 invalid-key error, got: %v", err)
	}
}

// A provider without the secrets capability renders no secret subtree.
func TestDynamicTreeScopesVerbsByCapability(t *testing.T) {
	root := newDynamicTestRoot(t)
	root.SetArgs([]string{"integrations", "github", "secret", "create", "K"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), `unknown subcommand "secret"`) {
		t.Fatalf("expected an unknown-subcommand error for a non-secrets provider, got: %v", err)
	}
}

// An unknown provider under a mounted tree suggests over the registry.
func TestDynamicUnknownProviderSuggests(t *testing.T) {
	root := newDynamicTestRoot(t)
	root.SetArgs([]string{"integrations", "clouflare", "connections", "list"})
	err := root.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{`unknown provider "clouflare"`, "did you mean:", "cloudflare", "providers:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%s", want, err)
		}
	}
}

// A dormant provider is named with its status instead of a typo suggestion.
func TestDynamicDormantProviderNamesStatus(t *testing.T) {
	root := newDynamicTestRoot(t)
	root.SetArgs([]string{"integrations", "aws"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), `provider "aws" is dormant`) {
		t.Fatalf("expected the dormant-status message, got: %v", err)
	}
}

// The cloudflare recipe extension mounts under the rendered tree (ICL3) and
// reads the cached descriptor lazily.
func TestDynamicMountsRecipeExtension(t *testing.T) {
	root := newDynamicTestRoot(t)
	root.SetArgs([]string{"integrations", "cloudflare", "recipe"})
	err := root.Execute()
	// The fabricated cache has no connect recipe for cloudflare: the graceful
	// message proves the extension is mounted and consults the cache.
	if err == nil || !strings.Contains(err.Error(), "declares no connect recipe") {
		t.Fatalf("expected the mounted recipe extension's no-recipe message, got: %v", err)
	}
}

// Without a cache nothing dynamic mounts: the static SP5 grammar handles the
// provider positional exactly as shipped (the golden tests pin its dialect).
func TestNoCacheKeepsStaticBehavior(t *testing.T) {
	root := &cobra.Command{Use: "orun", SilenceUsage: true, SilenceErrors: true}
	integrationsCmd, _ := newIntegrationsCommand()
	root.AddCommand(integrationsCmd)
	root.SetArgs([]string{"integrations", "cloudflare", "secrt", "create", "K"})
	err := root.Execute()
	if err == nil || !strings.Contains(err.Error(), `unknown resource "secrt"`) {
		t.Fatalf("expected the static SP5 typo error, got: %v", err)
	}
}

// `orun integrations sync` is always registered, cache or not.
func TestSyncSubcommandRegistered(t *testing.T) {
	integrationsCmd, _ := newIntegrationsCommand()
	found := false
	for _, c := range integrationsCmd.Commands() {
		if c.Name() == "sync" {
			found = true
		}
	}
	if !found {
		t.Fatal("sync subcommand not registered")
	}
}

// argv scanning for the mount guard and the --org override.
func TestArgvHelpers(t *testing.T) {
	if !argvTouchesIntegrations([]string{"orun", "integrations", "sync"}) {
		t.Error("integrations invocation not detected")
	}
	if !argvTouchesIntegrations([]string{"orun", "__complete", "integrations", "cl"}) {
		t.Error("completion invocation not detected")
	}
	if argvTouchesIntegrations([]string{"orun", "secrets", "list"}) {
		t.Error("non-integrations invocation must not mount")
	}
	if got := argvFlagValue([]string{"orun", "integrations", "--org", "acme"}, "--org"); got != "acme" {
		t.Errorf("--org value = %q", got)
	}
	if got := argvFlagValue([]string{"orun", "integrations", "--org=acme"}, "--org"); got != "acme" {
		t.Errorf("--org= value = %q", got)
	}
	if got := argvFlagValue([]string{"orun", "integrations"}, "--org"); got != "" {
		t.Errorf("absent --org = %q", got)
	}
}
