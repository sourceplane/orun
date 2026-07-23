package integrationscli

// ICL1: descriptor → cobra rendering. The cloudflare fixture must render its
// full tree (derived standard verbs + served verbs, served winning at a
// shared path); a manifest-only change must appear with zero Go changes;
// dormant providers render no tree; unknown ops render but refuse to run.

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/configsurface"
	"github.com/spf13/cobra"
)

// testDeps returns renderer deps that capture executions instead of hitting
// any plane.
func testDeps(t *testing.T) (Deps, *[]*Invocation) {
	t.Helper()
	var got []*Invocation
	deps := Deps{
		SecretCreate: func(provider string) *cobra.Command {
			return &cobra.Command{
				Use:   "create <KEY>",
				Short: "sp5 delegate for " + provider,
				Args:  cobra.ExactArgs(1),
				RunE:  func(cmd *cobra.Command, args []string) error { return nil },
			}
		},
		Exec: func(cmd *cobra.Command, inv *Invocation) error {
			got = append(got, inv)
			return nil
		},
	}
	return deps, &got
}

// execTree registers the provider commands under a fresh root and executes
// args, returning the Execute error.
func execTree(t *testing.T, registry []configsurface.IntegrationDescriptor, deps Deps, args ...string) error {
	t.Helper()
	root := &cobra.Command{Use: "orun", SilenceUsage: true, SilenceErrors: true}
	for _, c := range ProviderCommands(registry, deps) {
		root.AddCommand(c)
	}
	root.SetArgs(args)
	return root.Execute()
}

func findCommand(t *testing.T, parent *cobra.Command, path ...string) *cobra.Command {
	t.Helper()
	cur := parent
	for _, seg := range path {
		var next *cobra.Command
		for _, c := range cur.Commands() {
			if c.Name() == seg {
				next = c
				break
			}
		}
		if next == nil {
			t.Fatalf("command %q not found under %q", strings.Join(path, " "), parent.Name())
		}
		cur = next
	}
	return cur
}

func TestCloudflareFixtureRendersFullTree(t *testing.T) {
	resetExtensions()
	deps, _ := testDeps(t)
	cf := BuildProviderCommand(fixtureDescriptor(t, "cloudflare"), deps)
	if cf == nil {
		t.Fatal("live provider must render")
	}
	if cf.Name() != "cloudflare" || !strings.Contains(cf.Short, "Workers, Pages, DNS") {
		t.Errorf("provider command = %q / %q", cf.Name(), cf.Short)
	}
	// Core derived verbs.
	findCommand(t, cf, "connections", "list")
	findCommand(t, cf, "connections", "get")
	findCommand(t, cf, "connections", "revoke")
	findCommand(t, cf, "health")
	// Capability-derived: secrets → secret create (SP5 delegate),
	// credential-broker → templates list + credentials list/revoke.
	create := findCommand(t, cf, "secret", "create")
	if !strings.Contains(create.Short, "sp5 delegate") {
		t.Errorf("secret create must be the SP5 delegate, got Short=%q", create.Short)
	}
	findCommand(t, cf, "templates", "list")
	findCommand(t, cf, "credentials", "list")
	findCommand(t, cf, "credentials", "revoke")
	// Served-only verb.
	audit := findCommand(t, cf, "dns", "audit")
	if audit.Flags().Lookup("zone") == nil {
		t.Error("served verb must register its declared flags")
	}
	// Served wins over the derived verb at the same path: the summary is the
	// served one.
	list := findCommand(t, cf, "connections", "list")
	if list.Short != "List Cloudflare connections (served)" {
		t.Errorf("served verb must override the derived one, got Short=%q", list.Short)
	}
	// Uniform flags: --json everywhere, --yes on mutations only.
	if list.Flags().Lookup("json") == nil {
		t.Error("list must carry --json")
	}
	revoke := findCommand(t, cf, "connections", "revoke")
	if revoke.Flags().Lookup("yes") == nil {
		t.Error("revoke must carry --yes")
	}
	if list.Flags().Lookup("yes") != nil {
		t.Error("list must not carry --yes")
	}
}

