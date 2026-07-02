package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/configsurface"
	"github.com/sourceplane/orun/internal/discovery"
	"github.com/sourceplane/orun/internal/loader"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/secretpolicy"
	"github.com/sourceplane/orun/internal/secretref"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

// `orun policy` — manage and test the portable Layer-2 SecretPolicy documents
// (specs/orun-secrets/cli-surface.md §2, policy-model.md). list/show/lint are
// offline (local Stack + repo policies/); test/push talk to the config surface.
// No secret value is involved — policies are conditions, not values.

var (
	policyBackendURL    string
	policyOrgFlag       string
	policyRefFlag       string
	policyAsFlag        string
	policyEnvFlag       string
	policyComponentType string
	policyPlatformFlag  string
	policyServesFrom    string
	policyBranchFlag    string
	policyDeclaredFlag  bool
)

func registerPolicyCommand(root *cobra.Command) {
	policyCmd := &cobra.Command{
		Use:   "policy",
		Short: "Manage and test portable secret-access policy (Layer 2)",
		Long: `Inspect, lint, test, and push SecretPolicy documents — the portable
Layer-2 conditions (who / what / where / how) over secret access.

list, show, and lint are offline: they load the resolved Stack and the repo's
policies/ overlays. test and push talk to Orun Cloud.`,
	}
	policyCmd.PersistentFlags().StringVar(&policyBackendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	policyCmd.PersistentFlags().StringVar(&policyOrgFlag, "org", "", "Workspace slug/id override for scope resolution (defaults to the linked workspace)")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List SecretPolicy documents in scope, grouped by tier",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return runPolicyList() },
	}

	showCmd := &cobra.Command{
		Use:   "show <name>",
		Short: "Show one SecretPolicy document's rules",
		Args:  cobra.ExactArgs(1),
		RunE:  func(cmd *cobra.Command, args []string) error { return runPolicyShow(args[0]) },
	}

	lintCmd := &cobra.Command{
		Use:   "lint",
		Short: "Check predicate vocabulary and narrow-only overlay rules (exits non-zero on findings)",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return runPolicyLint() },
	}

	testCmd := &cobra.Command{
		Use:   "test --ref secret://ws/prj/env/KEY --as <subject> --env <env>",
		Short: "Dry-run a two-layer access decision against the backend engine",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return runPolicyTest(cmd) },
	}
	testCmd.Flags().StringVar(&policyRefFlag, "ref", "", "The secret:// reference to test")
	testCmd.Flags().StringVar(&policyAsFlag, "as", "", "Subject to test as (user:<id>, team:<slug>, service_principal:<id>, workflow, *authenticated)")
	testCmd.Flags().StringVar(&policyEnvFlag, "env", "", "Environment slug (defaults to the ref's env)")
	testCmd.Flags().StringVar(&policyComponentType, "component-type", "", "component.type fact")
	testCmd.Flags().StringVar(&policyPlatformFlag, "platform", "local-cli", "Execution platform (local-cli, ci-oidc, service)")
	testCmd.Flags().StringVar(&policyServesFrom, "serves-from", "", "servesFrom fact (environment, project, workspace, account)")
	testCmd.Flags().StringVar(&policyBranchFlag, "branch", "", "trigger.branch fact")
	testCmd.Flags().BoolVar(&policyDeclaredFlag, "declared", false, "trigger.declared fact")
	_ = testCmd.MarkFlagRequired("ref")
	_ = testCmd.MarkFlagRequired("as")

	pushCmd := &cobra.Command{
		Use:   "push",
		Short: "Validate and push the resolved tier-tagged documents to the backend",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return runPolicyPush(cmd) },
	}

	policyCmd.AddCommand(listCmd, showCmd, testCmd, lintCmd, pushCmd)
	root.AddCommand(policyCmd)
}

// ── offline tier loading ─────────────────────────────────────────────────────

