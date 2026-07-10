package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sourceplane/orun/internal/agent"
	"github.com/sourceplane/orun/internal/agenttype"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
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
		"sealed and replayable. Run bare to open the interactive Agent surface (the TUI\n" +
		"head). See specs/orun-agents/ and specs/orun-agents-live/.",
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		// Bare `orun agent` is the front door: the cockpit on the Agent
		// surface (orun-agents-live AL3).
		return runAgentTUI(cmd.Context())
	},
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

// agentTypeRef is the ref that tracks an agent type's newest sealed version.
func agentTypeRef(name string) string { return "agents/types/" + name + "/latest" }

var agentLintCmd = &cobra.Command{
	Use:   "lint [dir]",
	Short: "Validate agents/*.md agent-type files without writing",
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "agents"
		if len(args) == 1 {
			dir = args[0]
		}
		decls, issues := agenttype.LoadDir(dir)
		out := cmd.OutOrStdout()
		errs := 0
		for _, is := range issues {
			if is.Level == "error" {
				errs++
			}
			fmt.Fprintln(out, is.String())
		}
		for _, d := range decls {
			fmt.Fprintf(out, "%s: ok: agent-type %q (harness %s, model %s, owner %s)\n",
				d.Path, d.Name, d.Harness, d.Model, d.Owner)
		}
		if errs > 0 {
			return fmt.Errorf("%d agent-type error(s)", errs)
		}
		return nil
	},
}

var agentImportIDOnly bool

var agentImportCmd = &cobra.Command{
	Use:   "import [dir]",
	Short: "Seal agents/*.md into the object store as AgentTypeSnapshots",
	Long: `Parse every agent-type file (frontmatter capability envelope + persona body),
seal each into a content-addressed AgentTypeSnapshot — persona verbatim as a
body blob, the binary's base literacy pinned via extends — and move
refs/agents/types/<name>/latest. Idempotent: an unchanged file re-seals to the
same id (mirrors 'orun work import' for the work tree).`,
	Args: cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dir := "agents"
		if len(args) == 1 {
			dir = args[0]
		}
		decls, issues := agenttype.LoadDir(dir)
		out := cmd.OutOrStdout()
		for _, is := range issues {
			if is.Level == "error" {
				fmt.Fprintln(out, is.String())
			}
		}
		if agenttypeHasError(issues) {
			return fmt.Errorf("agent import: fix lint errors first")
		}
		if len(decls) == 0 {
			fmt.Fprintf(out, "no agent types under %s\n", dir)
			return nil
		}
		store, refs, _, err := openObjectModel()
		if err != nil {
			return err
		}
		w := nodewriter.New(store, refs)
		for _, d := range decls {
			// Pin the binary's literacy unless the file pins a custom one
			// (name@id) explicitly.
			litName, litBody := agent.LiteracyName, agent.Literacy()
			if d.Extends != "" && strings.Contains(d.Extends, "@") {
				litName, litBody = "", nil
			}
			id, err := w.WriteAgentType(cmd.Context(), d.Snapshot(), d.Body, litName, litBody, agentTypeRef(d.Name))
			if err != nil {
				return fmt.Errorf("agent import %s: %w", d.Name, err)
			}
			if agentImportIDOnly {
				fmt.Fprintf(out, "%s %s\n", id, d.Name)
				continue
			}
			fmt.Fprintf(out, "sealed  %s  %s (refs/%s)\n", id, d.Name, agentTypeRef(d.Name))
		}
		return nil
	},
}

