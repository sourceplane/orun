package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/mcpserve"
	"github.com/sourceplane/orun/internal/workmcp"
)

func registerMcpCommand(root *cobra.Command) {
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "The orun MCP: the agent surface over the work plane",
		Long: `The orun MCP (specs/orun-work v2, WP5) — the only agent surface.

Reads return the fold's output with its evidence; the write surface is four
tools (task_create, task_comment, task_assign, contract_propose) through the
same cloud mutators as the console keyboard. There is no lifecycle write
tool and no pin tool: an agent moves work forward by doing the work — its
branch, PR, and gates are observed like anyone else's.

Subcommands:
  serve   Serve MCP over stdio (newline-delimited JSON-RPC 2.0)`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	registerMcpServeCommand(mcpCmd)
	root.AddCommand(mcpCmd)
}

func registerMcpServeCommand(parent *cobra.Command) {
	var (
		workspace  string
		backendURL string
	)
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Serve the orun MCP over stdio",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := workClient(cmd.Context(), backendURL, workspace)
			if err != nil {
				return err
			}
			// One composed server (orun-mcp UM0): the work provider today;
			// the platform provider mounts beside it in UM1.
			server := &mcpserve.Server{
				Providers: []mcpserve.ToolProvider{&workmcp.Server{API: client, Workspace: client.Scope().OrgID}},
				Version:   version,
			}
			fmt.Fprintln(cmd.ErrOrStderr(), "orun MCP serving on stdio (workspace "+client.Scope().OrgID+")")
			return server.Serve(cmd.Context(), cmd.InOrStdin(), cmd.OutOrStdout())
		},
	}
	cmd.Flags().StringVar(&workspace, "workspace", "", "target workspace (org id or slug; defaults to the linked repo's)")
	cmd.Flags().StringVar(&backendURL, "backend-url", "", "Backend URL (Orun Cloud or self-hosted)")
	parent.AddCommand(cmd)
}