// policyIntentContext locates the intent file and repo root for offline loads,
// honoring the --intent flag and falling back to auto-discovery.
func policyIntentContext() (intentPath, repoRoot string) {
	intentPath = strings.TrimSpace(intentFile)
	repoRoot = strings.TrimSpace(intentRoot)
	if repoRoot == "" && intentPath != "" {
		repoRoot = filepath.Dir(intentPath)
	}
	if repoRoot == "" || repoRoot == "." {
		if cwd, err := os.Getwd(); err == nil {
			if p, dir, derr := discovery.FindIntentFile(cwd); derr == nil {
				if intentPath == "" || intentPath == "intent.yaml" {
					intentPath = p
				}
				repoRoot = dir
			} else if repoRoot == "" {
				repoRoot = cwd
			}
		}
	}
	return intentPath, repoRoot
}

// stackRoots resolves the local roots of the resolved Stacks (composition
// sources) for tier discovery. Best-effort: a repo with no compositions still
// yields intent-tier policies.
func stackRoots(intentPath string) []string {
	intent, err := loader.LoadIntent(intentPath)
	if err != nil || intent == nil {
		return nil
	}
	reg, err := loader.LoadCompositionsForIntent(intent, intentPath, configDir)
	if err != nil || reg == nil {
		return nil
	}
	roots := make([]string, 0, len(reg.SourceRoots))
	seen := map[string]bool{}
	for _, root := range reg.SourceRoots {
		root = strings.TrimSpace(root)
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		roots = append(roots, root)
	}
	sort.Strings(roots)
	return roots
}

// loadPolicyTiersStrict loads all three tiers, failing on the first parse error
// (used by push).
func loadPolicyTiersStrict() (secretpolicy.Tiers, error) {
	intentPath, repoRoot := policyIntentContext()
	var tiers secretpolicy.Tiers
	for _, root := range stackRoots(intentPath) {
		comp, stack, err := secretpolicy.LoadStackRoot(os.DirFS(root))
		if err != nil {
			return tiers, err
		}
		tiers.Composition = append(tiers.Composition, comp...)
		tiers.Stack = append(tiers.Stack, stack...)
	}
	if repoRoot != "" {
		intentDocs, err := secretpolicy.LoadIntentPolicies(os.DirFS(repoRoot))
		if err != nil {
			return tiers, err
		}
		tiers.Intent = append(tiers.Intent, intentDocs...)
	}
	return tiers, nil
}

// loadPolicyTiersLenient loads all three tiers, collecting parse failures as
// findings instead of aborting (used by list/show/lint).
func loadPolicyTiersLenient() (secretpolicy.Tiers, []secretpolicy.Finding) {
	intentPath, repoRoot := policyIntentContext()
	var tiers secretpolicy.Tiers
	var findings []secretpolicy.Finding
	for _, root := range stackRoots(intentPath) {
		comp, stack, fs := secretpolicy.LoadStackRootLenient(os.DirFS(root))
		tiers.Composition = append(tiers.Composition, comp...)
		tiers.Stack = append(tiers.Stack, stack...)
		findings = append(findings, fs...)
	}
	if repoRoot != "" {
		intentDocs, fs := secretpolicy.LoadIntentPoliciesLenient(os.DirFS(repoRoot))
		tiers.Intent = append(tiers.Intent, intentDocs...)
		findings = append(findings, fs...)
	}
	return tiers, findings
}

// ── list ─────────────────────────────────────────────────────────────────────

func runPolicyList() error {
	tiers, findings := loadPolicyTiersLenient()
	printPolicyLoadFindings(findings)
	groups := []struct {
		title string
		docs  []secretpolicy.Document
	}{
		{"COMPOSITION", tiers.Composition},
		{"STACK", tiers.Stack},
		{"INTENT", tiers.Intent},
	}
	printed := false
	for _, g := range groups {
		if len(g.docs) == 0 {
			continue
		}
		printed = true
		fmt.Printf("%s\n", g.title)
		rows := make([][]string, 0, len(g.docs))
		for _, d := range g.docs {
			rows = append(rows, []string{d.Name, d.Source, fmt.Sprintf("%d", len(d.Rules))})
		}
		fmt.Print(indentBlock(renderColumns([]string{"NAME", "SOURCE", "RULES"}, rows)))
		fmt.Println()
	}
	if !printed {
		fmt.Println("No SecretPolicy documents in scope.")
	}
	return nil
}

