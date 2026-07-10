package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sourceplane/orun/internal/agent"
	"github.com/sourceplane/orun/internal/agent/attach"
	"github.com/sourceplane/orun/internal/agent/driver"
	"github.com/sourceplane/orun/internal/agenttype"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/workmcp"
	"github.com/spf13/cobra"
)

// command_agent_serve.go — the in-sandbox entrypoint (orun-agents-live AL4):
// the AG2 loop with its attach plane pointed at the cloud relay's dial-out
// binding. This is what a Daytona box runs, retiring the cloud's bash
// bootstrap stand-in (saas-agents-live AL8 deletes it). Everything with agent
// semantics is here; the cloud provides the box, the identity, and the relay.

var (
	serveSessionID string
	serveType      string
	serveTask      string
	serveDriver    string
)

var agentServeCmd = &cobra.Command{
	Use:   "serve",
	Short: "Run a session body and stream it to the cloud relay (in-sandbox entrypoint)",
	Long: `Run the delegation loop with its attach plane pointed at the per-session
cloud relay (attach-protocol.md §6.3): event batches dial out to the relay,
the steer/verdict/interrupt return-queue dials back. The console and a remote
'orun agent attach as_…' are interchangeable heads over the same stream.

Identity comes from the sandbox environment (injected by the control plane):
  ORUN_CLOUD_API    the api-edge base URL
  ORUN_ORG_ID       the workspace (org_…) id
  ORUN_SESSION_ID   the as_… session id (overridable with --session)
  ORUN_SESSION_TOKEN the session bearer (the service-principal credential)`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		sessionID := serveSessionID
		if sessionID == "" {
			sessionID = os.Getenv("ORUN_SESSION_ID")
		}
		if sessionID == "" {
			return fmt.Errorf("no session id (set --session or ORUN_SESSION_ID)")
		}
		cloudAPI := os.Getenv("ORUN_CLOUD_API")
		orgID := os.Getenv("ORUN_ORG_ID")
		token := os.Getenv("ORUN_SESSION_TOKEN")
		if cloudAPI == "" || orgID == "" || token == "" {
			return fmt.Errorf("missing sandbox env: ORUN_CLOUD_API, ORUN_ORG_ID, ORUN_SESSION_TOKEN are required")
		}
		relayBase := fmt.Sprintf("%s/v1/organizations/%s/agents/sessions/%s", cloudAPI, orgID, sessionID)

		store, refs, _, ok := openObjectStores()
		if !ok {
			return fmt.Errorf("no object store at .orun — pull the brief first")
		}
		ctx := cmd.Context()

		// Resolve the agent type + contract the same way `agent run` does; the
		// brief is content-addressed so this run matches its local twin by hash.
		var persona []byte
		var toolPolicy nodes.AgentToolPolicy
		typeName := serveType
		if typeName == "" {
			typeName = os.Getenv("ORUN_AGENT_TYPE")
		}
		if typeName != "" {
			d, issues := agenttype.Load(filepath.Join("agents", typeName+".md"))
			if d == nil {
				return fmt.Errorf("agent type %q: %v", typeName, issues)
			}
			persona = d.Body
			toolPolicy = d.Tools
		}
		task := serveTask
		if task == "" {
			task = os.Getenv("ORUN_TASK_KEY")
		}
		runKind := nodes.RunKindImplementation
		if task == "" {
			runKind = nodes.RunKindInteractive
		}
		brief, err := agent.AssembleBrief(ctx, store, agent.BriefInput{
			RunKind: runKind, Task: task, Persona: persona,
		})
		if err != nil {
			return err
		}

		drv, err := driver.Get(serveDriver)
		if err != nil {
			return err
		}
		var mcpConfigPath string
		if serveDriver == driver.ClaudeCodeID {
			setup, mErr := agent.WriteMCPConfig(filepath.Join(".orun", "agent-mcp"),
				agent.NewToolPolicy(toolPolicy), workmcp.ToolNames(), nil)
			if mErr != nil {
				return mErr
			}
			mcpConfigPath = setup.ConfigPath
			drv = &driver.ClaudeCode{ExtraArgs: setup.HarnessArgs()}
		}
		if serveDriver == "stub" && runKind == nodes.RunKindInteractive {
			drv = &driver.Stub{Interactive: true}
		}

		branch := ""
		if task != "" {
			branch = "agent/" + task + "-" + slugify(typeName)
		}

		// Host the attach plane and bridge it to the relay in the background;
		// the loop runs in the foreground and seals on terminal state.
		inputs := agent.NewInputQueue()
		srv := attach.NewServer(attach.SessionInfo{
			SessionID: sessionID, BriefID: brief.ID, AgentType: typeName,
			Task: task, RunKind: string(runKind), Harness: serveDriver,
		}, inputs)
		relayCtx, relayCancel := context.WithCancel(ctx)
		defer relayCancel()
		go func() {
			_ = attach.ServeToRelay(relayCtx, srv, inputs, attach.RelayConfig{
				BaseURL: relayBase, Token: token,
			})
		}()

		opts := agent.RunOptions{
			SessionID:     sessionID,
			Driver:        drv,
			Brief:         brief,
			Branch:        branch,
			Policy:        agent.NewToolPolicy(toolPolicy),
			MCPConfigPath: mcpConfigPath,
			Inputs:        inputs,
			Observe:       srv.Observe,
			ObserveDelta:  srv.ObserveDelta,
		}
		if typeName != "" {
			if ref, rerr := refs.Read(ctx, agentTypeRef(typeName)); rerr == nil && ref.Target != "" {
				opts.Refs = refs
				opts.Seal = &agent.SealInput{RunKind: runKind, AgentType: ref.Target, Brief: brief.ID, Principal: "sp_session"}
			}
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "orun agent serve: session %s → relay %s\n", sessionID, relayBase)
		res, err := agent.Run(ctx, store, opts)
		srv.Close("terminal")
		relayCancel()
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "session %s ended: %s\n", res.SessionID, res.Outcome.Status)
		return nil
	},
}

func registerAgentServeCommand(parent *cobra.Command) {
	agentServeCmd.Flags().StringVar(&serveSessionID, "session", "", "session id (defaults to ORUN_SESSION_ID)")
	agentServeCmd.Flags().StringVar(&serveType, "type", "", "agent type (defaults to ORUN_AGENT_TYPE)")
	agentServeCmd.Flags().StringVar(&serveTask, "task", "", "task key (defaults to ORUN_TASK_KEY)")
	agentServeCmd.Flags().StringVar(&serveDriver, "driver", "claude-code", "driver id")
	parent.AddCommand(agentServeCmd)
}
