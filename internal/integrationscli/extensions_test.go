package integrationscli

// ICL3: the native-extension seam — mount after served verbs, served wins on
// collision (debug-logged), and the first extension: the provider recipe
// printer over the cached descriptor.

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/configsurface"
	"github.com/spf13/cobra"
)

func TestExtensionMountsUnderProviderTree(t *testing.T) {
	resetExtensions()
	t.Cleanup(resetExtensions)
	ran := false
	RegisterExtension("cloudflare", &cobra.Command{
		Use:  "verify-token",
		RunE: func(cmd *cobra.Command, args []string) error { ran = true; return nil },
	})
	deps, _ := testDeps(t)
	err := execTree(t, loadFixtureRegistry(t), deps, "cloudflare", "verify-token")
	if err != nil {
		t.Fatalf("execute extension: %v", err)
	}
	if !ran {
		t.Fatal("extension did not run")
	}
}

// On a path collision the served verb wins: the extension is NOT mounted and
// the shadowing is named at debug — server truth is never silently overridden.
func TestExtensionCollisionServedWinsAndLogs(t *testing.T) {
	resetExtensions()
	t.Cleanup(resetExtensions)
	extRan := false
	RegisterExtension("cloudflare", &cobra.Command{
		Use:  "health", // collides with the derived core verb
		RunE: func(cmd *cobra.Command, args []string) error { extRan = true; return nil },
	})
	var debugLog bytes.Buffer
	var invocations []*Invocation
	deps := Deps{
		Exec: func(cmd *cobra.Command, inv *Invocation) error {
			invocations = append(invocations, inv)
			return nil
		},
		Debugf: func(format string, args ...interface{}) {
			fmt.Fprintf(&debugLog, format+"\n", args...)
		},
	}
	err := execTree(t, loadFixtureRegistry(t), deps, "cloudflare", "health")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if extRan {
		t.Fatal("shadowed extension must not run — served wins")
	}
	if len(invocations) != 1 || invocations[0].Op != "integrations.connectionHealth" {
		t.Fatalf("served verb did not run: %+v", invocations)
	}
	log := debugLog.String()
	for _, want := range []string{"health", "cloudflare", "shadowed", "not mounted"} {
		if !strings.Contains(log, want) {
			t.Errorf("debug log missing %q: %q", want, log)
		}
	}
}

func TestRecipeCommandPrintsCachedRecipe(t *testing.T) {
	cache := &CachedRegistry{
		Org:       "org_1",
		FetchedAt: time.Now(),
		Registry:  loadFixtureRegistry(t),
	}
	cmd := NewRecipeCommand("cloudflare", func() *CachedRegistry { return cache })
	var out bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("recipe: %v", err)
	}
	got := out.String()
	for _, want := range []string{
		"Create an API token scoped for Orun:",
		"token permissions:",
		"Account.Workers Scripts:Edit: deploy Workers",
		"Zone.DNS:Edit: manage DNS records",
		"links:",
		"Create token: https://dash.example.test/profile/api-tokens",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("recipe output missing %q:\n%s", want, got)
		}
	}
}

func TestRecipeCommandGracefulWithoutCache(t *testing.T) {
	cmd := NewRecipeCommand("cloudflare", func() *CachedRegistry { return nil })
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "no cached integration registry") ||
		!strings.Contains(err.Error(), "orun integrations sync") {
		t.Errorf("expected the graceful no-cache message, got: %v", err)
	}
	// A cache without the provider or without a recipe also degrades politely.
	cache := &CachedRegistry{Org: "org_1", FetchedAt: time.Now(),
		Registry: []configsurface.IntegrationDescriptor{{Provider: "github", Status: "live"}}}
	cmd = NewRecipeCommand("cloudflare", func() *CachedRegistry { return cache })
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "not in the cached registry") {
		t.Errorf("expected the missing-provider message, got: %v", err)
	}
	cmd = NewRecipeCommand("github", func() *CachedRegistry { return cache })
	cmd.SilenceUsage = true
	cmd.SilenceErrors = true
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "declares no connect recipe") {
		t.Errorf("expected the no-recipe message, got: %v", err)
	}
}
