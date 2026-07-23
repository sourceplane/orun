package main

// The dynamic `orun integrations` layer (specs/orun-integrations-cli, ICL0–3):
// provider verb trees rendered at command construction from the org's cached
// Integration Registry read. The CLI carries no catalog — no cache means no
// tree (today's SP5 behavior plus a one-line sync hint), and every execution
// is server-validated regardless of cache state.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/discovery"
	"github.com/sourceplane/orun/internal/integrationscli"
	"github.com/sourceplane/orun/internal/loader"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

const (
	integrationsSyncHint  = "hint: run `orun integrations sync` to fetch the workspace's provider verb trees from the registry"
	integrationsStaleNote = "note: the cached integration registry is older than 24h; run `orun integrations sync` to refresh"
)

// integrationsDynamicState carries the cache the mounted tree was rendered
// from, consulted by the parent command's fallback RunE (listing, unknown
// provider). Per-command, never global, so parallel test trees stay isolated.
type integrationsDynamicState struct {
	cache *integrationscli.CachedRegistry
	stale bool
}

// maybeMountDynamicIntegrations mounts the rendered provider trees when (a)
// this invocation touches the integrations namespace at all — construction
// must stay cheap for every other command — and (b) a workspace resolves
// without network and has a cached registry. Everything here is best-effort
// and non-fatal: any miss leaves the static SP5 surface exactly as shipped.
func maybeMountDynamicIntegrations(integrationsCmd *cobra.Command, state *integrationsDynamicState) {
	if !argvTouchesIntegrations(os.Args) {
		return
	}
	org, orunDir := dynamicIntegrationsScope(os.Args)
	if org == "" {
		return
	}
	cache := integrationscli.LoadCachedRegistry(orunDir, org)
	if cache == nil {
		return
	}
	mountDynamicIntegrations(integrationsCmd, state, cache)
}

// mountDynamicIntegrations renders the cached registry as provider subtrees
// under the integrations command. Split from the maybe- wrapper so tests can
// mount a fabricated cache deterministically.
func mountDynamicIntegrations(integrationsCmd *cobra.Command, state *integrationsDynamicState, cache *integrationscli.CachedRegistry) {
	state.cache = cache
	state.stale = cache.Stale(time.Now())
	registerBuiltinIntegrationExtensions(func() *integrationscli.CachedRegistry { return state.cache })
	deps := integrationscli.Deps{
		SecretCreate: newDynamicSecretCreateCommand,
		Exec: func(cmd *cobra.Command, inv *integrationscli.Invocation) error {
			return runDynamicIntegrationVerb(cmd, inv, state)
		},
		Debugf: integrationsDebugf,
	}
	for _, providerCmd := range integrationscli.ProviderCommands(cache.Registry, deps) {
		integrationsCmd.AddCommand(providerCmd)
	}
}

// builtinIntegrationExtensionsOnce guards the process-wide native-extension
// registrations (ICL3): the cloudflare connect-recipe printer, air-gapped prep
// from the cached descriptor.
var builtinIntegrationExtensionsOnce sync.Once

func registerBuiltinIntegrationExtensions(load func() *integrationscli.CachedRegistry) {
	builtinIntegrationExtensionsOnce.Do(func() {
		integrationscli.RegisterExtension("cloudflare", integrationscli.NewRecipeCommand("cloudflare", load))
	})
}

// argvTouchesIntegrations reports whether this invocation names the
// integrations namespace anywhere (covers `orun integrations …` and shell
// completion's `orun __complete integrations …`).
func argvTouchesIntegrations(argv []string) bool {
	for _, a := range argv[1:] {
		if a == "integrations" {
			return true
		}
	}
	return false
}

