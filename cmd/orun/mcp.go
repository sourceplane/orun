package main

import (
	"encoding/json"
	"fmt"
	"text/tabwriter"

	"github.com/spf13/cobra"

	"github.com/sourceplane/orun/internal/mcpserve"
	"github.com/sourceplane/orun/internal/platformmcp"
	"github.com/sourceplane/orun/internal/workmcp"
)

// mcpRosterCounts derives every tool count in the mcp help text from the
// live rosters (UM4: zero hardcoded numbers — the field report caught the
// help text drifting from the actual surface).
type mcpRosterCounts struct {
	work, workReads, workWrites             int
	platform, platformReads, platformWrites int
	server                                  int // built-in tools (UM5: connection_info)
	resources, prompts                      int // platform-plane templates/prompts (UM6)
}

func countMcpRoster() mcpRosterCounts {
	var c mcpRosterCounts
	for _, t := range workmcp.Tools() {
		c.work++
		if ro, _ := t.Annotations["readOnlyHint"].(bool); ro {
			c.workReads++
		}
	}
	c.workWrites = c.work - c.workReads
	c.platform = len((&platformmcp.Provider{}).Tools())
	c.platformReads = len((&platformmcp.Provider{ReadOnly: true}).Tools())
	c.platformWrites = c.platform - c.platformReads
	c.server = len((&mcpserve.ConnectionInfoProvider{}).Tools())
	c.resources = len((&platformmcp.Provider{}).ResourceTemplates())
	c.prompts = len((&platformmcp.Provider{}).Prompts())
	return c
}

func registerMcpCommand(root *cobra.Command) {
	counts := countMcpRoster()
	mcpCmd := &cobra.Command{
		Use:   "mcp",
		Short: "The orun MCP: one agent surface over the work and platform planes",
		Long: fmt.Sprintf(`The orun MCP (specs/orun-mcp) — the ecosystem's one local MCP.

One stdio JSON-RPC loop composes two tool planes:
  work      %d tools over the work plane (specs/orun-work WP5): %d reads +
            %d mutator-only writes, derived lifecycle, sealed briefs.
            Mounted when a workspace scope resolves.
  platform  %d tools over the Orun Cloud public API — %d reads (catalog,
            runs and logs, audit, events, access, usage, billing, config,
            webhooks) + %d writes (project/environment create, flag set,
            webhook create/replay, member invite; per-attempt
            Idempotency-Key). Mounted whenever cloud auth resolves; tool
            schemas are pinned to the vendored TS-plane manifest.
            --read-only drops the %d platform writes.
            Also serves %d resource templates (catalog entity overview, run
            summary) and %d prompts — capabilities advertise resources and
            prompts only when this plane mounts (UM6).

plus %d built-in tool (connection_info), mounted on EVERY serve — even with
no credentials at all: initialize always answers, and the tool reports auth
state, backend URL, which planes mounted and why, and the exact fix (UM5).

Subcommands:
  serve    Serve MCP over stdio (newline-delimited JSON-RPC 2.0)
  tools    Print the merged tool roster
  doctor   Diagnose the serve preconditions (binary, auth, workspace, backend)`,
			counts.work, counts.workReads, counts.workWrites,
			counts.platform, counts.platformReads, counts.platformWrites,
			counts.platformWrites, counts.resources, counts.prompts, counts.server),
		RunE: func(cmd *cobra.Command, args []string) error {
			return cmd.Help()
		},
	}
	registerMcpServeCommand(mcpCmd, counts)
	registerMcpToolsCommand(mcpCmd)
	registerMcpDoctorCommand(mcpCmd)
	root.AddCommand(mcpCmd)
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
				// The read-only column is the wire annotation itself (UM4) —
				// the display can never disagree with what a client sees.
				ro, _ := t.Annotations["readOnlyHint"].(bool)
				rows = append(rows, row{t.Name, "work", ro, t.Description})
			}
			for _, t := range (&platformmcp.Provider{ReadOnly: readOnly}).Tools() {
				ro, _ := t.Annotations["readOnlyHint"].(bool)
				rows = append(rows, row{t.Name, "platform", ro, t.Description})
			}
			// The built-in provider (UM5): present on every serve, listed
			// last to match the composed roster order.
			for _, t := range (&mcpserve.ConnectionInfoProvider{}).Tools() {
				ro, _ := t.Annotations["readOnlyHint"].(bool)
				rows = append(rows, row{t.Name, "server", ro, t.Description})
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