var agentShowCmd = &cobra.Command{
	Use:   "show <name>[@sha256:…]",
	Short: "Materialize a sealed agent type from the object store",
	Long: `Resolve an agent type by name (refs/agents/types/<name>/latest) or by an
explicit @<objectId> pin, and print the sealed envelope + persona from content
alone — the offline read-back of 'orun agent import'.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name, pin := args[0], ""
		if i := strings.Index(args[0], "@"); i >= 0 {
			name, pin = args[0][:i], args[0][i+1:]
		}
		store, refs, _, ok := openObjectStores()
		if !ok {
			return fmt.Errorf("no object store at .orun — run `orun agent import` first")
		}
		ctx := cmd.Context()
		target := pin
		if target == "" {
			ref, err := refs.Read(ctx, agentTypeRef(name))
			if err != nil {
				return fmt.Errorf("agent type %q not sealed (run `orun agent import`): %w", name, err)
			}
			target = ref.Target
		}
		entries, err := store.GetTree(ctx, objectstore.ObjectID(target))
		if err != nil {
			return fmt.Errorf("agent type %s: %w", target, err)
		}
		var rec nodes.AgentTypeSnapshot
		var persona []byte
		for _, e := range entries {
			switch e.Name {
			case "agent-type.json":
				_, b, err := store.Get(ctx, e.ID)
				if err != nil {
					return err
				}
				if rec, err = nodes.Decode[nodes.AgentTypeSnapshot](b); err != nil {
					return err
				}
			case "body.md":
				if _, persona, err = store.Get(ctx, e.ID); err != nil {
					return err
				}
			}
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "agent-type %s @ %s\n", rec.Name, target)
		fmt.Fprintf(out, "harness    %s · model %s\n", rec.Harness, rec.Model)
		fmt.Fprintf(out, "owner      %s\n", rec.Owner)
		if rec.AutonomyDefault != "" {
			fmt.Fprintf(out, "autonomy   %s\n", rec.AutonomyDefault)
		}
		if len(rec.MayAffect) > 0 {
			fmt.Fprintf(out, "mayAffect  %s\n", strings.Join(rec.MayAffect, " · "))
		}
		if rec.Extends != "" {
			fmt.Fprintf(out, "extends    %s\n", rec.Extends)
		}
		fmt.Fprintf(out, "tools      allow=%v ask=%v deny=%v\n", rec.Tools.Allow, rec.Tools.Ask, rec.Tools.Deny)
		fmt.Fprintf(out, "\n%s", persona)
		return nil
	},
}

func agenttypeHasError(issues []agenttype.Issue) bool {
	for _, i := range issues {
		if i.Level == "error" {
			return true
		}
	}
	return false
}

var agentReplayCmd = &cobra.Command{
	Use:   "replay <sessionId>[@sha256:…]",
	Short: "Re-render a sealed session's transcript from content alone",
	Long: "Resolve a session by id (refs/agents/sessions/<id>) or an explicit\n" +
		"@<objectId> pin, and replay its sealed segment chain — the run reconstructed\n" +
		"deterministically from the object graph, no live process.",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		sess, pin := args[0], ""
		if i := strings.Index(args[0], "@"); i >= 0 {
			sess, pin = args[0][:i], args[0][i+1:]
		}
		store, refs, _, ok := openObjectStores()
		if !ok {
			return fmt.Errorf("no object store at .orun")
		}
		ctx := cmd.Context()
		target := pin
		if target == "" {
			ref, err := refs.Read(ctx, agent.SessionRef(sess))
			if err != nil {
				return fmt.Errorf("session %q not sealed: %w", sess, err)
			}
			target = ref.Target
		}
		snap, lines, err := agent.Replay(ctx, store, objectstore.ObjectID(target))
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		fmt.Fprintf(out, "session   %s @ %s\n", snap.SessionID, target)
		fmt.Fprintf(out, "runKind   %s\n", snap.RunKind)
		fmt.Fprintf(out, "agentType %s\n", snap.AgentType)
		fmt.Fprintf(out, "brief     %s\n", snap.Brief)
		if snap.Outcome != nil {
			fmt.Fprintf(out, "outcome   %s", snap.Outcome.Status)
			if snap.Outcome.PR != "" {
				fmt.Fprintf(out, "  pr=%s", snap.Outcome.PR)
			}
			fmt.Fprintln(out)
		}
		fmt.Fprintf(out, "\ntranscript (%d events):\n", len(lines))
		for _, l := range lines {
			fmt.Fprintf(out, "  %3d  %-20s %s\n", l.Seq, l.Kind, compactPayload(l.Payload))
		}
		return nil
	},
}

func compactPayload(p map[string]any) string {
	if len(p) == 0 {
		return ""
	}
	b, err := json.Marshal(p)
	if err != nil {
		return ""
	}
	return string(b)
}

func registerAgentCommand(root *cobra.Command) {
	agentContextCmd.Flags().BoolVar(&agentContextSeal, "seal", false, "store the literacy blob and pin its ref")
	agentContextCmd.AddCommand(agentContextIDCmd)
	agentImportCmd.Flags().BoolVar(&agentImportIDOnly, "id-only", false, "print only '<id> <name>' lines (for scripting)")
	agentCmd.AddCommand(agentContextCmd)
	agentCmd.AddCommand(agentLintCmd)
	agentCmd.AddCommand(agentImportCmd)
	agentCmd.AddCommand(agentShowCmd)
	registerAgentRunCommand(agentCmd)
	registerAgentServeCommand(agentCmd)
	agentCmd.AddCommand(agentReplayCmd)
	root.AddCommand(agentCmd)
}
