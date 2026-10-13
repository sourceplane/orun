package main

// `orun workspace` — pick the workspace you are working in, once, instead of
// spelling it on every command. The selection is a machine-wide DEFAULT stored
// in ~/.orun/config.yaml: it fills in when nothing more specific applies, and
// never overrides --workspace, ORUN_WORKSPACE, an intent.yaml, or a repo link
// (resolveScope owns that order; this file only reads and reports it).

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/cliauth"
	"github.com/sourceplane/orun/internal/ui"
	"github.com/spf13/cobra"
)

func registerWorkspaceCommand(root *cobra.Command) {
	workspaceCmd := &cobra.Command{
		Use:     "workspace",
		Aliases: []string{"ws"},
		Short:   "Show, list, and choose the workspace you are working in",
		Long: `Every Orun Cloud command runs against one workspace. Choose it once:

  orun workspace use <ws-id|slug>    select the working workspace
  orun workspace                     show the current one and where it came from
  orun workspace list                every workspace this session can reach

A workspace is resolved most-specific-first:

  --workspace  >  ORUN_WORKSPACE/ORUN_ORG  >  intent.yaml  >  this repo's link
  >  the workspace selected here

The selection is LAST on purpose: it fills in for bare commands without ever
retargeting a repo that declares or links its own workspace. "orun workspace"
always names the rung that actually won.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 1 {
				// "show"/"current" are what people naturally type for the
				// bare behavior — resolving them as workspace NAMES sent a
				// nonsense request upstream (GET /organizations/show, live).
				switch args[0] {
				case "show", "current":
					return runWorkspaceShow()
				}
				return runWorkspaceShowRemote(cmd.Context(), args[0])
			}
			return runWorkspaceShow()
		},
	}

	listCmd := &cobra.Command{
		Use:     "list",
		Aliases: []string{"ls"},
		Short:   "List the workspaces this session can reach (marks the current one)",
		Args:    cobra.NoArgs,
		RunE:    func(cmd *cobra.Command, args []string) error { return runWorkspaceList(cmd.Context()) },
	}

	var clear bool
	useCmd := &cobra.Command{
		Use:     "use [<ws-id|slug>]",
		Aliases: []string{"switch", "select"},
		Short:   "Select the working workspace (no argument: pick from a list)",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if clear {
				if len(args) > 0 {
					return fmt.Errorf("--clear takes no workspace argument")
				}
				return runWorkspaceClear()
			}
			target := ""
			if len(args) == 1 {
				target = args[0]
			}
			return runWorkspaceUse(target)
		},
	}
	useCmd.Flags().BoolVar(&clear, "clear", false, "Unset the selection (fall back to the repo link / explicit flags)")

	var wsCreateSlug, wsCreateAccount string
	createCmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new workspace owned by the current session",
		Long: `Create a new workspace and print its ws_… id and slug. Creating one does not
select it; follow with "orun workspace use <slug>".

A login that owns more than one account must name the account the workspace
belongs to with --account <ws_…|slug>; without it the platform refuses and
the CLI lists the accounts to choose from.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCloudWorkspaceCreate(cmd.Context(), workspaceCreateOpts{
				Name:        args[0],
				Slug:        wsCreateSlug,
				Account:     wsCreateAccount,
				CommandPath: cmd.CommandPath(),
			})
		},
	}
	addWorkspaceCreateFlags(createCmd, &wsCreateSlug, &wsCreateAccount)

	workspaceCmd.AddCommand(listCmd, useCmd, createCmd)
	root.AddCommand(workspaceCmd)
}

// currentWorkspace reports the workspace a bare command in this directory would
// use and the rung it came from — the same chain resolveScope walks, minus the
// per-command flag (which is not parsed here).
func currentWorkspace() (ws, source string) {
	intent := loadIntentForCloudConfig()
	intentOrg, _, _ := intentScope(intent)
	linkOrg := ""
	if backendURL := resolveBackendURLWithConfig(intent, ""); backendURL != "" {
		if repo, err := resolveRepoContext(backendURL); err == nil && repo != nil {
			linkOrg = repo.OrgID
		}
	}
	return workspaceSource("", intentOrg, linkOrg)
}