func indentBlock(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	for i, l := range lines {
		lines[i] = "  " + l
	}
	return strings.Join(lines, "\n") + "\n"
}

// ── show ─────────────────────────────────────────────────────────────────────

func runPolicyShow(name string) error {
	tiers, findings := loadPolicyTiersLenient()
	printPolicyLoadFindings(findings)
	for _, d := range tiers.Ordered() {
		if d.Name == name {
			fmt.Print(renderPolicyDocument(d))
			return nil
		}
	}
	return fmt.Errorf("no SecretPolicy named %q in scope; run `orun policy list`", name)
}

func renderPolicyDocument(d secretpolicy.Document) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s  (tier %s, source %s)\n", d.Name, d.Tier, d.Source)
	if d.Path != "" {
		fmt.Fprintf(&b, "  %s\n", d.Path)
	}
	for _, r := range d.Rules {
		fmt.Fprintf(&b, "\n  rule %s  [%s]\n", r.ID, r.Effect)
		if len(r.Subjects) > 0 {
			fmt.Fprintf(&b, "    subjects: %s\n", strings.Join(r.Subjects, ", "))
		} else {
			fmt.Fprintf(&b, "    subjects: (any)\n")
		}
		fmt.Fprintf(&b, "    scope:    env=%s key=%s\n", r.Scope.Env, r.Scope.Key)
		if len(r.When) > 0 {
			preds := make([]string, 0, len(r.When))
			for _, p := range r.When {
				preds = append(preds, p.String())
			}
			fmt.Fprintf(&b, "    when:     %s\n", strings.Join(preds, " AND "))
		}
	}
	return b.String()
}

// ── lint ─────────────────────────────────────────────────────────────────────

func runPolicyLint() error {
	tiers, findings := loadPolicyTiersLenient()
	findings = append(findings, secretpolicy.Lint(tiers)...)
	color := ui.ColorEnabledForWriter(os.Stdout)
	errCount := 0
	for _, f := range findings {
		label := ui.Yellow(color, "warning")
		if f.Severity == secretpolicy.SevError {
			label = ui.Red(color, "error")
			errCount++
		}
		loc := f.Source
		if f.RuleID != "" {
			loc = fmt.Sprintf("%s rule %s", f.Source, f.RuleID)
		}
		if loc == "" {
			loc = f.Path
		}
		fmt.Printf("%s [%s] %s: %s\n", label, f.Kind, loc, f.Message)
	}
	if len(findings) == 0 {
		fmt.Printf("%s no policy findings (%d documents)\n", ui.Green(color, "✓"), len(tiers.Ordered()))
		return nil
	}
	if errCount > 0 {
		return fmt.Errorf("%d policy error(s)", errCount)
	}
	return nil
}

func printPolicyLoadFindings(findings []secretpolicy.Finding) {
	for _, f := range findings {
		if f.Severity == secretpolicy.SevError {
			fmt.Fprintf(os.Stderr, "⚠ %s: %s\n", f.Source, f.Message)
		}
	}
}

// ── test ─────────────────────────────────────────────────────────────────────

