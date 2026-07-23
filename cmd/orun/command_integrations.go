package main

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/sourceplane/orun/internal/configsurface"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

// `orun integrations <provider> secret create <KEY>` — integration-namespaced
// secret authoring (saas-secrets-platform SP5, ownership-model.md Surface 3).
//
// The ownership boundary: `orun secrets` views/manages ALL secrets and creates
// static (human) ones; AUTHORING an integration-bound secret lives under the
// integration's own namespace, mirroring the console. Providers are dynamic —
// the CLI never carries a provider or template catalog. Everything authoring-
// shaped (which providers, which templates, which modes, which delivery
// targets) is derived at runtime from the org's bulk capability read
// (GET …/integrations/secrets-capabilities, SP-A1/SP-A7); validation errors
// list what IS declared so the capability read doubles as the help surface.
//
// Invariant: value-less. No secret value is ever read, sent, or printed here —
// the server mints from the connected parent (at resolve for brokered; once +
// on schedule for rotated).

var (
	integrationsTemplateFlag string
	integrationsModeFlag     string
	integrationsParamFlags   []string
)

// integrationsResources / integrationsVerbs are the STATIC halves of the
// grammar (the provider positional is dynamic). Kept as data so the typo
// suggester and the usage error speak from one list.
var (
	integrationsResources = []string{"secret"}
	integrationsVerbs     = []string{"create"}
)

const integrationsUsageLine = "orun integrations <provider> secret create <KEY> --connection <int_…> --template <id> [--mode brokered|rotated]"

func registerIntegrationsCommand(root *cobra.Command) {
	integrationsCmd := &cobra.Command{
		Use:   "integrations <provider> secret create <KEY>",
		Short: "Integration-owned secret authoring (providers and templates come from the platform, not the CLI)",
		Long: `Author integration-bound secrets in the owning integration's namespace
(saas-secrets-platform SP5). The value is never entered: it is minted from the
provider connection — just-in-time at resolve (--mode brokered) or once + on a
schedule (--mode rotated).

Providers, scope templates, modes, and delivery targets are declared by each
integration and read from the platform at runtime; the CLI carries no catalog.
When a provider, template, mode, or target does not validate, the error lists
what the org's integrations actually declare.

Viewing and lifecycle stay on the substrate: use ` + "`orun secrets list/rotate/\nreveal/revoke/versions`" + ` for any secret, of any type.

Examples:
  orun integrations cloudflare secret create CF_DEPLOY_TOKEN \
    --connection int_0123… --template workers-deploy --env prod
  orun integrations cloudflare secret create CF_API_TOKEN \
    --connection int_0123… --template workers-deploy --mode rotated \
    --rotation 30d --grace-seconds 3600 --deliver-target cloudflare-worker --env prod`,
		// The provider is a positional (providers are unknown offline), so the
		// static grammar underneath it is parsed by hand: a RunE with free args
		// keeps `orun integrations <anything>` inside this one command while
		// still failing typos loudly with suggestions (SP-A7).
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			provider, key, err := parseIntegrationsSecretArgs(args)
			if err != nil {
				return err
			}
			return runIntegrationsSecretCreate(cmd, provider, key)
		},
	}
	integrationsCmd.PersistentFlags().StringVar(&secretsBackendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	integrationsCmd.PersistentFlags().StringVar(&secretsOrgFlag, "org", "", "Workspace slug/id override for scope resolution (defaults to the linked workspace)")
	addSecretsScopeFlags(integrationsCmd)
	addSecretsJSONFlag(integrationsCmd)
	integrationsCmd.Flags().StringVar(&secretsConnection, "connection", "", "Integration connection public id (int_…) the value is minted against (required)")
	integrationsCmd.Flags().StringVar(&integrationsTemplateFlag, "template", "", "Scope template id declared by the provider (required; errors list the declared templates)")
	integrationsCmd.Flags().StringVar(&integrationsModeFlag, "mode", "brokered", "Secret mode: brokered (minted at resolve, never stored) or rotated (stored + re-minted on schedule)")
	integrationsCmd.Flags().StringArrayVar(&integrationsParamFlags, "param", nil, "Template param as key=value (repeatable; the template declares which are required)")
	integrationsCmd.Flags().StringVar(&secretsRotation, "rotation", "", "Rotation cadence for a rotated secret (e.g. 30d)")
	integrationsCmd.Flags().IntVar(&secretsGraceSeconds, "grace-seconds", 0, "Overlap seconds the prior token stays valid after a rotation (rotated mode; default: server 24h)")
	integrationsCmd.Flags().StringVar(&secretsDeliverTarget, "deliver-target", "", "Materialize target re-delivered on rotation for a long-lived consumer (rotated mode)")
	integrationsCmd.Flags().StringVar(&secretsDisplayName, "display-name", "", "Human display name for the key")
	root.AddCommand(integrationsCmd)
}

