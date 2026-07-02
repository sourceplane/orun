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
)

func registerSecretsCommand(root *cobra.Command) {
	secretsCmd := &cobra.Command{
		Use:   "secrets",
		Short: "Manage secret values and metadata (write-only; values are never displayed)",
		Long: `Manage secrets on Orun Cloud: set, import, rotate, revoke, and inspect
metadata. The surface is write-only — values go up, only metadata comes back.

Scope defaults to the linked project: --env <env> targets an environment rung,
--project the project-wide rung, --workspace the workspace-shared rung.`,
	}
	secretsCmd.PersistentFlags().StringVar(&secretsBackendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	secretsCmd.PersistentFlags().StringVar(&secretsOrgFlag, "org", "", "Workspace slug/id override for scope resolution (defaults to the linked workspace)")

	setCmd := &cobra.Command{
		Use:   "set <KEY>",
		Short: "Set a secret value (reads the value from stdin unless --value is given)",
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
		Short: "Append a new version of a secret (value from stdin unless --value)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretsRotate(cmd, args[0])
		},
	}
	rotateCmd.Flags().StringVar(&secretsEnvFlag, "env", "", "Target environment (slug)")
	rotateCmd.Flags().StringVar(&secretsValueFlag, "value", "", "New secret value (prefer stdin: --value may land in shell history)")

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

	versionsCmd := &cobra.Command{
		Use:   "versions <KEY> --env <env>",
		Short: "Show a secret's version history (metadata only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSecretsVersions(cmd, args[0])
		},
	}
	versionsCmd.Flags().StringVar(&secretsEnvFlag, "env", "", "Target environment (slug)")

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
	revealCmd.Flags().StringVar(&secretsEnvFlag, "env", "", "Target environment (slug)")
	revealCmd.Flags().BoolVar(&secretsBreakGlass, "break-glass", false, "Required acknowledgement that this is an audited emergency reveal")
	revealCmd.Flags().StringVar(&secretsReason, "reason", "", "Required justification, recorded in the audit log")

	secretsCmd.AddCommand(setCmd, importCmd, listCmd, rotateCmd, revokeCmd, versionsCmd, revealCmd)
	root.AddCommand(secretsCmd)
}

// runSecretsReveal implements the break-glass reveal — the one value-returning
// command (SD-3). It hard-requires --break-glass and --reason, prints the value
// to stdout only (with a stderr warning), and never writes it to any file.
func runSecretsReveal(cmd *cobra.Command, key string) error {
	ctx := cmd.Context()
	if !secretsBreakGlass {
		return fmt.Errorf("reveal requires --break-glass: this is an audited emergency action, not the normal path (workloads receive secrets via `orun run`)")
	}
	if strings.TrimSpace(secretsReason) == "" {
		return fmt.Errorf("reveal requires --reason: the justification is recorded in the audit log")
	}
	rt, err := newSecretsRuntime(ctx)
	if err != nil {
		return err
	}
	if strings.TrimSpace(secretsEnvFlag) == "" {
		return rt.errEnvRequired(false)
	}
	scope, label, err := rt.environmentScope(ctx, secretsEnvFlag)
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

// readSecretValue returns the value for set/rotate: from --value (with a
// shell-history warning) or from stdin. On a TTY a short prompt goes to
// stderr; all of stdin is read and one trailing newline is trimmed.
func readSecretValue(cmd *cobra.Command) (string, error) {
	if cmd.Flags().Changed("value") {
		fmt.Fprintln(os.Stderr, "warning: --value may be recorded in your shell history; prefer piping the value on stdin")
		if secretsValueFlag == "" {
			return "", fmt.Errorf("--value is empty")
		}
		return secretsValueFlag, nil
	}
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintln(os.Stderr, "Enter value (input hidden not guaranteed; end with EOF):")
	}
	data, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("reading value from stdin: %w", err)
	}
	value := trimOneTrailingNewline(string(data))
	if value == "" {
		return "", fmt.Errorf("no value provided: pipe the value on stdin or pass --value")
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

	value, err := readSecretValue(cmd)
	if err != nil {
		return err
	}

	req := configsurface.CreateSecretRequest{
		SecretKey:      key,
		Value:          value,
		DisplayName:    secretsDisplayName,
		RotationPolicy: secretsRotation,
		Personal:       secretsPersonal,
	}
	if secretsLocked {
		overridable := false
		req.Overridable = &overridable
	}
	meta, err := rt.client.CreateSecret(ctx, scope, req)
	if err != nil {
		return renderSecretsWriteError(err, key)
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
	fmt.Print(renderImportSummary(rows))
	fmt.Printf("\n%s → %s: %s\n", secretsFromDotenv, label, summarizeImportCounts(rows))
	if importErr != nil {
		return importErr
	}
	return nil
}

// importSummaryRow is one line of the import summary; it carries no value.
type importSummaryRow struct {
	Key    string
	Result string
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
	return nil
}

// renderSecretsTable renders the metadata table. With chain it adds the
// serving-scope view (SERVES FROM, LOCKED). Values are structurally absent.
func renderSecretsTable(items []configsurface.SecretMeta, chain bool) string {
	sorted := make([]configsurface.SecretMeta, len(items))
	copy(sorted, items)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].SecretKey < sorted[j].SecretKey })

	headers := []string{"KEY", "SCOPE", "VERSION", "STATUS", "ROTATED", "LAST USED"}
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
			orDash(formatAge(m.LastRotatedAt)),
			orDash(formatAge(m.LastUsedAt)),
		}
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
	if strings.TrimSpace(secretsEnvFlag) == "" {
		return rt.errEnvRequired(false)
	}
	scope, label, err := rt.environmentScope(ctx, secretsEnvFlag)
	if err != nil {
		return err
	}
	id, err := findSecretID(ctx, rt, scope, label, key)
	if err != nil {
		return err
	}
	value, err := readSecretValue(cmd)
	if err != nil {
		return err
	}
	meta, err := rt.client.RotateSecret(ctx, scope, id, value)
	if err != nil {
		return renderSecretsWriteError(err, key)
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
	if strings.TrimSpace(secretsEnvFlag) == "" {
		return rt.errEnvRequired(false)
	}
	scope, label, err := rt.environmentScope(ctx, secretsEnvFlag)
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

// jsonCompact is a tiny helper kept for --json symmetry with other commands.
func jsonCompact(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return "[]"
	}
	return string(data)
}
