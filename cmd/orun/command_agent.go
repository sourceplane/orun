package main

import (
	"fmt"

	"github.com/sourceplane/orun/internal/agent"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/spf13/cobra"
)

// command_agent.go — the `orun agent` verb group (specs/orun-agents/). AG0
// ships `context` (print/seal the base literacy); `run`, `serve`, `pull`,
// `import`, and `replay` land with AG1–AG4.

var agentContextSeal bool

var agentCmd = &cobra.Command{
	Use:   "agent",
	Short: "The agent runtime",
	Long: "Delegate work to a coding agent (Claude Code first, any driver behind the seam).\n" +
		"Agent types are content-addressed objects sealed from agents/*.md; runs are\n" +
		"sealed and replayable. See specs/orun-agents/.",
}

var agentContextCmd = &cobra.Command{
	Use:   "context",
	Short: "Print the base orun literacy every agent type extends",
	Long: "Print the versioned base-literacy document shipped with this binary — the layer\n" +
		"of orun understanding every agent type extends instead of restating. With --seal,\n" +
		"also store it as a content-addressed blob and pin refs/agents/literacy/" + agent.LiteracyVersion + ".",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !agentContextSeal {
			fmt.Print(string(agent.Literacy()))
			return nil
		}
		store, refs, _, ok := openObjectStores()
		if !ok {
			return fmt.Errorf("no object store at .orun — run `orun plan` first (or drop --seal to just print)")
		}
		id, err := agent.SealLiteracy(cmd.Context(), store, refs)
		if err != nil {
			return err
		}
		fmt.Printf("%s %s@%s\n", id, agent.LiteracyName, agent.LiteracyVersion)
		return nil
	},
}

var agentContextIDCmd = &cobra.Command{
	Use:   "id",
	Short: "Print the literacy's content id without writing",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		id, err := agent.LiteracyID(objectstore.DefaultAlgo)
		if err != nil {
			return err
		}
		fmt.Printf("%s %s@%s\n", id, agent.LiteracyName, agent.LiteracyVersion)
		return nil
	},
}

func registerAgentCommand(root *cobra.Command) {
	agentContextCmd.Flags().BoolVar(&agentContextSeal, "seal", false, "store the literacy blob and pin its ref")
	agentContextCmd.AddCommand(agentContextIDCmd)
	agentCmd.AddCommand(agentContextCmd)
	root.AddCommand(agentCmd)
}