func runWorkspaceShow() error {
	color := ui.ColorEnabledForWriter(os.Stdout)
	ws, source := currentWorkspace()
	sel := cliauth.SelectedWorkspace()
	if ws == "" {
		fmt.Println("No workspace resolves here.")
		fmt.Printf("  checked: %s\n", workspaceResolutionOrder)
		if orgs := sessionOrgs(); len(orgs) > 0 {
			fmt.Printf("  choose one: `orun workspace use %s`  (all of them: `orun workspace list`)\n", orgs[0].Label)
		} else {
			fmt.Println("  no workspaces on this session — `orun auth status`, or create one: `orun workspace create <name>`")
		}
		return nil
	}
	fmt.Printf("%s %s\n", ui.Green(color, "workspace:"), workspaceDisplay(ws))
	fmt.Printf("  from: %s\n", source)
	if selID := strings.TrimSpace(sel.ID); selID != "" && selID != ws {
		// The honest case: a selection exists but something more specific wins
		// here. Say so rather than letting the user wonder why `use` "did
		// nothing" in this directory.
		fmt.Printf("  note: `orun workspace use` is set to %s, which %s overrides here\n", sel.Label(), source)
	}
	fmt.Printf("  change: `orun workspace use <ws-id|slug>`, or --workspace <ws> for one command\n")
	return nil
}

func runWorkspaceList(ctx context.Context) error {
	orgs := sessionOrgs()
	if len(orgs) == 0 {
		if creds, err := cliauth.LoadSession(); err != nil || creds == nil {
			return errNotLoggedIn()
		}
		fmt.Println("This session belongs to no workspaces yet — create one: `orun workspace create <name>`")
		return nil
	}
	current, source := currentWorkspace()
	fmt.Print(renderWorkspaceList(withOrgAccounts(ctx, orgs), current))
	if current != "" {
		fmt.Printf("\n* current, from %s. Switch: `orun workspace use <slug>`\n", source)
		return nil
	}
	fmt.Printf("\nNone selected. Choose one: `orun workspace use %s`\n", orgs[0].Label)
	return nil
}

// renderWorkspaceList is the `workspace list` table. The ACCOUNT column —
// which account each workspace belongs to, the value `workspace create
// --account` takes — appears only when at least one workspace's account is
// known, so a backend that does not report accounts prints the old table.
func renderWorkspaceList(orgs []orgChoice, current string) string {
	headers := []string{"", "SLUG", "WORKSPACE ID", "ROLE"}
	if anyOrgHasAccount(orgs) {
		headers = append(headers, "ACCOUNT")
	}
	rows := make([][]string, 0, len(orgs))
	for _, o := range orgs {
		mark := ""
		if o.matches(current) {
			mark = "*"
		}
		row := []string{mark, orDash(o.Slug), o.primaryID(), orDash(o.Role)}
		if len(headers) > 4 {
			row = append(row, orDash(o.accountLabel()))
		}
		rows = append(rows, row)
	}
	return renderColumns(headers, rows)
}

