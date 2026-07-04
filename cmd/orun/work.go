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

Run 'orun work <subcommand> --help' for details.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	registerWorkImportCommand(workCmd)
	registerWorkListCommand(workCmd)
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
	for _, s := range plan.Specs {
		req.Specs = append(req.Specs, remotestate.WorkImportSpec{
			Slug: s.Slug, Title: s.Title, DocPath: s.DocPath, DocSHA256: s.DocSHA256, PlanPath: s.PlanPath,
		})
	}
	for _, t := range plan.Tasks {
		req.Tasks = append(req.Tasks, remotestate.WorkImportTask{
			SpecSlug: t.SpecSlug, MilestoneID: t.MilestoneID, Title: t.Title, Contract: toWireContract(t.Contract),
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
				fmt.Fprintf(out, "workspace: %s\n", plan.Workspace)
				fmt.Fprintf(out, "specs:     %d\n", len(plan.Specs))
				fmt.Fprintf(out, "tasks:     %d\n\n", len(plan.Tasks))
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
			fmt.Fprintf(out, "specs:  %d created, %d skipped\n", resp.SpecsCreated, resp.SpecsSkipped)
			fmt.Fprintf(out, "tasks:  %d created, %d skipped\n", resp.TasksCreated, resp.TasksSkipped)
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