// parseIntegrationsSecretArgs parses the positional grammar
// `<provider> secret create <KEY>`. The provider half is dynamic (validated
// later against the capability read); the static halves fail loudly with a
// "did you mean" suggestion, extending the secrets tree's typo UX (SP-A7).
func parseIntegrationsSecretArgs(args []string) (provider, key string, err error) {
	provider = strings.TrimSpace(args[0])
	if provider == "" {
		return "", "", fmt.Errorf("usage: %s", integrationsUsageLine)
	}
	if len(args) < 2 {
		return "", "", fmt.Errorf("missing resource after provider %q\n\nusage:\n  %s", provider, integrationsUsageLine)
	}
	if args[1] != "secret" {
		return "", "", unknownIntegrationsWord("resource", args[1], integrationsResources)
	}
	if len(args) < 3 {
		return "", "", fmt.Errorf("missing verb after %q\n\nusage:\n  %s", "secret", integrationsUsageLine)
	}
	if args[2] != "create" {
		return "", "", unknownIntegrationsWord("verb", args[2], integrationsVerbs)
	}
	if len(args) < 4 {
		return "", "", fmt.Errorf("missing <KEY>\n\nusage:\n  %s", integrationsUsageLine)
	}
	if len(args) > 4 {
		return "", "", fmt.Errorf("unexpected argument %q after the key\n\nusage:\n  %s", args[4], integrationsUsageLine)
	}
	return provider, args[3], nil
}

// unknownIntegrationsWord is the typo error for the static grammar words,
// speaking the same "did you mean" dialect as unknownSecretsSubcommand.
func unknownIntegrationsWord(kind, got string, valid []string) error {
	var b strings.Builder
	fmt.Fprintf(&b, "unknown %s %q for \"orun integrations\"", kind, got)
	if suggestion := ui.SuggestMatch(got, valid); suggestion != "" {
		fmt.Fprintf(&b, "\n\ndid you mean:\n  %s", suggestion)
	}
	fmt.Fprintf(&b, "\n\nusage:\n  %s", integrationsUsageLine)
	return fmt.Errorf("%s", b.String())
}

// parseTemplateParams parses the repeatable --param key=value flags into the
// wire map. Purely syntactic — which params are required/accepted is the
// template's declaration, checked in validateAgainstCapability.
func parseTemplateParams(raw []string) (map[string]string, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	params := make(map[string]string, len(raw))
	for _, kv := range raw {
		i := strings.Index(kv, "=")
		if i <= 0 {
			return nil, fmt.Errorf("--param must be key=value, got %q", kv)
		}
		key := strings.TrimSpace(kv[:i])
		if key == "" {
			return nil, fmt.Errorf("--param must be key=value, got %q", kv)
		}
		if _, dup := params[key]; dup {
			return nil, fmt.Errorf("--param %q supplied more than once", key)
		}
		params[key] = kv[i+1:]
	}
	return params, nil
}

// integrationsCreatePreflight collects the local (no-network) flag failures
// so the caller sees the whole gate before auth or the capability read runs.
func integrationsCreatePreflight(key, connection, template, mode string, graceSeconds int, deliverTarget string) error {
	if !secretKeyPattern.MatchString(key) {
		return fmt.Errorf("invalid key %q: keys must match ^[A-Za-z][A-Za-z0-9._-]{0,127}$", key)
	}
	if strings.TrimSpace(connection) == "" {
		return fmt.Errorf("missing --connection <int_…>: the connection the value is minted against (find it on the provider's integration page)")
	}
	if !strings.HasPrefix(connection, "int_") {
		return fmt.Errorf("--connection must be an integration connection public id (int_…)")
	}
	if strings.TrimSpace(template) == "" {
		return fmt.Errorf("missing --template <id>: the provider's scope template to mint against (the create error lists declared templates, or see the provider's integration page)")
	}
	if mode != "brokered" && mode != "rotated" {
		return fmt.Errorf("--mode must be brokered or rotated, got %q", mode)
	}
	if graceSeconds < 0 {
		return fmt.Errorf("--grace-seconds must be non-negative")
	}
	if mode != "rotated" {
		if graceSeconds > 0 {
			return fmt.Errorf("--grace-seconds applies to --mode rotated only (a brokered value is minted per-resolve and never stored)")
		}
		if strings.TrimSpace(deliverTarget) != "" {
			return fmt.Errorf("--deliver-target applies to --mode rotated only (a brokered value is minted per-resolve and never materialized)")
		}
	}
	return nil
}