func runWorkspaceUse(target string) error {
	orgs := sessionOrgs()
	if len(orgs) == 0 {
		if creds, err := cliauth.LoadSession(); err != nil || creds == nil {
			return errNotLoggedIn()
		}
		return fmt.Errorf("this session belongs to no workspaces yet — create one: `orun workspace create <name>`")
	}
	target = strings.TrimSpace(target)
	if target == "" {
		if !termIsInteractive() {
			return fmt.Errorf("`orun workspace use` needs a workspace when not running interactively: `orun workspace use <ws-id|slug>`\n  workspaces: %s", joinOrgLabels(orgs))
		}
		idx, err := pickFromOrgs("Choose the workspace to work in:", orgs)
		if err != nil {
			return err
		}
		target = orgs[idx].primaryID()
	}
	choice := matchOrgChoice(orgs, target)
	if choice == nil {
		return errUnknownWorkspace(target, orgs)
	}
	if err := cliauth.SelectWorkspace(cliauth.WorkspaceSelection{
		ID:    choice.primaryID(),
		Slug:  choice.Slug,
		SetAt: time.Now().UTC().Format(time.RFC3339),
	}); err != nil {
		return fmt.Errorf("save workspace selection: %w", err)
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	fmt.Printf("%s working workspace: %s\n", ui.Green(color, "✓"), workspaceDisplay(choice.primaryID(), choice.Slug))
	// Report what will ACTUALLY happen in this directory. A repo that links or
	// declares its own workspace still wins, and finding that out from a failed
	// command later is worse than one line here.
	if ws, source := currentWorkspace(); ws != "" && ws != choice.primaryID() && source != "`orun workspace use`" {
		fmt.Printf("  note: in this directory %s still resolves %s — the selection applies everywhere else\n", source, workspaceDisplay(ws))
	}
	return nil
}

func runWorkspaceClear() error {
	sel := cliauth.SelectedWorkspace()
	if strings.TrimSpace(sel.ID) == "" {
		fmt.Println("No working workspace was selected.")
		return nil
	}
	if err := cliauth.SelectWorkspace(cliauth.WorkspaceSelection{}); err != nil {
		return fmt.Errorf("clear workspace selection: %w", err)
	}
	fmt.Printf("✓ cleared the working workspace (was %s)\n", sel.Label())
	return nil
}

// matchOrgChoice resolves a user-typed workspace against the session's orgs by
// slug, ws_… id, or org_… id — case-insensitively, since slugs are lowercase
// but people type what they see.
func matchOrgChoice(orgs []orgChoice, target string) *orgChoice {
	for i := range orgs {
		if orgs[i].matches(target) {
			return &orgs[i]
		}
	}
	return nil
}

func errUnknownWorkspace(target string, orgs []orgChoice) error {
	var b strings.Builder
	fmt.Fprintf(&b, "no workspace %q on this session", target)
	labels := make([]string, 0, len(orgs))
	for _, o := range orgs {
		labels = append(labels, o.Label)
	}
	if suggestion := ui.SuggestMatch(target, labels); suggestion != "" {
		fmt.Fprintf(&b, "\n\ndid you mean:\n  orun workspace use %s", suggestion)
	}
	fmt.Fprintf(&b, "\n\nworkspaces: %s", strings.Join(labels, ", "))
	fmt.Fprintf(&b, "\n(joined a new one? `orun auth login` refreshes the session's workspace list)")
	return fmt.Errorf("%s", b.String())
}

func joinOrgLabels(orgs []orgChoice) string {
	labels := make([]string, 0, len(orgs))
	for _, o := range orgs {
		labels = append(labels, o.Label)
	}
	return strings.Join(labels, ", ")
}

// workspaceDisplay renders "slug (ws_…)" when both are known, else whichever
// identifier we have.
func workspaceDisplay(id string, slug ...string) string {
	s := ""
	if len(slug) > 0 {
		s = strings.TrimSpace(slug[0])
	}
	if s == "" {
		if c := matchOrgChoice(sessionOrgs(), id); c != nil {
			s = c.Slug
		}
	}
	if s == "" || s == id {
		return id
	}
	return fmt.Sprintf("%s (%s)", s, id)
}

// runWorkspaceShowRemote resolves a NAMED workspace against the backend and
// prints its identity. Unlike the bare form (local resolution state), this
// works for any credential that can read the workspace — including
// workspace-scoped API tokens (ORUN_TOKEN), which see no memberships list.
// The "slug:" line is a stable, machine-parsable contract (flows read it).
func runWorkspaceShowRemote(ctx context.Context, target string) error {
	backendURL, err := requireBackendURL(loadIntentForCloudConfig(), cloudBackendURL)
	if err != nil {
		return err
	}
	token, err := cloudSessionToken(ctx, backendURL)
	if err != nil {
		return err
	}
	client := cliauth.NewBackendClient(backendURL, version)
	org, err := client.GetOrg(ctx, token, target)
	if err != nil {
		return fmt.Errorf("workspace %q: %w", target, err)
	}
	primary := strings.TrimSpace(org.WorkspaceRef)
	if primary == "" {
		primary = org.ID
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	fmt.Printf("%s %s\n", ui.Green(color, "workspace:"), primary)
	fmt.Printf("  slug: %s\n", org.Slug)
	fmt.Printf("  name: %s\n", org.Name)
	fmt.Printf("  id:   %s\n", org.ID)
	if account := accountDisplay(org.AccountID, org.AccountSlug); account != "" {
		fmt.Printf("  account: %s\n", account)
	}
	return nil
}