func runPolicyTest(cmd *cobra.Command) error {
	ref, err := secretref.Parse(strings.TrimSpace(policyRefFlag))
	if err != nil {
		return fmt.Errorf("--ref: %w", err)
	}
	subject, err := parseAsSubject(policyAsFlag)
	if err != nil {
		return fmt.Errorf("--as: %w", err)
	}
	env := strings.TrimSpace(policyEnvFlag)
	if env == "" {
		env = ref.Env
	}
	platform := strings.TrimSpace(policyPlatformFlag)
	if !validPolicyPlatform(platform) {
		return fmt.Errorf("--platform must be one of: local-cli, ci-oidc, service")
	}

	req := configsurface.EvaluateSecretPolicyRequest{
		Key:        ref.Key,
		Env:        env,
		Platform:   platform,
		Subject:    subject,
		ServesFrom: strings.TrimSpace(policyServesFrom),
	}
	if strings.TrimSpace(policyComponentType) != "" {
		req.Component = &configsurface.EvalComponent{Type: strings.TrimSpace(policyComponentType)}
	}
	if strings.TrimSpace(policyBranchFlag) != "" || policyDeclaredFlag {
		req.Trigger = &configsurface.EvalTrigger{Branch: strings.TrimSpace(policyBranchFlag), Declared: policyDeclaredFlag}
	}

	ctx := cmd.Context()
	rt, err := newPolicyRuntime(ctx)
	if err != nil {
		return err
	}
	scope := rt.evaluateScope()
	res, err := rt.client.EvaluateSecretPolicy(ctx, scope, req)
	if err != nil {
		return err
	}
	fmt.Print(renderTestDecision(ref.String(), strings.TrimSpace(policyAsFlag), res))
	return nil
}

func validPolicyPlatform(p string) bool {
	switch p {
	case "local-cli", "ci-oidc", "service":
		return true
	default:
		return false
	}
}

// parseAsSubject parses the --as value into the evaluate route's subject shape.
// Multiple comma-separated tokens accumulate (team: entries build teams[]).
func parseAsSubject(as string) (configsurface.EvalSubject, error) {
	trimmed := strings.TrimSpace(as)
	if trimmed == "" {
		return configsurface.EvalSubject{}, fmt.Errorf("empty subject")
	}
	var subj configsurface.EvalSubject
	for _, tok := range strings.Split(trimmed, ",") {
		tok = strings.TrimSpace(tok)
		if tok == "" {
			continue
		}
		switch {
		case tok == "*authenticated":
			// any authenticated caller; leave id/kind at defaults
		case tok == "workflow" || tok == "user" || tok == "service_principal":
			subj.Kind = tok
		case strings.HasPrefix(tok, "user:"):
			subj.ID = strings.TrimSpace(tok[len("user:"):])
			subj.Kind = "user"
			if subj.ID == "" {
				return configsurface.EvalSubject{}, fmt.Errorf("subject %q is missing its id", tok)
			}
		case strings.HasPrefix(tok, "service_principal:"):
			subj.ID = strings.TrimSpace(tok[len("service_principal:"):])
			subj.Kind = "service_principal"
			if subj.ID == "" {
				return configsurface.EvalSubject{}, fmt.Errorf("subject %q is missing its id", tok)
			}
		case strings.HasPrefix(tok, "team:"):
			slug := strings.TrimSpace(tok[len("team:"):])
			if slug == "" {
				return configsurface.EvalSubject{}, fmt.Errorf("subject %q is missing its slug", tok)
			}
			subj.Teams = append(subj.Teams, slug)
		default:
			return configsurface.EvalSubject{}, fmt.Errorf("unknown subject %q (want user:<id>, team:<slug>, service_principal:<id>, workflow, user, service_principal, or *authenticated)", tok)
		}
	}
	if subj.Kind == "" {
		subj.Kind = "user"
	}
	return subj, nil
}

// renderTestDecision renders the two-layer decision as deterministic plain text.
func renderTestDecision(ref, subjectLabel string, res *configsurface.EvaluateResult) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Decision for %s as %s:\n", ref, subjectLabel)
	fmt.Fprintf(&b, "  Layer 1 (RBAC)   %s  action=%s reason=%s\n",
		verdict(res.Layer1.Allow), orNA(res.Layer1.Action), orNA(res.Layer1.Reason))
	rule := res.Layer2.RuleID
	if rule == "" {
		rule = "(none)"
	}
	fmt.Fprintf(&b, "  Layer 2 (policy) %s  ruleId=%s reason=%s\n",
		verdict(res.Layer2.Allow), rule, orNA(res.Layer2.Reason))
	fmt.Fprintf(&b, "  => %s\n", strings.ToUpper(verdict(res.Decision.Allow)))
	return b.String()
}

