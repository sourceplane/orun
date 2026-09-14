package main

import (
	"context"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/remotestate"
)

// `orun baseline` — the registry, from the command line
// (orun-bootstrap-engine BE-O7).
//
// Before this, the binary had no baseline verbs at all, so the "one command"
// story broke at step one: an operator had to open a console to learn an id and
// a tag before the CLI could do anything with either. Every read here maps 1:1
// onto a route the platform already serves.
func registerBaselineCommand(root *cobra.Command) {
	cmd := &cobra.Command{
		Use:   "baseline",
		Short: "The baseline registry: what can be built, and whether this workspace can build it",
		RunE:  func(c *cobra.Command, _ []string) error { return c.Help() },
	}
	cmd.AddCommand(newBaselineListCommand())
	cmd.AddCommand(newBaselineShowCommand())
	cmd.AddCommand(newBaselineCheckCommand())
	root.AddCommand(cmd)
}

func newBaselineListCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		publicOnly bool
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List the baselines this workspace's account may build",
		Long: `List baselines. By default this reads the workspace-scoped catalogue —
the public set PLUS anything the account registered itself — because a
signed-in reader should see their own private baseline at the one moment it
matters. --public reads the catalogue a stranger sees.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			ctx := cmd.Context()
			var (
				rows []remotestate.Baseline
				err  error
			)
			if publicOnly {
				client, cerr := anonymousBaselineClient(backendURL)
				if cerr != nil {
					return cerr
				}
				rows, err = client.ListPublicBaselines(ctx)
			} else {
				client, cerr := cloudClient(ctx, backendURL, workspace)
				if cerr != nil {
					return cerr
				}
				rows, err = client.ListBaselines(ctx, client.Scope().OrgID)
			}
			if err != nil {
				return fmt.Errorf("orun baseline list: %w", err)
			}
			if asJSON {
				return encodeJSON(cmd, rows)
			}
			if len(rows) == 0 {
				fmt.Fprintln(cmd.OutOrStdout(), "no baselines")
				return nil
			}
			w := tabwriter.NewWriter(cmd.OutOrStdout(), 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTAG\tTIER\tMINS\tSTACK\tSUMMARY")
			for _, b := range rows {
				if b.Retired() {
					continue
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\t%s\n",
					b.ID, b.Tag, b.Tier, b.ExpectedMinutes,
					strings.Join(b.Stack, ","), firstLine(b.Summary))
			}
			return w.Flush()
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "workspace id (ws_…/org_…) or slug")
	cmd.Flags().StringVar(&backendURL, "backend-url", "", "platform base URL")
	cmd.Flags().BoolVar(&publicOnly, "public", false, "read the public catalogue instead of this account's")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

func newBaselineShowCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
		asJSON     bool
	)
	cmd := &cobra.Command{
		Use:   "show <id[@tag]>",
		Short: "Show one baseline and whether this workspace can build it",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			view, err := resolveBlueprint(cmd.Context(), backendURL, workspace, args[0])
			if err != nil {
				return err
			}
			if asJSON {
				return encodeJSON(cmd, view)
			}
			b := view.Blueprint
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "%s — %s\n", b.ID, b.Name)
			if b.Summary != "" {
				fmt.Fprintf(out, "\n%s\n", strings.TrimSpace(b.Summary))
			}
			fmt.Fprintf(out, "\nsource   %s@%s\n", b.SourceRepo, b.Tag)
			if b.ExpectedMinutes > 0 {
				fmt.Fprintf(out, "budget   about %d minutes\n", b.ExpectedMinutes)
			}
			// A stale pin RESOLVES and says so: the visitor clicking a link
			// from a blog post wants to build the platform, not to litigate a
			// version.
			if view.PinnedTagStale {
				fmt.Fprintf(out, "\nyou asked for %s; the registry now publishes %s, and that is what a build would use\n",
					view.PinnedTag, b.Tag)
			}
			fmt.Fprintln(out, "\nproviders")
			for _, p := range b.Readiness.Integrations {
				mark := "✕"
				if p.Connected {
					mark = "✓"
				}
				fmt.Fprintf(out, "  %s %s\n", mark, p.Provider)
			}
			if b.Readiness.IntegrationsReady {
				fmt.Fprintln(out, "\nthis workspace can build it")
			} else {
				fmt.Fprintln(out, "\nconnect the providers above before building")
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "workspace id (ws_…/org_…) or slug")
	cmd.Flags().StringVar(&backendURL, "backend-url", "", "platform base URL")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	return cmd
}

func newBaselineCheckCommand() *cobra.Command {
	var (
		workspace  string
		backendURL string
	)
	cmd := &cobra.Command{
		Use:   "check <id[@tag]>",
		Short: "Exit non-zero unless this workspace can build the baseline",
		Long: `Readiness as an exit code, for a script or a CI gate: zero when every
provider the baseline needs is connected, non-zero when one is not, naming
which. Nothing is written and no build is started.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			view, err := resolveBlueprint(cmd.Context(), backendURL, workspace, args[0])
			if err != nil {
				return err
			}
			if view.Blueprint.Readiness.IntegrationsReady {
				fmt.Fprintf(cmd.OutOrStdout(), "%s is ready to build\n", view.Blueprint.ID)
				return nil
			}
			var missing []string
			for _, p := range view.Blueprint.Readiness.Integrations {
				if !p.Connected {
					missing = append(missing, p.Provider)
				}
			}
			return exitErr(1, "%s needs %s connected in this workspace",
				view.Blueprint.ID, strings.Join(missing, ", "))
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "workspace id (ws_…/org_…) or slug")
	cmd.Flags().StringVar(&backendURL, "backend-url", "", "platform base URL")
	return cmd
}

// resolveBlueprint reads one baseline for the caller's workspace.
func resolveBlueprint(ctx context.Context, backendURL, workspace, id string) (*remotestate.BlueprintView, error) {
	client, err := cloudClient(ctx, backendURL, workspace)
	if err != nil {
		return nil, err
	}
	view, err := client.GetBlueprint(ctx, client.Scope().OrgID, id)
	if err != nil {
		return nil, fmt.Errorf("orun baseline: %w", err)
	}
	return view, nil
}

// anonymousBaselineClient reads the public catalogue, which needs no session —
// that is what "public" means, and requiring a login to read it would make the
// signed-out catalogue a lie.
func anonymousBaselineClient(backendURLFlag string) (*remotestate.Client, error) {
	backendURL, err := requireBackendURL(nil, backendURLFlag)
	if err != nil {
		return nil, err
	}
	return remotestate.NewClient(backendURL, version, nil), nil
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		s = s[:i]
	}
	if len(s) > 72 {
		s = s[:69] + "…"
	}
	return s
}