// dynamicIntegrationsScope resolves, without network and without failing, the
// workspace whose cache should render and the .orun directory it lives under.
// Precedence mirrors resolveScope (--org argv > ORUN_WORKSPACE/ORUN_ORG >
// intent execution.state > cached RepoLink) — flags are not parsed yet at
// command construction, so --org is read from argv directly.
func dynamicIntegrationsScope(argv []string) (org, orunDir string) {
	root := "."
	var intent *model.Intent
	if cwd, err := os.Getwd(); err == nil {
		if intentPath, intentDir, err := discovery.FindIntentFile(cwd); err == nil {
			root = intentDir
			if loaded, lerr := loader.LoadIntent(intentPath); lerr == nil {
				intent = loaded
			}
		}
	}
	orunDir = filepath.Join(root, ".orun")
	if v := argvFlagValue(argv, "--org"); v != "" {
		return v, orunDir
	}
	if v := preferWorkspace(os.Getenv(workspaceEnvVar), os.Getenv(orgEnvVar)); v != "" {
		return v, orunDir
	}
	if intentOrg, _, _ := intentScope(intent); intentOrg != "" {
		return intentOrg, orunDir
	}
	backendURL := resolveBackendURLWithConfig(intent, "")
	if backendURL != "" {
		if remote, err := currentGitRemoteURL(root); err == nil {
			if fullName := parseGitHubRepoFullName(remote); fullName != "" {
				if link, lerr := cliauth.FindRepoLink(backendURL, remote, fullName); lerr == nil && link != nil {
					return strings.TrimSpace(link.OrgID), orunDir
				}
			}
		}
	}
	return "", orunDir
}

// argvFlagValue scans raw argv for `--name value` / `--name=value`.
func argvFlagValue(argv []string, name string) string {
	for i, a := range argv {
		if a == name && i+1 < len(argv) {
			return strings.TrimSpace(argv[i+1])
		}
		if strings.HasPrefix(a, name+"=") {
			return strings.TrimSpace(a[len(name)+1:])
		}
	}
	return ""
}

// integrationsDebugf logs renderer diagnostics (e.g. a shadowed native
// extension) — debug only, stderr, never part of command output.
func integrationsDebugf(format string, args ...interface{}) {
	if debugMode || os.Getenv("ORUN_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}
}

// newDynamicSecretCreateCommand is the SP5 delegation seam (ICL2 golden bar):
// the rendered `secret create` leaf binds the SAME flag variables and calls
// the SAME runIntegrationsSecretCreate as the static grammar, so authoring
// behavior — preflight, capability validation, wire shape, output, errors —
// is byte-identical with or without a cache.
func newDynamicSecretCreateCommand(provider string) *cobra.Command {
	c := &cobra.Command{
		Use:   "create <KEY>",
		Short: "Create an integration-bound secret minted from a " + provider + " connection",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runIntegrationsSecretCreate(cmd, provider, args[0])
		},
	}
	addSecretsScopeFlags(c)
	addSecretsJSONFlag(c)
	c.Flags().StringVar(&secretsConnection, "connection", "", "Integration connection public id (int_…) the value is minted against (required)")
	c.Flags().StringVar(&integrationsTemplateFlag, "template", "", "Scope template id declared by the provider (required; errors list the declared templates)")
	c.Flags().StringVar(&integrationsModeFlag, "mode", "brokered", "Secret mode: brokered (minted at resolve, never stored) or rotated (stored + re-minted on schedule)")
	c.Flags().StringArrayVar(&integrationsParamFlags, "param", nil, "Template param as key=value (repeatable; the template declares which are required)")
	c.Flags().StringVar(&secretsRotation, "rotation", "", "Rotation cadence for a rotated secret (e.g. 30d)")
	c.Flags().IntVar(&secretsGraceSeconds, "grace-seconds", 0, "Overlap seconds the prior token stays valid after a rotation (rotated mode; default: server 24h)")
	c.Flags().StringVar(&secretsDeliverTarget, "deliver-target", "", "Materialize target re-delivered on rotation for a long-lived consumer (rotated mode)")
	c.Flags().StringVar(&secretsDisplayName, "display-name", "", "Human display name for the key")
	return c
}

