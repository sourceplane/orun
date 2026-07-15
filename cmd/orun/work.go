package main

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/worklens"
)

func registerWorkCommand(root *cobra.Command) {
	workCmd := &cobra.Command{
		Use:   "work",
		Short: "The work plane: import spec trees, inspect derived lifecycle",
		Long: `The work plane (specs/orun-work, v2 — the work lens).

Work lifecycle is a derived query over two append-only logs, never a stored
status. The CLI surface is deliberately small; the board lives in Orun Cloud.

Subcommands:
  import    Map a specs/ tree to Spec/Task envelopes and apply to Orun Cloud
  list      Show the workspace's derived lifecycle (rungs with evidence)
  edit      Edit an item's envelope (title/description/owner/target/…)
  cancel    Retire an item (task or epic) — the append-only "delete"

Run 'orun work <subcommand> --help' for details.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	registerWorkImportCommand(workCmd)
	registerWorkListCommand(workCmd)
	registerWorkEditCommand(workCmd)
	registerWorkCancelCommand(workCmd)
	root.AddCommand(workCmd)
}

// workClient resolves scope + auth and builds the cloud client — the same
// preamble as catalog push (flag > env > intent > cached link).
func workClient(ctx context.Context, backendURLFlag, orgFlag string) (*remotestate.Client, error) {
	backendURL, err := requireBackendURL(nil, backendURLFlag)
	if err != nil {
		return nil, err
	}
	repo, err := resolveRepoContext(backendURL)
	if err != nil {
		return nil, err
	}
	linkOrg, linkProject := "", ""
	if repo != nil {
		linkOrg, linkProject = repo.OrgID, repo.ProjectID
	}
	intentOrg, intentProject, _ := intentScope(loadIntentForCloudConfig())
	scope := resolveScope(orgFlag, "", intentOrg, intentProject, linkOrg, linkProject)
	if scope.OrgID == "" {
		return nil, fmt.Errorf("orun work: no workspace resolved; pass --workspace or link the repo (orun auth login)")
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
		return nil, fmt.Errorf("remote state auth: %w", err)
	}
	return remotestate.NewClientWithScope(backendURL, version, tokenSrc, scope), nil
}

func toWireContract(c *worklens.Contract) *remotestate.WorkContract {
	if c == nil {
		return nil
	}
	return &remotestate.WorkContract{
		Goal:         c.Goal,
		Affects:      c.Affects,
		DoneWhen:     c.DoneWhen,
		Gates:        c.Gates,
		DesignRefs:   c.DesignRefs,
		Deps:         c.Deps,
		GatesDefined: c.GatesDefined,
	}
}

func toWirePlan(plan *worklens.ImportPlan, prefix string) remotestate.WorkImportRequest {
	req := remotestate.WorkImportRequest{
		Workspace: plan.Workspace,
		Root:      plan.Root,
		Prefix:    prefix,
	}
	for _, i := range plan.Initiatives {
		req.Initiatives = append(req.Initiatives, remotestate.WorkImportInitiative{Slug: i.Slug, Title: i.Title})
	}
	for _, s := range plan.Specs {
		req.Specs = append(req.Specs, remotestate.WorkImportSpec{
			Slug: s.Slug, Title: s.Title, DocPath: s.DocPath, DocSHA256: s.DocSHA256, PlanPath: s.PlanPath,
			Initiative: s.Initiative,
		})
	}
	for _, m := range plan.Milestones {
		req.Milestones = append(req.Milestones, remotestate.WorkImportMilestone{
			SpecSlug: m.SpecSlug, Key: m.Key, Title: m.Title, Goal: m.Goal, DoneWhen: m.DoneWhen, Ordinal: m.Ordinal,
		})
	}
	for _, t := range plan.Tasks {
		req.Tasks = append(req.Tasks, remotestate.WorkImportTask{
			SpecSlug: t.SpecSlug, MilestoneID: t.MilestoneID, Milestone: t.Milestone, Title: t.Title, Contract: toWireContract(t.Contract),
		})
	}
	return req
}

func registerWorkImportCommand(parent *cobra.Command) {
	var (
		workspace  string
		backendURL string
		prefix     string
		dryRun     bool
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "import <specs-dir>",
		Short: "Map a specs tree to Spec/Task envelopes (epic READMEs → Specs, milestones → Tasks)",
		Long: `Parse a repo's specs tree into the work plane's import plan: each epic
folder's README.md becomes a Spec (doc body content-addressed verbatim) and
each implementation-plan.md milestone becomes a Task whose contract is the
milestone's Goal / Deps / Done when fields.

Lifecycle is never imported: IMPLEMENTATION-STATUS.md tables stay behind, and
rungs derive from real observations after apply — the point of the exercise.

--dry-run prints the deterministic mapping; without it the plan applies to
Orun Cloud through the work mutators (every event lands as via=import;
re-imports skip existing specs and milestones).`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			plan, err := worklens.ParseSpecTree(args[0], workspace)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
			if dryRun {
				if asJSON {
					enc := json.NewEncoder(out)
					enc.SetIndent("", "  ")
					return enc.Encode(plan)
				}
				fmt.Fprintf(out, "workspace:   %s\n", plan.Workspace)
				if len(plan.Initiatives) > 0 {
					fmt.Fprintf(out, "initiatives: %d\n", len(plan.Initiatives))
				}
				fmt.Fprintf(out, "specs:       %d\n", len(plan.Specs))
				fmt.Fprintf(out, "milestones:  %d\n", len(plan.Milestones))
				fmt.Fprintf(out, "tasks:       %d\n\n", len(plan.Tasks))
				for _, s := range plan.Specs {
					n := 0
					for _, t := range plan.Tasks {
						if t.SpecSlug == s.Slug {
							n++
						}
					}
					fmt.Fprintf(out, "  %-32s %2d tasks  %s\n", s.Slug, n, s.DocSHA256[:19])
				}
				fmt.Fprintln(out, "\n(dry run — nothing written)")
				return nil
			}

			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			resp, err := client.ImportWork(cmd.Context(), toWirePlan(plan, prefix))
			if err != nil {
				return fmt.Errorf("orun work import: %w", err)
			}
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}
			if resp.InitiativesCreated+resp.InitiativesSkipped > 0 {
				fmt.Fprintf(out, "initiatives: %d created, %d skipped\n", resp.InitiativesCreated, resp.InitiativesSkipped)
			}
			fmt.Fprintf(out, "specs:       %d created, %d skipped\n", resp.SpecsCreated, resp.SpecsSkipped)
			fmt.Fprintf(out, "milestones:  %d created, %d skipped\n", resp.MilestonesCreated, resp.MilestonesSkipped)
			fmt.Fprintf(out, "tasks:       %d created, %d skipped, %d migrated into milestones\n", resp.TasksCreated, resp.TasksSkipped, resp.TasksMigrated)
			fmt.Fprintln(out, "\nlifecycle derives from observations — check `orun work list`")
			return nil
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "target workspace (org id or slug; defaults to the linked repo's)")
	cmd.Flags().StringVar(&backendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	cmd.Flags().StringVar(&prefix, "prefix", "WRK", "task-key prefix for imported milestones (2–5 uppercase)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the mapping without writing")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	parent.AddCommand(cmd)
}

func registerWorkEditCommand(parent *cobra.Command) {
	var (
		workspace   string
		backendURL  string
		asJSON      bool
		title       string
		description string
		owner       string
		target      string
		initiative  string
		criteria    []string
	)
	cmd := &cobra.Command{
		Use:   "edit <key>",
		Short: "Edit an item's envelope (title/description/owner/target/…)",
		Long: `Edit an item's envelope through the one mutator (item_edited): title,
plus the fields the item's kind exposes — description, owner, target date, and
success criteria for an initiative; target date and initiative filing for an
epic; title for a task.

This is intent only. Nothing here can move a rung — lifecycle stays derived
from observations (WP-3). Only the flags you pass are sent; pass an empty
value (e.g. --owner "") to clear a field.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			f := cmd.Flags()
			req := remotestate.EditWorkItemRequest{}
			if f.Changed("title") {
				req.Title = &title
			}
			if f.Changed("description") {
				req.Description = &description
			}
			if f.Changed("owner") {
				req.Owner = &owner
			}
			if f.Changed("target") {
				req.TargetDate = &target
			}
			if f.Changed("initiative") {
				req.Initiative = &initiative
			}
			if f.Changed("criteria") {
				req.SuccessCriteria = criteria
			}
			if req.Title == nil && req.Description == nil && req.Owner == nil &&
				req.TargetDate == nil && req.Initiative == nil && req.SuccessCriteria == nil {
				return fmt.Errorf("orun work edit: nothing to change; pass at least one of --title/--description/--owner/--target/--initiative/--criteria")
			}

			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			resp, err := client.EditWorkItem(cmd.Context(), args[0], req)
			if err != nil {
				return fmt.Errorf("orun work edit: %w", err)
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}
			fmt.Fprintf(out, "edited %s (seq %d)\n", resp.Key, resp.Seq)
			return nil
		},
	}
	cmd.Flags().StringVar(&title, "title", "", "new title")
	cmd.Flags().StringVar(&description, "description", "", "new description (initiatives)")
	cmd.Flags().StringVar(&owner, "owner", "", "owner subject; \"\" clears (initiatives)")
	cmd.Flags().StringVar(&target, "target", "", "target date YYYY-MM-DD; \"\" clears")
	cmd.Flags().StringVar(&initiative, "initiative", "", "file an epic under this initiative key; \"\" unfiles")
	cmd.Flags().StringArrayVar(&criteria, "criteria", nil, "success criterion (repeatable; initiatives)")
	cmd.Flags().StringVar(&workspace, "workspace", "", "target workspace (org id or slug; defaults to the linked repo's)")
	cmd.Flags().StringVar(&backendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	parent.AddCommand(cmd)
}

