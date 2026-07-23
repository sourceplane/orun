package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/sourceplane/orun/internal/configsurface"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

// `orun secrets` — manage values (write-only + metadata) against the platform
// config surface (specs/orun-secrets/cli-surface.md §1, data-model.md §8).
// Invariant: no secret value is ever printed, logged, or embedded in an error.

var (
	secretsBackendURL  string
	secretsOrgFlag     string
	secretsEnvFlag     string
	secretsProjectFlag bool
	secretsWorkspFlag  bool
	secretsPersonal    bool
	secretsLocked      bool
	secretsValueFlag   string
	secretsRotation    string
	secretsDisplayName string
	secretsChain       bool
	secretsJSONOut     bool
	secretsFromDotenv  string
	secretsBreakGlass  bool
	secretsReason      string
	// provider-rotated-secrets RS4: create-from-parent + rotate-now flags.
	secretsFromBroker    string
	secretsConnection    string
	secretsGraceSeconds  int
	secretsDeliverTarget string
	secretsRemint        bool
)

func registerSecretsCommand(root *cobra.Command) {
	secretsCmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage secret values and metadata (write-only; values are never displayed)",
		Long: `Manage secrets on Orun Cloud: set, import, rotate, revoke, and inspect
metadata. The surface is write-only — values go up, only metadata comes back.

Scope defaults to the linked project: --env <env> targets an environment rung,
--project the project-wide rung, --workspace the workspace-shared rung.`,
		// The group itself has no action, but a RunE lets us intercept an
		// unknown subcommand (a typo like `secrets revieal`) and fail with a
		// "did you mean" suggestion and a non-zero exit — cobra would otherwise
		// treat the typo as args to a non-runnable group and silently print help.
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return cmd.Help()
			}
			return unknownSecretsSubcommand(cmd, args[0])
		},
	}
	secretsCmd.PersistentFlags().StringVar(&secretsBackendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	secretsCmd.PersistentFlags().StringVar(&secretsOrgFlag, "org", "", "Workspace slug/id override for scope resolution (defaults to the linked workspace)")

	setCmd := &cobra.Command{
		Use:   "set <KEY>",
		Short: "Set a secret value (prompts for the value, or reads stdin / --value)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretsSet(cmd, args[0])
		},
	}
	addSecretsScopeFlags(setCmd)
	setCmd.Flags().BoolVar(&secretsPersonal, "personal", false, "Store a personal overlay for you only (environment scope)")
	setCmd.Flags().BoolVar(&secretsLocked, "locked", false, "Lock the key so lower rungs cannot shadow it (implies --workspace)")
	setCmd.Flags().StringVar(&secretsValueFlag, "value", "", "Secret value (prefer stdin: --value may land in shell history)")
	setCmd.Flags().StringVar(&secretsRotation, "rotation", "", "Rotation policy for the key")
	setCmd.Flags().StringVar(&secretsDisplayName, "display-name", "", "Human display name for the key")
	// provider-rotated-secrets RS4: create-from-parent — no value is read; the
	// server mints v1 from the connected parent and rotates it on schedule.
	setCmd.Flags().StringVar(&secretsFromBroker, "from-broker", "", "Create a provider-rotated secret from a broker template (e.g. cloudflare/workers-deploy); no value is read")
	setCmd.Flags().StringVar(&secretsConnection, "connection", "", "Integration connection public id (int_…) the value is minted against (with --from-broker)")
	setCmd.Flags().IntVar(&secretsGraceSeconds, "grace-seconds", 0, "Overlap seconds the prior token stays valid after a rotation (with --from-broker; default: server 24h)")
	setCmd.Flags().StringVar(&secretsDeliverTarget, "deliver-target", "", "Materialize target re-delivered on rotation for a long-lived consumer (with --from-broker)")
	addSecretsJSONFlag(setCmd)

	importCmd := &cobra.Command{
		Use:   "import --from-dotenv <file> --env <env>",
		Short: "Bulk-import a .env file (write-only; prints a per-key summary)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretsImport(cmd)
		},
	}
	importCmd.Flags().StringVar(&secretsFromDotenv, "from-dotenv", "", "Path to the .env file to import")
	importCmd.Flags().StringVar(&secretsEnvFlag, "env", "", "Target environment (slug)")
	addSecretsJSONFlag(importCmd)
	_ = importCmd.MarkFlagRequired("from-dotenv")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List secret metadata for a scope (never values)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretsList(cmd)
		},
	}
	addSecretsScopeFlags(listCmd)
	listCmd.Flags().BoolVar(&secretsChain, "chain", false, "Show the inheritance view for an environment (requires --env)")
	listCmd.Flags().BoolVar(&secretsJSONOut, "json", false, "Print the raw metadata items as JSON")

	rotateCmd := &cobra.Command{
		Use:   "rotate <KEY> --env <env>",
		Short: "Append a new version of a secret (prompts for the value, or reads stdin / --value)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretsRotate(cmd, args[0])
		},
	}
	addSecretsScopeFlags(rotateCmd)
	rotateCmd.Flags().StringVar(&secretsValueFlag, "value", "", "New secret value (prefer stdin: --value may land in shell history)")
	// provider-rotated-secrets RS4: rotate-now for a provider-rotated secret —
	// no value is read; the server re-mints from the connected parent (RS3).
	rotateCmd.Flags().BoolVar(&secretsRemint, "remint", false, "Re-mint a provider-rotated secret from its connected parent (no value is read)")
	addSecretsJSONFlag(rotateCmd)

	revokeCmd := &cobra.Command{
		Use:     "revoke <KEY>",
		Aliases: []string{"rm"},
		Short:   "Revoke (tombstone) a secret",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretsRevoke(cmd, args[0])
		},
	}
	addSecretsScopeFlags(revokeCmd)
	addSecretsJSONFlag(revokeCmd)

	versionsCmd := &cobra.Command{
		Use:   "versions <KEY> --env <env>",
		Short: "Show a secret's version history (metadata only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretsVersions(cmd, args[0])
		},
	}
	addSecretsScopeFlags(versionsCmd)
	addSecretsJSONFlag(versionsCmd)

	revealCmd := &cobra.Command{
		Use:   "reveal <KEY> --env <env> --break-glass --reason <why>",
		Short: "Break-glass: reveal a value for incident recovery (audited + alerted)",
		Long: `Reveal a secret's plaintext value. This is the ONLY command that returns a
value, and it is not the normal path — a workload receives secrets by runner
injection (orun run), never by a human reveal.

reveal is an elevated, deny-by-default action: it requires --break-glass and a
--reason, is recorded in the audit log, and raises a secret.revealed alert.
Expect near-zero use outside incident recovery.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretsReveal(cmd, args[0])
		},
	}
	addSecretsScopeFlags(revealCmd)
	revealCmd.Flags().BoolVar(&secretsBreakGlass, "break-glass", false, "Required acknowledgement that this is an audited emergency reveal")
	revealCmd.Flags().StringVar(&secretsReason, "reason", "", "Required justification, recorded in the audit log")

	secretsCmd.AddCommand(setCmd, importCmd, listCmd, rotateCmd, revokeCmd, versionsCmd, revealCmd)
	root.AddCommand(secretsCmd)
}

// addSecretsJSONFlag registers the shared --json flag so every secrets
// subcommand can emit machine-readable output for scripts and CI (metadata
// only — never a value).
func addSecretsJSONFlag(cmd *cobra.Command) {
	cmd.Flags().BoolVar(&secretsJSONOut, "json", false, "Emit machine-readable JSON instead of the human table")
}

// emitJSON writes v to stdout as indented JSON — the shared --json output path.
// Callers pass only metadata structs; no secret value is ever routed here.
func emitJSON(v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return fmt.Errorf("encoding JSON: %w", err)
	}
	fmt.Println(string(data))
	return nil
}

// unknownSecretsSubcommand turns a typo'd `orun secrets <x>` into an actionable
// error with a "did you mean" suggestion and a non-zero exit, instead of the
// silent generic help cobra prints for a non-runnable group. It reuses the
// shared Levenshtein suggester (ui.SuggestMatch) already used for env/component
// typos, so the whole CLI speaks one "did you mean" dialect.
func unknownSecretsSubcommand(parent *cobra.Command, name string) error {
	canonical, candidates := secretsSubcommandNames(parent)
	var b strings.Builder
	fmt.Fprintf(&b, "unknown subcommand %q for %q", name, parent.CommandPath())
	if suggestion := ui.SuggestMatch(name, candidates); suggestion != "" {
		fmt.Fprintf(&b, "\n\ndid you mean:\n  %s %s", parent.CommandPath(), suggestion)
	}
	if len(canonical) > 0 {
		b.WriteString("\n\navailable subcommands:\n")
		for _, n := range canonical {
			fmt.Fprintf(&b, "  %s\n", n)
		}
	}
	return fmt.Errorf("%s", strings.TrimRight(b.String(), "\n"))
}

// secretsSubcommandNames returns the invokable subcommands under `secrets`,
// sorted: `canonical` is the printable list (names only), `candidates` also
// includes aliases (e.g. `rm`) so a near-miss can still resolve to one.
func secretsSubcommandNames(parent *cobra.Command) (canonical, candidates []string) {
	for _, c := range parent.Commands() {
		if c.Hidden || c.Name() == "help" || c.Name() == "completion" {
			continue
		}
		canonical = append(canonical, c.Name())
		candidates = append(candidates, c.Name())
		candidates = append(candidates, c.Aliases...)
	}
	sort.Strings(canonical)
	sort.Strings(candidates)
	return canonical, candidates
}

// runSecretsReveal implements the break-glass reveal — the one value-returning
// command (SD-3). It hard-requires --break-glass and --reason, prints the value
// to stdout only (with a stderr warning), and never writes it to any file.
func runSecretsReveal(cmd *cobra.Command, key string) error {
	ctx := cmd.Context()
	// Validate every precondition up front and report them together, so a
	// caller isn't walked through --break-glass, then --reason, then a scope
	// selector across three separate failed runs.
	if err := revealPreflight(key); err != nil {
		return err
	}
	rt, err := newSecretsRuntime(ctx)
	if err != nil {
		return err
	}
	scope, label, err := rt.targetScope(ctx, false)
	if err != nil {
		return err
	}
	id, err := findSecretID(ctx, rt, scope, label, key)
	if err != nil {
		return err
	}
	revealed, err := rt.client.RevealSecret(ctx, scope, id, secretsReason)
	if err != nil {
		return renderSecretsWriteError(err, key)
	}
	color := ui.ColorEnabledForWriter(os.Stderr)
	fmt.Fprintf(os.Stderr, "%s break-glass reveal of %s (%s, version %d) — this access has been audited and alerted\n",
		ui.Yellow(color, "⚠"), key, label, revealed.Version)
	// The value goes to stdout ALONE so it can be piped without the notice; it
	// is never written to a file by orun.
	fmt.Println(revealed.Value)
	return nil
}

// revealPreflight collects the reveal preconditions that are missing (the
// break-glass acknowledgement, the audit reason, and a scope selector) and, if
// any, returns a single error listing all of them at once with a ready-to-run
// example. It inspects flags only — no auth or network — so it runs before the
// runtime is built and the caller sees the whole gate in one shot.
func revealPreflight(key string) error {
	type want struct{ flag, why string }
	var missing []want
	if !secretsBreakGlass {
		missing = append(missing, want{"--break-glass", "acknowledge this is an audited, alerted emergency action"})
	}
	if strings.TrimSpace(secretsReason) == "" {
		missing = append(missing, want{`--reason "<why>"`, "recorded in the audit log"})
	}

	intent := loadIntentForCloudConfig()
	envNames := intentEnvironmentNames(intent)
	if !anySecretsScopeSelector() {
		hint := "<env>"
		if len(envNames) > 0 {
			hint = "<" + strings.Join(envNames, "|") + ">"
		}
		missing = append(missing, want{"--env " + hint, "which rung to reveal from (or --project / --workspace)"})
	}
	if len(missing) == 0 {
		return nil
	}

	width := 0
	for _, m := range missing {
		if len(m.flag) > width {
			width = len(m.flag)
		}
	}
	var b strings.Builder
	b.WriteString("reveal needs more to proceed:\n")
	for _, m := range missing {
		fmt.Fprintf(&b, "    %s   %s\n", padRight(m.flag, width), m.why)
	}
	exampleScope := "--env <env>"
	if len(envNames) > 0 {
		exampleScope = "--env " + envNames[0]
	}
	fmt.Fprintf(&b, "\n  example:\n    orun secrets reveal %s %s --break-glass --reason \"incident #123\"", key, exampleScope)
	return fmt.Errorf("%s", b.String())
}

// anySecretsScopeSelector reports whether the caller named any rung selector
// (--env / --project / --workspace). Presence only; mutual-exclusion and slug
// resolution are handled downstream by targetScope.
func anySecretsScopeSelector() bool {
	return strings.TrimSpace(secretsEnvFlag) != "" || secretsProjectFlag || secretsWorkspFlag
}

// addSecretsScopeFlags registers the rung-selector flags shared by set/list/
// revoke. Within the secrets group --workspace/--project are scope selectors
// (cli-surface §1); the org override spelling is the persistent --org.
func addSecretsScopeFlags(cmd *cobra.Command) {
	cmd.Flags().StringVar(&secretsEnvFlag, "env", "", "Target environment (slug) on the linked project")
	cmd.Flags().BoolVar(&secretsProjectFlag, "project", false, "Target the project-wide rung (every environment inherits)")
	cmd.Flags().BoolVar(&secretsWorkspFlag, "workspace", false, "Target the workspace-shared rung")
}

// secretsRuntime carries the resolved backend, auth, and org/project scope
// for one secrets invocation.
type secretsRuntime struct {
	intent     *model.Intent
	backendURL string
	client     *configsurface.Client
	org        string
	project    string
}

// newSecretsRuntime resolves the backend URL (flag > env > intent > user
// config), the org/project scope (--org flag > ORUN_WORKSPACE/ORUN_ORG >
// intent execution.state > cached RepoLink), and a bearer token source.
func newSecretsRuntime(ctx context.Context) (*secretsRuntime, error) {
	intent := loadIntentForCloudConfig()
	backendURL, err := requireBackendURL(intent, secretsBackendURL)
	if err != nil {
		return nil, err
	}
	linkOrg, linkProject := "", ""
	if repo, repoErr := resolveRepoContext(backendURL); repoErr == nil && repo != nil {
		linkOrg, linkProject = repo.OrgID, repo.ProjectID
	}
	intentOrg, intentProject, _ := intentScope(intent)
	scope := resolveScope(secretsOrgFlag, "", intentOrg, intentProject, linkOrg, linkProject)
	if strings.TrimSpace(scope.OrgID) == "" {
		return nil, errRepoNotLinked(backendURL)
	}
	tokenSrc, _, _, err := remotestate.ResolveTokenSource(ctx, remotestate.ResolveOptions{
		BackendURL:   backendURL,
		Version:      version,
		Interactive:  termIsInteractive(),
		RequireLogin: true,
		Org:          scope.OrgID,
	})
	if err != nil {
		if isNoLoginErr(err) {
			return nil, errNotLoggedIn()
		}
		return nil, fmt.Errorf("orun cloud auth: %w", err)
	}
	return &secretsRuntime{
		intent:     intent,
		backendURL: backendURL,
		client:     configsurface.NewClient(backendURL, version, tokenSrc),
		org:        scope.OrgID,
		project:    scope.ProjectID,
	}, nil
}

// targetScope maps the rung-selector flags to a configsurface.Scope plus a
// human label. With no selector: the project rung when defaultToProject (list),
// otherwise an actionable missing---env error naming the declared envs.
func (rt *secretsRuntime) targetScope(ctx context.Context, defaultToProject bool) (configsurface.Scope, string, error) {
	selectors := 0
	if strings.TrimSpace(secretsEnvFlag) != "" {
		selectors++
	}
	if secretsProjectFlag {
		selectors++
	}
	if secretsWorkspFlag {
		selectors++
	}
	if selectors > 1 {
		return configsurface.Scope{}, "", fmt.Errorf("--env, --project, and --workspace are mutually exclusive")
	}
	switch {
	case strings.TrimSpace(secretsEnvFlag) != "":
		return rt.environmentScope(ctx, secretsEnvFlag)
	case secretsProjectFlag:
		if strings.TrimSpace(rt.project) == "" {
			return configsurface.Scope{}, "", errRepoNotLinked(rt.backendURL)
		}
		return configsurface.Scope{Kind: configsurface.ScopeProject, Org: rt.org, Project: rt.project},
			fmt.Sprintf("project %q", rt.project), nil
	case secretsWorkspFlag:
		return configsurface.Scope{Kind: configsurface.ScopeWorkspace, Org: rt.org},
			fmt.Sprintf("workspace %q", rt.org), nil
	case defaultToProject:
		if strings.TrimSpace(rt.project) == "" {
			return configsurface.Scope{}, "", errRepoNotLinked(rt.backendURL)
		}
		return configsurface.Scope{Kind: configsurface.ScopeProject, Org: rt.org, Project: rt.project},
			fmt.Sprintf("project %q", rt.project), nil
	default:
		return configsurface.Scope{}, "", rt.errEnvRequired(true)
	}
}

// environmentScope resolves an env slug to its public env_… id and builds the
// environment-rung scope.
func (rt *secretsRuntime) environmentScope(ctx context.Context, env string) (configsurface.Scope, string, error) {
	if strings.TrimSpace(rt.project) == "" {
		return configsurface.Scope{}, "", errRepoNotLinked(rt.backendURL)
	}
	envID, err := rt.client.ResolveEnvironmentID(ctx, rt.org, rt.project, env)
	if err != nil {
		return configsurface.Scope{}, "", err
	}
	return configsurface.Scope{Kind: configsurface.ScopeEnvironment, Org: rt.org, Project: rt.project, EnvID: envID},
		fmt.Sprintf("environment %q", strings.TrimSpace(env)), nil
}

// errEnvRequired is the missing---env error. When the intent cheaply declares
// environment names they are listed; otherwise the error just asks for --env.
func (rt *secretsRuntime) errEnvRequired(scopeFlagsAllowed bool) error {
	alt := ""
	if scopeFlagsAllowed {
		alt = " (or --project / --workspace for a project- or workspace-scoped secret)"
	}
	if names := intentEnvironmentNames(rt.intent); len(names) > 0 {
		return fmt.Errorf("missing --env; declared environments: %s%s", strings.Join(names, ", "), alt)
	}
	return fmt.Errorf("missing --env <env>%s", alt)
}

// intentEnvironmentNames returns the environment names declared in the intent,
// sorted; nil when no intent is loaded.
func intentEnvironmentNames(intent *model.Intent) []string {
	if intent == nil || len(intent.Environments) == 0 {
		return nil
	}
	names := make([]string, 0, len(intent.Environments))
	for name := range intent.Environments {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

// readSecretValue returns the value for set/rotate. Precedence:
//   - --value flag (with a shell-history warning);
//   - an interactive terminal: a masked single-line prompt (echo off), where
//     Enter submits — no Ctrl-D / EOF needed (the wrangler-style flow);
//   - a pipe / non-TTY stdin: the piped value, with one trailing newline
//     trimmed (so `printf 'x' |` and `echo x |` both work), for CI and scripts.
func readSecretValue(cmd *cobra.Command) (string, error) {
	if cmd.Flags().Changed("value") {
		fmt.Fprintln(os.Stderr, "warning: --value may be recorded in your shell history; prefer the interactive prompt or piping on stdin")
		if secretsValueFlag == "" {
			return "", fmt.Errorf("--value is empty")
		}
		return secretsValueFlag, nil
	}

	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		// Masked, single line: read the value with terminal echo disabled and
		// submit on Enter. The value is taken verbatim (never trimmed) so a
		// token with meaningful whitespace is preserved.
		fmt.Fprint(os.Stderr, "Enter secret value (input hidden): ")
		data, err := term.ReadPassword(fd)
		fmt.Fprintln(os.Stderr) // Enter is not echoed; move off the prompt line.
		if err != nil {
			return "", fmt.Errorf("reading value: %w", err)
		}
		value := string(data)
		if value == "" {
			return "", fmt.Errorf("no value entered")
		}
		return value, nil
	}

	// Piped / non-interactive stdin.
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("reading value from stdin: %w", err)
	}
	value := trimOneTrailingNewline(string(data))
	if value == "" {
		return "", fmt.Errorf("no value provided: run interactively, pipe the value on stdin, or pass --value")
	}
	return value, nil
}

// trimOneTrailingNewline removes exactly one trailing LF or CRLF.
func trimOneTrailingNewline(s string) string {
	s = strings.TrimSuffix(s, "\n")
	s = strings.TrimSuffix(s, "\r")
	return s
}

// ── set ──────────────────────────────────────────────────────────────────────

func runSecretsSet(cmd *cobra.Command, key string) error {
	if !secretKeyPattern.MatchString(key) {
		return fmt.Errorf("invalid key %q: keys must match ^[A-Za-z][A-Za-z0-9._-]{0,127}$", key)
	}
	// --locked implies/requires the workspace rung (SD-12′).
	if secretsLocked {
		if strings.TrimSpace(secretsEnvFlag) != "" || secretsProjectFlag {
			return fmt.Errorf("--locked applies to the workspace rung only; use --locked with --workspace (or alone)")
		}
		secretsWorkspFlag = true
	}
	if secretsPersonal && strings.TrimSpace(secretsEnvFlag) == "" {
		return fmt.Errorf("--personal requires --env <env>: personal overlays are environment-scoped")
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

	req := configsurface.CreateSecretRequest{
		SecretKey:      key,
		DisplayName:    secretsDisplayName,
		RotationPolicy: secretsRotation,
		Personal:       secretsPersonal,
	}
	if secretsFromBroker != "" {
		// Create-from-parent (provider-rotated-secrets RS4): NO value is read —
		// the server mints v1 from the connected parent and the engine rotates
		// it on schedule. The template arg is provider/template for readability;
		// the server derives the provider from the connection.
		rotation, err := buildRotationBinding(secretsFromBroker, secretsConnection, secretsGraceSeconds, secretsDeliverTarget)
		if err != nil {
			return err
		}
		if secretsValueFlag != "" {
			return fmt.Errorf("--from-broker and --value are mutually exclusive: the value is minted from the connected parent")
		}
		if secretsPersonal {
			return fmt.Errorf("--from-broker cannot be personal: broker authority binds at shared scopes only")
		}
		req.Rotation = rotation
		// Deprecation (saas-secrets-platform SP5, SP-A7): authoring an
		// integration-bound secret moves to the integration namespace. Keep
		// working for one release, printing the EXACT namespaced substitute
		// for this invocation — stderr only, so --json and pipes stay clean.
		fmt.Fprintln(os.Stderr, fromBrokerDeprecationNotice(replacementSpec{
			Key:           key,
			Provider:      strings.SplitN(secretsFromBroker, "/", 2)[0],
			Template:      rotation.Template,
			Connection:    secretsConnection,
			Rotation:      secretsRotation,
			GraceSeconds:  secretsGraceSeconds,
			DeliverTarget: secretsDeliverTarget,
			DisplayName:   secretsDisplayName,
			Env:           secretsEnvFlag,
			Project:       secretsProjectFlag,
			Workspace:     secretsWorkspFlag,
		}))
	} else {
		value, err := readSecretValue(cmd)
		if err != nil {
			return err
		}
		req.Value = value
	}
	if secretsLocked {
		overridable := false
		req.Overridable = &overridable
	}
	meta, err := rt.client.CreateSecret(ctx, scope, req)
	if err != nil {
		return renderSecretsWriteError(err, key)
	}
	if secretsJSONOut {
		return emitJSON(meta)
	}

	color := ui.ColorEnabledForWriter(os.Stdout)
	detail := label
	if meta != nil && meta.Version > 0 {
		detail += fmt.Sprintf(", version %d", meta.Version)
	}
	if secretsPersonal {
		detail += ", personal"
	}
	if secretsLocked {
		detail += ", locked"
	}
	fmt.Printf("%s set %s (%s)\n", ui.Green(color, "✓"), key, detail)
	return nil
}

// renderSecretsWriteError makes the two common write failures actionable. It
// never includes the value being written (the *APIError carries only the
// server envelope).
func renderSecretsWriteError(err error, key string) error {
	var apiErr *configsurface.APIError
	if asAPIError(err, &apiErr) {
		switch {
		case apiErr.IsLocked():
			return fmt.Errorf("cannot write %s: %w\nhint: the key is locked at a higher rung (or conflicts with an existing row); see `orun secrets list --chain --env <env>`", key, err)
		case apiErr.IsNotFound():
			return fmt.Errorf("cannot write %s: %w\nhint: the scope was not found or you lack access; check `orun cloud status`", key, err)
		}
	}
	return err
}

// asAPIError unwraps err looking for a *configsurface.APIError.
func asAPIError(err error, target **configsurface.APIError) bool {
	for e := err; e != nil; {
		if apiErr, ok := e.(*configsurface.APIError); ok {
			*target = apiErr
			return true
		}
		unwrapper, ok := e.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		e = unwrapper.Unwrap()
	}
	return false
}

// ── import ───────────────────────────────────────────────────────────────────

func runSecretsImport(cmd *cobra.Command) error {
	if strings.TrimSpace(secretsEnvFlag) == "" {
		rt := &secretsRuntime{intent: loadIntentForCloudConfig()}
		return rt.errEnvRequired(false)
	}
	f, err := os.Open(secretsFromDotenv)
	if err != nil {
		return fmt.Errorf("open dotenv file: %w", err)
	}
	entries, parseErr := parseDotenv(f)
	f.Close()
	if parseErr != nil {
		return parseErr
	}
	if len(entries) == 0 {
		return fmt.Errorf("%s contains no KEY=VALUE lines", secretsFromDotenv)
	}

	ctx := cmd.Context()
	rt, err := newSecretsRuntime(ctx)
	if err != nil {
		return err
	}
	scope, label, err := rt.environmentScope(ctx, secretsEnvFlag)
	if err != nil {
		return err
	}

	var toSend []configsurface.ImportSecret
	for _, e := range entries {
		if e.Invalid {
			continue
		}
		toSend = append(toSend, configsurface.ImportSecret{SecretKey: e.Key, Value: e.Value})
	}
	var results []configsurface.ImportResult
	var importErr error
	if len(toSend) > 0 {
		results, importErr = rt.client.ImportSecrets(ctx, scope, toSend)
	}

	rows := mergeImportSummary(entries, results)
	if secretsJSONOut {
		if err := emitJSON(rows); err != nil {
			return err
		}
		return importErr
	}
	fmt.Print(renderImportSummary(rows))
	fmt.Printf("\n%s → %s: %s\n", secretsFromDotenv, label, summarizeImportCounts(rows))
	if importErr != nil {
		return importErr
	}
	return nil
}

// importSummaryRow is one line of the import summary; it carries no value.
type importSummaryRow struct {
	Key    string `json:"key"`
	Result string `json:"result"`
}

// mergeImportSummary joins parse results with server per-key outcomes,
// preserving the file order. Keys the server did not report on (e.g. a chunk
// aborted mid-import) render as "not imported".
func mergeImportSummary(entries []dotenvEntry, results []configsurface.ImportResult) []importSummaryRow {
	statusByKey := make(map[string]string, len(results))
	for _, r := range results {
		statusByKey[r.SecretKey] = r.Status
	}
	rows := make([]importSummaryRow, 0, len(entries))
	for _, e := range entries {
		switch {
		case e.Invalid:
			rows = append(rows, importSummaryRow{Key: e.Key, Result: "invalid: " + e.Reason})
		case statusByKey[e.Key] != "":
			rows = append(rows, importSummaryRow{Key: e.Key, Result: statusByKey[e.Key]})
		default:
			rows = append(rows, importSummaryRow{Key: e.Key, Result: "not imported"})
		}
	}
	return rows
}

// renderImportSummary renders the per-key KEY/RESULT table.
func renderImportSummary(rows []importSummaryRow) string {
	table := make([][]string, 0, len(rows))
	for _, r := range rows {
		table = append(table, []string{r.Key, r.Result})
	}
	return renderColumns([]string{"KEY", "RESULT"}, table)
}

// summarizeImportCounts tallies results into "N created, N conflict, N
// invalid, …" keyed by the first word of each result.
func summarizeImportCounts(rows []importSummaryRow) string {
	counts := map[string]int{}
	var order []string
	for _, r := range rows {
		kind := r.Result
		if i := strings.IndexAny(kind, ": "); i > 0 {
			kind = kind[:i]
		}
		if counts[kind] == 0 {
			order = append(order, kind)
		}
		counts[kind]++
	}
	parts := make([]string, 0, len(order))
	for _, kind := range order {
		parts = append(parts, fmt.Sprintf("%d %s", counts[kind], kind))
	}
	if len(parts) == 0 {
		return "nothing to import"
	}
	return strings.Join(parts, ", ")
}

// ── list ─────────────────────────────────────────────────────────────────────

func runSecretsList(cmd *cobra.Command) error {
	if secretsChain && strings.TrimSpace(secretsEnvFlag) == "" {
		return fmt.Errorf("--chain shows an environment's inheritance view; pass --env <env>")
	}
	ctx := cmd.Context()
	rt, err := newSecretsRuntime(ctx)
	if err != nil {
		return err
	}
	scope, label, err := rt.targetScope(ctx, true)
	if err != nil {
		return err
	}
	items, raw, err := rt.client.ListSecrets(ctx, scope, secretsChain)
	if err != nil {
		return err
	}
	if secretsJSONOut {
		fmt.Println(string(raw))
		return nil
	}
	if len(items) == 0 {
		fmt.Printf("No secrets in %s.\n", label)
		return nil
	}
	fmt.Print(renderSecretsTable(items, secretsChain))
	if warn := orphanWarning(items); warn != "" {
		fmt.Fprint(os.Stderr, warn)
	}
	return nil
}

// renderSecretsTable renders the metadata table. With chain it adds the
// serving-scope view (SERVES FROM, LOCKED). Values are structurally absent.
// A HEALTH column appears only when the scope contains at least one brokered
// row, so static-only scopes keep the pre-broker table shape.
func renderSecretsTable(items []configsurface.SecretMeta, chain bool) string {
	sorted := make([]configsurface.SecretMeta, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].SecretKey < sorted[j].SecretKey })

	showHealth := false
	for _, m := range sorted {
		if m.Brokered() {
			showHealth = true
			break
		}
	}

	headers := []string{"KEY", "SCOPE", "VERSION", "STATUS"}
	if showHealth {
		headers = append(headers, "HEALTH")
	}
	headers = append(headers, "ROTATED", "LAST USED")
	if chain {
		headers = append(headers, "SERVES FROM", "LOCKED")
	}
	rows := make([][]string, 0, len(sorted))
	for _, m := range sorted {
		row := []string{
			m.SecretKey,
			orDash(normalizeScopeName(m.EffectiveScope())),
			versionCell(m.Version),
			orDash(defaultString(m.Status, "active")),
		}
		if showHealth {
			row = append(row, secretHealthCell(m))
		}
		row = append(row, orDash(formatAge(m.LastRotatedAt)), orDash(formatAge(m.LastUsedAt)))
		if chain {
			locked := ""
			if m.Locked() {
				locked = "yes"
			}
			row = append(row, orDash(normalizeScopeName(m.ServesFrom)), orDash(locked))
		}
		rows = append(rows, row)
	}
	return renderColumns(headers, rows)
}

// secretHealthCell renders the derived binding-health axis for a row. Static
// rows have no binding and render "-"; a brokered row is "orphaned" when its
// connection can no longer mint, "ok" when healthy, and "unknown" when the
// health lookup was unreachable (never asserted orphaned on doubt).
func secretHealthCell(m configsurface.SecretMeta) string {
	if !m.Brokered() {
		return "-"
	}
	if m.IsOrphaned() {
		return "orphaned"
	}
	if m.Orphaned == nil {
		return "unknown"
	}
	return "ok"
}

// orphanWarning returns an actionable stderr notice when any brokered row is
// orphaned, naming the keys and pointing at the two remedies (repoint or
// revoke). Empty when nothing is orphaned. It goes to stderr so `--json` and
// piped stdout stay clean.
func orphanWarning(items []configsurface.SecretMeta) string {
	var orphaned []string
	for _, m := range items {
		if m.IsOrphaned() {
			orphaned = append(orphaned, m.SecretKey)
		}
	}
	if len(orphaned) == 0 {
		return ""
	}
	sort.Strings(orphaned)
	color := ui.ColorEnabledForWriter(os.Stderr)
	noun := "secret"
	if len(orphaned) > 1 {
		noun = "secrets"
	}
	return fmt.Sprintf(
		"\n%s %d brokered %s orphaned: %s\n  Their integration connection is no longer active, so they will FAIL to resolve at plan/run time.\n  Repoint them to a live connection or revoke them: `orun secrets revoke <KEY>`\n",
		ui.Yellow(color, "⚠"), len(orphaned), noun, strings.Join(orphaned, ", "),
	)
}

// normalizeScopeName maps the server's "organization" spelling to the CLI's
// "workspace" vocabulary (organization ≡ workspace, data-model.md §7).
func normalizeScopeName(s string) string {
	if strings.EqualFold(s, "organization") {
		return "workspace"
	}
	return s
}

func versionCell(v int) string {
	if v <= 0 {
		return "-"
	}
	return strconv.Itoa(v)
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

func defaultString(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

// renderColumns renders a fixed-width text table: headers then rows, columns
// sized to the widest cell, two spaces between columns.
func renderColumns(headers []string, rows [][]string) string {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len(h)
	}
	for _, row := range rows {
		for i, cell := range row {
			if i < len(widths) && len(cell) > widths[i] {
				widths[i] = len(cell)
			}
		}
	}
	var b strings.Builder
	writeRow := func(cells []string) {
		for i, cell := range cells {
			if i == len(cells)-1 {
				b.WriteString(cell)
				continue
			}
			b.WriteString(padRight(cell, widths[i]+2))
		}
		b.WriteString("\n")
	}
	writeRow(headers)
	for _, row := range rows {
		writeRow(row)
	}
	return b.String()
}

// ── rotate / revoke / versions ───────────────────────────────────────────────

// findSecretID lists the scope and returns the id of the row whose secretKey
// matches, preferring a row served by the requested rung. The error names the
// scope and points at `orun secrets list`.
func findSecretID(ctx context.Context, rt *secretsRuntime, scope configsurface.Scope, label, key string) (string, error) {
	items, _, err := rt.client.ListSecrets(ctx, scope, false)
	if err != nil {
		return "", err
	}
	var fallback string
	for _, m := range items {
		if m.SecretKey != key {
			continue
		}
		effective := normalizeScopeName(m.EffectiveScope())
		if effective == "" || effective == string(scope.Kind) {
			return m.ID, nil
		}
		if fallback == "" {
			fallback = m.ID
		}
	}
	if fallback != "" {
		return fallback, nil
	}
	return "", fmt.Errorf("secret %s not found in %s; run `orun secrets list%s` to inspect", key, label, listHintFlags(scope))
}

func listHintFlags(scope configsurface.Scope) string {
	switch scope.Kind {
	case configsurface.ScopeEnvironment:
		return " --env " + strings.TrimSpace(secretsEnvFlag)
	case configsurface.ScopeWorkspace:
		return " --workspace"
	default:
		return ""
	}
}

func runSecretsRotate(cmd *cobra.Command, key string) error {
	ctx := cmd.Context()
	rt, err := newSecretsRuntime(ctx)
	if err != nil {
		return err
	}
	scope, label, err := rt.targetScope(ctx, false)
	if err != nil {
		return err
	}
	id, err := findSecretID(ctx, rt, scope, label, key)
	if err != nil {
		return err
	}
	// --remint (RS4): rotate-now for a provider-rotated secret — send an empty
	// body; the server re-mints from the connected parent (RS3). No value is
	// read, so nothing secret ever touches this process.
	value := ""
	if !secretsRemint {
		value, err = readSecretValue(cmd)
		if err != nil {
			return err
		}
	} else if secretsValueFlag != "" {
		return fmt.Errorf("--remint and --value are mutually exclusive: the value is minted from the connected parent")
	}
	meta, err := rt.client.RotateSecret(ctx, scope, id, value)
	if err != nil {
		return renderSecretsWriteError(err, key)
	}
	if secretsJSONOut {
		return emitJSON(meta)
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	detail := label
	if meta != nil && meta.Version > 0 {
		detail += fmt.Sprintf(", version %d", meta.Version)
	}
	fmt.Printf("%s rotated %s (%s)\n", ui.Green(color, "✓"), key, detail)
	return nil
}

func runSecretsRevoke(cmd *cobra.Command, key string) error {
	ctx := cmd.Context()
	rt, err := newSecretsRuntime(ctx)
	if err != nil {
		return err
	}
	scope, label, err := rt.targetScope(ctx, false)
	if err != nil {
		return err
	}
	id, err := findSecretID(ctx, rt, scope, label, key)
	if err != nil {
		return err
	}
	if err := rt.client.DeleteSecret(ctx, scope, id); err != nil {
		return renderSecretsWriteError(err, key)
	}
	if secretsJSONOut {
		return emitJSON(map[string]interface{}{"key": key, "scope": string(scope.Kind), "revoked": true})
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	fmt.Printf("%s revoked %s (%s)\n", ui.Green(color, "✓"), key, label)
	return nil
}

func runSecretsVersions(cmd *cobra.Command, key string) error {
	ctx := cmd.Context()
	rt, err := newSecretsRuntime(ctx)
	if err != nil {
		return err
	}
	scope, label, err := rt.targetScope(ctx, false)
	if err != nil {
		return err
	}
	id, err := findSecretID(ctx, rt, scope, label, key)
	if err != nil {
		return err
	}
	versions, err := rt.client.ListVersions(ctx, scope, id)
	if err != nil {
		return err
	}
	if secretsJSONOut {
		return emitJSON(versions)
	}
	if len(versions) == 0 {
		fmt.Printf("No versions for %s in %s.\n", key, label)
		return nil
	}
	fmt.Print(renderVersionsTable(versions))
	return nil
}

// renderVersionsTable renders the version-metadata history, newest first.
func renderVersionsTable(versions []configsurface.SecretVersion) string {
	sorted := make([]configsurface.SecretVersion, len(versions))
	copy(sorted, versions)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Version > sorted[j].Version })

	rows := make([][]string, 0, len(sorted))
	for _, v := range sorted {
		rows = append(rows, []string{
			versionCell(v.Version),
			orDash(defaultString(v.Status, "active")),
			orDash(v.CreatedAt),
			orDash(string(v.CreatedBy)),
		})
	}
	return renderColumns([]string{"VERSION", "STATUS", "CREATED", "CREATED BY"}, rows)
}

// buildRotationBinding parses the create-from-parent flags (RS4) into the wire
// binding. The --from-broker arg is "provider/template" for readability; the
// server derives the authoritative provider from the connection, so the CLI
// only validates shape and passes the template through.
func buildRotationBinding(fromBroker, connection string, graceSeconds int, deliverTarget string) (*configsurface.SecretRotationBinding, error) {
	parts := strings.SplitN(fromBroker, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, fmt.Errorf("--from-broker must be provider/template (e.g. cloudflare/workers-deploy)")
	}
	template := parts[1]
	if connection == "" {
		return nil, fmt.Errorf("--from-broker requires --connection <int_…> (find it with the console's integration hub or connection detail page)")
	}
	if !strings.HasPrefix(connection, "int_") {
		return nil, fmt.Errorf("--connection must be an integration connection public id (int_…)")
	}
	if graceSeconds < 0 {
		return nil, fmt.Errorf("--grace-seconds must be non-negative")
	}
	binding := &configsurface.SecretRotationBinding{
		ConnectionID:  connection,
		Template:      template,
		DeliverTarget: deliverTarget,
	}
	if graceSeconds > 0 {
		binding.GraceSeconds = &graceSeconds
	}
	return binding, nil
}