// findSecretsCapability resolves the provider positional against the org's
// declared secret sources. Unknown providers fail with the declared list and
// a "did you mean" — the capability read IS the help surface (SP-A7).
func findSecretsCapability(caps []configsurface.SecretsCapability, provider string) (*configsurface.SecretsCapability, error) {
	names := make([]string, 0, len(caps))
	for i := range caps {
		if caps[i].Provider == provider {
			return &caps[i], nil
		}
		names = append(names, caps[i].Provider)
	}
	sort.Strings(names)
	var b strings.Builder
	fmt.Fprintf(&b, "provider %q is not a declared secret source for this workspace", provider)
	if suggestion := ui.SuggestMatch(provider, names); suggestion != "" {
		fmt.Fprintf(&b, "\n\ndid you mean:\n  orun integrations %s secret create …", suggestion)
	}
	if len(names) > 0 {
		fmt.Fprintf(&b, "\n\ndeclared secret sources: %s", strings.Join(names, ", "))
	} else {
		b.WriteString("\n\nno integration declares a secrets capability yet; connect one in the console's integration hub")
	}
	return nil, fmt.Errorf("%s", b.String())
}

// validateAgainstCapability checks one create request against the provider's
// declared capability: mode ∈ supportedModes, template exists and is active
// (SP-A6 soft-retire), the template's declared params are all supplied (and
// nothing undeclared is), and a rotated deliver target is a declared delivery
// target. Every rejection lists what IS declared. Returns the resolved
// template so the caller can echo its identity. Pure — no I/O.
func validateAgainstCapability(cap *configsurface.SecretsCapability, mode, template string, params map[string]string, deliverTarget string) (*configsurface.ScopeTemplate, error) {
	if !containsString(cap.SupportedModes, mode) {
		return nil, fmt.Errorf("provider %q does not support --mode %s; supported modes: %s",
			cap.Provider, mode, strings.Join(cap.SupportedModes, ", "))
	}

	var tpl *configsurface.ScopeTemplate
	active := make([]string, 0, len(cap.ScopeTemplates))
	for i := range cap.ScopeTemplates {
		t := &cap.ScopeTemplates[i]
		if t.Active() {
			active = append(active, t.ID)
		}
		if t.ID == template {
			tpl = t
		}
	}
	sort.Strings(active)
	if tpl != nil && !tpl.Active() {
		return nil, fmt.Errorf("template %q is retired and cannot back a new secret (existing bindings keep resolving); %s templates: %s",
			template, cap.Provider, strings.Join(active, ", "))
	}
	if tpl == nil {
		var b strings.Builder
		fmt.Fprintf(&b, "template %q is not declared by provider %q", template, cap.Provider)
		if suggestion := ui.SuggestMatch(template, active); suggestion != "" {
			fmt.Fprintf(&b, "\n\ndid you mean:\n  --template %s", suggestion)
		}
		if len(active) > 0 {
			fmt.Fprintf(&b, "\n\n%s templates: %s", cap.Provider, strings.Join(active, ", "))
		}
		return nil, fmt.Errorf("%s", b.String())
	}

	var missing []string
	for _, name := range tpl.Params {
		if _, ok := params[name]; !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("template %q requires params: %s; pass each as --param <key>=<value> (missing: %s)",
			tpl.ID, strings.Join(tpl.Params, ", "), strings.Join(missing, ", "))
	}
	var unknown []string
	for name := range params {
		if !containsString(tpl.Params, name) {
			unknown = append(unknown, name)
		}
	}
	if len(unknown) > 0 {
		sort.Strings(unknown)
		accepted := "none"
		if len(tpl.Params) > 0 {
			accepted = strings.Join(tpl.Params, ", ")
		}
		return nil, fmt.Errorf("template %q does not declare param(s): %s; accepted params: %s",
			tpl.ID, strings.Join(unknown, ", "), accepted)
	}

	if mode == "rotated" && deliverTarget != "" && !containsString(cap.DeliveryTargets, deliverTarget) {
		if len(cap.DeliveryTargets) == 0 {
			return nil, fmt.Errorf("provider %q declares no delivery targets (per-run consumers only); drop --deliver-target", cap.Provider)
		}
		return nil, fmt.Errorf("deliver target %q is not declared by provider %q; declared targets: %s",
			deliverTarget, cap.Provider, strings.Join(cap.DeliveryTargets, ", "))
	}
	return tpl, nil
}