func registerWorkCancelCommand(parent *cobra.Command) {
	var (
		workspace  string
		backendURL string
		asJSON     bool
		yes        bool
	)
	cmd := &cobra.Command{
		Use:   "cancel <key>",
		Short: "Retire an item (task or epic) — the append-only \"delete\"",
		Long: `Retire a task or epic by folding a terminal 'canceled' state onto it.
Cancel is the model's native "delete": a terminal, attributed, append-only
state — the record and its whole history stay, and agents stop picking up its
work. It is effectively permanent (there is no un-cancel).

Initiatives have no lifecycle to cancel — edit their envelope, or retire their
epics, instead.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			key := args[0]
			if !yes && termIsInteractive() {
				fmt.Fprintf(cmd.OutOrStdout(), "Retire %s? This is terminal and cannot be un-canceled. Re-run with --yes to confirm.\n", key)
				return fmt.Errorf("orun work cancel: confirmation required (pass --yes)")
			}
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			resp, err := client.CancelWorkItem(cmd.Context(), key)
			if err != nil {
				return fmt.Errorf("orun work cancel: %w", err)
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(resp)
			}
			fmt.Fprintf(out, "retired %s — canceled (seq %d)\n", resp.Key, resp.Seq)
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the confirmation prompt")
	cmd.Flags().StringVar(&workspace, "workspace", "", "target workspace (org id or slug; defaults to the linked repo's)")
	cmd.Flags().StringVar(&backendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	parent.AddCommand(cmd)
}

func registerWorkListCommand(parent *cobra.Command) {
	var (
		workspace  string
		backendURL string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "Show the workspace's derived lifecycle (rungs with evidence)",
		Long: `Fetch the fold summary from Orun Cloud: every rung prints with the
evidence it derives from. Nothing shown here is a stored status.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			summary, err := client.GetWorkSummary(cmd.Context())
			if err != nil {
				return fmt.Errorf("orun work list: %w", err)
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(summary)
			}
			for _, s := range summary.Specs {
				fmt.Fprintf(out, "%s — %s  %v\n", s.Key, s.Title, s.Progress)
			}
			for _, t := range summary.Tasks {
				evidence := ""
				if len(t.Lifecycle.Evidence) > 0 {
					evidence = "  (" + t.Lifecycle.Evidence[0] + ")"
				}
				flags := ""
				if t.Lifecycle.Blocked {
					flags = " [blocked]"
				}
				fmt.Fprintf(out, "  %-10s %-12s %s%s%s\n", t.Key, t.Lifecycle.Rung, t.Title, flags, evidence)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "target workspace (org id or slug; defaults to the linked repo's)")
	cmd.Flags().StringVar(&backendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	parent.AddCommand(cmd)
}