// runDynamicIntegrationVerb executes one rendered verb: resolve auth/tenancy
// exactly like every secrets command (flag > env > intent > link), then map
// the invocation through the compiled-in allowlist onto the typed planes.
func runDynamicIntegrationVerb(cmd *cobra.Command, inv *integrationscli.Invocation, state *integrationsDynamicState) error {
	ctx := cmd.Context()
	rt, err := newSecretsRuntime(ctx)
	if err != nil {
		return err
	}
	env := &integrationscli.ExecEnv{
		Client: rt.client,
		Org:    rt.org,
		Stdout: os.Stdout,
		Stderr: os.Stderr,
		Stdin:  os.Stdin,
	}
	if state != nil && state.stale {
		env.StaleNote = integrationsStaleNote
	}
	return integrationscli.ExecuteInvocation(ctx, env, inv)
}

// unknownIntegrationProvider is the typo error for a provider positional when
// a rendered tree exists: suggestions come from the registry (the tree IS the
// help surface), and a dormant/roadmap provider names its status instead.
func unknownIntegrationProvider(cache *integrationscli.CachedRegistry, name string) error {
	if d := cache.Descriptor(name); d != nil && !d.Live() {
		return fmt.Errorf("provider %q is %s — no CLI verbs are served yet; see `orun integrations` for the provider list", name, d.Status)
	}
	var live []string
	for _, d := range cache.Registry {
		if d.Live() {
			live = append(live, d.Provider)
		}
	}
	sort.Strings(live)
	var b strings.Builder
	fmt.Fprintf(&b, "unknown provider %q for \"orun integrations\"", name)
	if suggestion := ui.SuggestMatch(name, append([]string{"sync"}, live...)); suggestion != "" {
		fmt.Fprintf(&b, "\n\ndid you mean:\n  orun integrations %s", suggestion)
	}
	if len(live) > 0 {
		fmt.Fprintf(&b, "\n\nproviders: %s", strings.Join(live, ", "))
	}
	return fmt.Errorf("%s", b.String())
}

// ── sync ─────────────────────────────────────────────────────────────────────

func newIntegrationsSyncCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "sync",
		Short: "Refresh the cached integration registry (provider verb trees) for the linked workspace",
		Long: `Fetch the workspace's Integration Registry and cache it under
.orun/integrations/ so provider namespaces, verbs, help, and completion render
offline. The cache is presentation-only: every invocation is still validated
server-side. Caches go soft-stale after 24h; sync forces a refresh (an ETag
revalidation when the registry is unchanged).`,
		Args: cobra.NoArgs,
		RunE: runIntegrationsSync,
	}
}

func runIntegrationsSync(cmd *cobra.Command, _ []string) error {
	ctx := cmd.Context()
	rt, err := newSecretsRuntime(ctx)
	if err != nil {
		return err
	}
	orunDir := filepath.Join(storeDir(), ".orun")
	etag := ""
	cached := integrationscli.LoadCachedRegistry(orunDir, rt.org)
	if cached != nil {
		etag = cached.ETag
	}
	res, err := rt.client.GetIntegrationRegistryConditional(ctx, rt.org, etag)
	if err != nil {
		return err
	}
	registry := res.Registry
	suffix := ""
	if res.NotModified && cached != nil {
		registry = cached.Registry
		suffix = " (registry unchanged)"
	}
	if err := integrationscli.SaveCachedRegistry(orunDir, rt.org, registry, res.ETag, time.Now()); err != nil {
		return fmt.Errorf("write registry cache: %w", err)
	}
	providers, verbs := integrationscli.RegistryStats(registry)
	color := ui.ColorEnabledForWriter(os.Stdout)
	fmt.Printf("%s synced integration registry for workspace %q: %d providers, %d verbs%s\n",
		ui.Green(color, "✓"), rt.org, providers, verbs, suffix)
	return nil
}