func runIntegrationsSecretCreate(cmd *cobra.Command, provider, key string) error {
	mode := strings.TrimSpace(integrationsModeFlag)
	if err := integrationsCreatePreflight(key, secretsConnection, integrationsTemplateFlag, mode, secretsGraceSeconds, secretsDeliverTarget); err != nil {
		return err
	}
	params, err := parseTemplateParams(integrationsParamFlags)
	if err != nil {
		return err
	}

	ctx := cmd.Context()
	rt, err := newSecretsRuntime(ctx)
	if err != nil {
		return err
	}
	scope, label, err := rt.targetScope(ctx, false)
	if err != nil {
		return err
	}

	// Capability-driven validation (SP-A7): the provider's declaration is the
	// single source of truth for templates/modes/targets — nothing is assumed.
	caps, err := rt.client.ListSecretsCapabilities(ctx, rt.org)
	if err != nil {
		return err
	}
	capability, err := findSecretsCapability(caps, provider)
	if err != nil {
		return err
	}
	tpl, err := validateAgainstCapability(capability, mode, integrationsTemplateFlag, params, secretsDeliverTarget)
	if err != nil {
		return err
	}

	req := configsurface.CreateSecretRequest{
		SecretKey:      key,
		DisplayName:    secretsDisplayName,
		RotationPolicy: secretsRotation,
	}
	switch mode {
	case "brokered":
		req.Binding = &configsurface.SecretBrokerBinding{
			ConnectionID: secretsConnection,
			Template:     tpl.ID,
			Params:       params,
		}
	case "rotated":
		rotation := &configsurface.SecretRotationBinding{
			ConnectionID:  secretsConnection,
			Template:      tpl.ID,
			Params:        params,
			DeliverTarget: secretsDeliverTarget,
		}
		if secretsGraceSeconds > 0 {
			rotation.GraceSeconds = &secretsGraceSeconds
		}
		req.Rotation = rotation
	}

	meta, err := rt.client.CreateSecret(ctx, scope, req)
	if err != nil {
		return renderSecretsWriteError(err, key)
	}
	if secretsJSONOut {
		return emitJSON(meta)
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	detail := fmt.Sprintf("%s %s via %s, %s", provider, mode, tpl.ID, label)
	if meta != nil && meta.Version > 0 {
		detail += fmt.Sprintf(", version %d", meta.Version)
	}
	fmt.Printf("%s created %s (%s)\n", ui.Green(color, "✓"), key, detail)
	return nil
}

// ── --from-broker deprecation (SP-A7) ────────────────────────────────────────

// replacementSpec captures one legacy `secrets set --from-broker` invocation
// so the deprecation notice can print its EXACT namespaced substitute.
type replacementSpec struct {
	Key           string
	Provider      string
	Template      string
	Connection    string
	Rotation      string
	GraceSeconds  int
	DeliverTarget string
	DisplayName   string
	Env           string
	Project       bool
	Workspace     bool
}

// buildReplacementCommand renders the `orun integrations …` command line that
// replaces a deprecated --from-broker invocation, carrying over exactly the
// flags the caller actually passed. Pure — no I/O.
func buildReplacementCommand(spec replacementSpec) string {
	parts := []string{
		"orun", "integrations", spec.Provider, "secret", "create", spec.Key,
		"--connection", spec.Connection,
		"--template", spec.Template,
		"--mode", "rotated",
	}
	if spec.Rotation != "" {
		parts = append(parts, "--rotation", shellQuote(spec.Rotation))
	}
	if spec.GraceSeconds > 0 {
		parts = append(parts, "--grace-seconds", strconv.Itoa(spec.GraceSeconds))
	}
	if spec.DeliverTarget != "" {
		parts = append(parts, "--deliver-target", shellQuote(spec.DeliverTarget))
	}
	if spec.DisplayName != "" {
		parts = append(parts, "--display-name", shellQuote(spec.DisplayName))
	}
	switch {
	case strings.TrimSpace(spec.Env) != "":
		parts = append(parts, "--env", spec.Env)
	case spec.Project:
		parts = append(parts, "--project")
	case spec.Workspace:
		parts = append(parts, "--workspace")
	}
	return strings.Join(parts, " ")
}

// shellQuote single-quotes a value when it would not survive a shell unquoted,
// so the printed replacement command is copy-paste runnable.
func shellQuote(s string) string {
	if s != "" && !strings.ContainsAny(s, " \t\"'\\$`") {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// containsString reports whether v is an element of s.
func containsString(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// fromBrokerDeprecationNotice is the SP-A7 deprecation line printed (stderr)
// when `secrets set --from-broker` is used: the exact namespaced replacement
// for the caller's invocation. Pure — no I/O.
func fromBrokerDeprecationNotice(spec replacementSpec) string {
	return fmt.Sprintf("deprecated: --from-broker moves to the integration namespace; use '%s'", buildReplacementCommand(spec))
}
