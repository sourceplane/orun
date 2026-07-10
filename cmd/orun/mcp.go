package main

import (
	"context"
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/mcpserve"
	"github.com/sourceplane/orun/internal/platformmcp"
	"github.com/sourceplane/orun/internal/remotestate"
	"github.com/sourceplane/orun/internal/workmcp"
)

func registerMcpCommand(root *cobra.Command) {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "The orun MCP: one agent surface over the work and platform planes",
		Long: `The orun MCP (specs/orun-mcp) — the ecosystem's one local MCP.

One stdio JSON-RPC loop composes two tool planes:
  work      9 tools over the work plane (specs/orun-work WP5): mutator-only
            writes, derived lifecycle, sealed briefs. Mounted when a
            workspace scope resolves.
  platform  25 tools over the Orun Cloud public API — 19 reads (catalog,
            runs and logs, audit, events, access, usage, billing, config,
            webhooks) + 6 writes (project/environment create, flag set,
            webhook create/replay, member invite; per-attempt
            Idempotency-Key). Mounted whenever cloud auth resolves; tool
            schemas are pinned to the vendored TS-plane manifest.
            --read-only drops the 6 platform writes.

Subcommands:
  serve   Serve MCP over stdio (newline-delimited JSON-RPC 2.0)
  tools   Print the merged tool roster`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	registerMcpServeCommand(mcpCmd)
	registerMcpToolsCommand(mcpCmd)
	root.AddCommand(mcpCmd)
}

// mcpCloudClient is the serve-time auth/scope preamble: workClient's
// resolution chain, but the workspace is optional — the platform plane
// mounts on auth alone (design §1), so only auth failures are fatal here.
func mcpCloudClient(ctx context.Context, backendURLFlag, orgFlag string) (*remotestate.Client, error) {
	intent := loadIntentForCloudConfig()
	backendURL, err := requireBackendURL(intent, backendURLFlag)
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
	intentOrg, intentProject, _ := intentScope(intent)
	scope := resolveScope(orgFlag, "", intentOrg, intentProject, linkOrg, linkProject)
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

func registerMcpServeCommand(parent *cobra.Command) {
	var (
		workspace  string
		backendURL string
		readOnly   bool
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the orun MCP over stdio",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := mcpCloudClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			// Contextual mounting (orun-mcp UM1): platform tools whenever auth
			// resolves; work tools only with a workspace scope. Stdout is
			// protocol-pure; diagnostics go to stderr.
			ws := client.Scope().OrgID
			var providers []mcpserve.ToolProvider
			if ws != "" {
				providers = append(providers, &workmcp.Server{API: client, Workspace: ws})
			}
			providers = append(providers, &platformmcp.Provider{API: client, DefaultWorkspace: ws, ReadOnly: readOnly})
			server := &mcpserve.Server{Providers: providers, Version: version}
			if ws != "" {
				fmt.Fprintln(cmd.ErrOrStderr(), "orun MCP serving on stdio (workspace "+ws+"; work + platform tools)")
			} else {
				fmt.Fprintln(cmd.ErrOrStderr(), "orun MCP serving on stdio (no workspace resolved: platform tools only — pass --workspace or link the repo to mount work tools)")
			}
			return server.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "target workspace (org id or slug; defaults to the linked repo's)")
	cmd.Flags().StringVar(&backendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "serve only the platform plane's read tools (drops the 6 platform writes; work tools are mutator-shaped by design — WP-6 — and unaffected)")
	parent.AddCommand(cmd)
}

func registerMcpToolsCommand(parent *cobra.Command) {
	var (
		asJSON   bool
		readOnly bool
	)
	cmd := &cobra.Command{
		Use:   "tools",
		Short: "Print the merged tool roster (provider and read-only columns)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			type row struct {
				Name        string `json:"name"`
				Provider    string `json:"provider"`
				ReadOnly    bool   `json:"readOnly"`
				Description string `json:"description"`
			}
			var rows []row
			for _, t := range (&workmcp.Server{}).Tools() {
				rows = append(rows, row{t.Name, "work", workmcp.ReadOnly(t.Name), t.Description})
			}
			for _, t := range (&platformmcp.Provider{ReadOnly: readOnly}).Tools() {
				ro, _ := t.Annotations["readOnlyHint"].(bool)
				rows = append(rows, row{t.Name, "platform", ro, t.Description})
			}
			out := cmd.OutOrStdout()
			if asJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(rows)
			}
			w := tabwriter.NewWriter(out, 2, 4, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tPROVIDER\tREAD-ONLY\tDESCRIPTION")
			for _, r := range rows {
				ro := "write"
				if r.ReadOnly {
					ro = "read-only"
				}
				desc := r.Description
				if runes := []rune(desc); len(runes) > 88 {
					desc = string(runes[:85]) + "..."
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", r.Name, r.Provider, ro, desc)
			}
			return w.Flush()
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit JSON")
	cmd.Flags().BoolVar(&readOnly, "read-only", false, "list the roster as `serve --read-only` advertises it (platform writes dropped; work tools unaffected)")
	parent.AddCommand(cmd)
}
