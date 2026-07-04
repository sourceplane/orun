package main

import (
	"encoding/json"
	"fmt"

	"github.com/spf13/cobra"

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
  import    Map a specs/ tree to Spec/Task envelopes (dogfood path, WP0)

Run 'orun work <subcommand> --help' for details.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}

	registerWorkImportCommand(workCmd)
	root.AddCommand(workCmd)
}

func registerWorkImportCommand(parent *cobra.Command) {
	var (
		workspace string
		dryRun    bool
		asJSON    bool
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

WP0 ships --dry-run (the deterministic mapping); apply lands with the Orun
Cloud work API.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !dryRun {
				return fmt.Errorf("orun work import: only --dry-run is available until the cloud apply path lands (WP1); re-run with --dry-run")
			}
			plan, err := worklens.ParseSpecTree(args[0], workspace)
			if err != nil {
				return err
			}
			out := cmd.OutOrStdout()
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
			fmt.Fprintln(out, "\n(dry run — nothing written; apply lands with the cloud work API)")
			return nil
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "target workspace (ws_… id or slug)")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the mapping without writing")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the full import plan as JSON")
	_ = cmd.MarkFlagRequired("workspace")
	parent.AddCommand(cmd)
}