func verdict(allow bool) string {
	if allow {
		return "allow"
	}
	return "deny"
}

func orNA(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}

// ── push ─────────────────────────────────────────────────────────────────────

func runPolicyPush(cmd *cobra.Command) error {
	tiers, err := loadPolicyTiersStrict()
	if err != nil {
		return err
	}
	if err := secretpolicy.Validate(tiers); err != nil {
		return err
	}
	docs := tiers.Ordered()
	if len(docs) == 0 {
		fmt.Println("No SecretPolicy documents to push.")
		return nil
	}

	ctx := cmd.Context()
	rt, err := newPolicyRuntime(ctx)
	if err != nil {
		return err
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	for _, d := range docs {
		scope, err := rt.pushScope(d.Tier)
		if err != nil {
			return err
		}
		document, err := d.DocumentJSON()
		if err != nil {
			return err
		}
		result, err := rt.client.PutSecretPolicy(ctx, scope, configsurface.PutSecretPolicyRequest{
			Name:     d.Name,
			Tier:     string(d.Tier),
			Source:   d.Source,
			Document: document,
		})
		if err != nil {
			return fmt.Errorf("push %s (%s): %w", d.Name, d.Tier, err)
		}
		state := "unchanged"
		if result.Updated {
			state = "updated"
		}
		fmt.Printf("%s %s (%s, %s) %s\n", ui.Green(color, "✓"), d.Name, d.Tier, d.Source, state)
	}
	return nil
}

// ── backend runtime (auth + scope) ───────────────────────────────────────────

type policyRuntime struct {
	client  *configsurface.Client
	org     string
	project string
}

func newPolicyRuntime(ctx context.Context) (*policyRuntime, error) {
	intent := loadIntentForCloudConfig()
	backendURL, err := requireBackendURL(intent, policyBackendURL)
	if err != nil {
		return nil, err
	}
	linkOrg, linkProject := "", ""
	if repo, repoErr := resolveRepoContext(backendURL); repoErr == nil && repo != nil {
		linkOrg, linkProject = repo.OrgID, repo.ProjectID
	}
	intentOrg, intentProject, _ := intentScope(intent)
	scope := resolveScope(policyOrgFlag, "", intentOrg, intentProject, linkOrg, linkProject)
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
	return &policyRuntime{
		client:  configsurface.NewClient(backendURL, version, tokenSrc),
		org:     scope.OrgID,
		project: scope.ProjectID,
	}, nil
}

// evaluateScope prefers the project rung (which sees stack/composition +
// intent-tier documents) and falls back to workspace.
func (rt *policyRuntime) evaluateScope() configsurface.Scope {
	if strings.TrimSpace(rt.project) != "" {
		return configsurface.Scope{Kind: configsurface.ScopeProject, Org: rt.org, Project: rt.project}
	}
	return configsurface.Scope{Kind: configsurface.ScopeWorkspace, Org: rt.org}
}

// pushScope maps a document's tier to its tenancy scope: stack/composition are
// workspace-wide; intent overlays are project-scoped (data-model.md §7d).
func (rt *policyRuntime) pushScope(tier secretpolicy.Tier) (configsurface.Scope, error) {
	switch tier {
	case secretpolicy.TierIntent:
		if strings.TrimSpace(rt.project) == "" {
			return configsurface.Scope{}, fmt.Errorf("cannot push intent-tier policy: no linked project")
		}
		return configsurface.Scope{Kind: configsurface.ScopeProject, Org: rt.org, Project: rt.project}, nil
	default:
		return configsurface.Scope{Kind: configsurface.ScopeWorkspace, Org: rt.org}, nil
	}
}