func TestProvisionCapabilityDerivesSandboxes(t *testing.T) {
	resetExtensions()
	deps, got := testDeps(t)
	sb := BuildProviderCommand(fixtureDescriptor(t, "supabase"), deps)
	findCommand(t, sb, "sandboxes", "list")
	root := &cobra.Command{Use: "orun", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(sb)
	root.SetArgs([]string{"supabase", "sandboxes", "list"})
	if err := root.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(*got) != 1 || (*got)[0].Op != "integrations.listSandboxes" || (*got)[0].Provider != "supabase" {
		t.Fatalf("invocations = %+v", *got)
	}
}

func TestDormantProviderRendersNoTree(t *testing.T) {
	resetExtensions()
	deps, _ := testDeps(t)
	if cmd := BuildProviderCommand(fixtureDescriptor(t, "aws"), deps); cmd != nil {
		t.Fatalf("dormant provider must render no tree, got %q", cmd.Name())
	}
	cmds := ProviderCommands(loadFixtureRegistry(t), deps)
	for _, c := range cmds {
		if c.Name() == "aws" {
			t.Error("dormant provider mounted")
		}
	}
	if len(cmds) != 4 {
		t.Errorf("live providers = %d, want 4", len(cmds))
	}
}

// A manifest-only change (a new verb on a provider) renders with zero Go
// changes.
func TestManifestOnlyVerbAdditionRenders(t *testing.T) {
	resetExtensions()
	deps, got := testDeps(t)
	d := fixtureDescriptor(t, "github")
	d.CLI = &configsurface.CliNamespace{Verbs: []configsurface.CliVerb{{
		Path:    []string{"runners", "list"},
		Summary: "List registered runners",
		Invoke:  configsurface.CliInvoke{Plane: "integrations", Op: "integrations.listSandboxes"},
	}}}
	err := execTree(t, []configsurface.IntegrationDescriptor{d}, deps, "github", "runners", "list")
	if err != nil {
		t.Fatalf("execute: %v", err)
	}
	if len(*got) != 1 || (*got)[0].Op != "integrations.listSandboxes" {
		t.Fatalf("invocations = %+v", *got)
	}
}

// An op newer than this binary renders (help keeps working) but running it
// reports the needs-a-newer-orun error — never a crash, never a guess.
func TestUnknownOpRendersButRefusesToRun(t *testing.T) {
	resetExtensions()
	deps, got := testDeps(t)
	err := execTree(t, loadFixtureRegistry(t), deps, "cloudflare", "dns", "audit", "--zone", "z1")
	if err == nil {
		t.Fatal("expected the needs-a-newer-orun error")
	}
	for _, want := range []string{"needs a newer orun", "integrations.dnsAudit.v2", "upgrade orun"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%s", want, err)
		}
	}
	if len(*got) != 0 {
		t.Errorf("unknown op must never execute, got %+v", *got)
	}
}

// Typo suggestions speak the secrets-tree "did you mean" dialect over the
// rendered tree (provider level and group level).
func TestUnknownVerbSuggestsOverRenderedTree(t *testing.T) {
	resetExtensions()
	deps, _ := testDeps(t)
	err := execTree(t, loadFixtureRegistry(t), deps, "cloudflare", "connectons")
	if err == nil {
		t.Fatal("expected error")
	}
	for _, want := range []string{`unknown subcommand "connectons"`, "did you mean:", "connections", "available subcommands:"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error missing %q:\n%s", want, err)
		}
	}
	err = execTree(t, loadFixtureRegistry(t), deps, "cloudflare", "connections", "lst")
	if err == nil || !strings.Contains(err.Error(), "did you mean:") || !strings.Contains(err.Error(), "list") {
		t.Errorf("group-level suggestion missing, got: %v", err)
	}
}

func TestBareProviderPrintsHelp(t *testing.T) {
	resetExtensions()
	deps, _ := testDeps(t)
	if err := execTree(t, loadFixtureRegistry(t), deps, "cloudflare"); err != nil {
		t.Fatalf("bare provider must print help, got: %v", err)
	}
}

func TestRenderProviderListing(t *testing.T) {
	out := RenderProviderListing(loadFixtureRegistry(t))
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 6 {
		t.Fatalf("listing lines = %d:\n%s", len(lines), out)
	}
	if !strings.HasPrefix(lines[0], "PROVIDER") || !strings.Contains(lines[0], "CATEGORY") ||
		!strings.Contains(lines[0], "CONNECTED") || !strings.Contains(lines[0], "STATUS") {
		t.Errorf("header = %q", lines[0])
	}
	// Sorted by provider; dormant listed with its status; connected count
	// rendered when projected, dash when not.
	if !strings.HasPrefix(lines[1], "aws") || !strings.Contains(lines[1], "dormant") {
		t.Errorf("aws row = %q", lines[1])
	}
	if !strings.HasPrefix(lines[2], "cloudflare") || !strings.Contains(lines[2], "2") || !strings.Contains(lines[2], "live") {
		t.Errorf("cloudflare row = %q", lines[2])
	}
	if !strings.HasPrefix(lines[5], "supabase") || !strings.Contains(lines[5], "-") {
		t.Errorf("supabase row = %q", lines[5])
	}
}

func TestRegistryStats(t *testing.T) {
	providers, verbs := RegistryStats(loadFixtureRegistry(t))
	if providers != 5 {
		t.Errorf("providers = %d, want 5", providers)
	}
	// cloudflare: 4 core + secret create + 3 broker + 1 served-only (dns
	// audit; connections list overrides) = 9; github: 4 + 3 = 7;
	// slack: 4; supabase: 4 + 1 + 1 = 6; aws dormant: 0.
	if verbs != 26 {
		t.Errorf("verbs = %d, want 26", verbs)
	}
}
