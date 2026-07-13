package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

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
		errOut := cmd.ErrOrStderr()
		sessionID := serveSessionID
		if sessionID == "" {
			sessionID = os.Getenv("ORUN_SESSION_ID")
		}
		cloudAPI := os.Getenv("ORUN_CLOUD_API")
		orgID := os.Getenv("ORUN_ORG_ID")
		token := os.Getenv("ORUN_SESSION_TOKEN")

		// Dial-home identity is create-time sandbox env the control plane set on
		// the box. Log (redacted) which of the four actually reached this
		// process — this is the first line to read when a session never leaves
		// `provisioning`. The signature of the control-plane env-propagation
		// split is the identity trio empty while ORUN_SESSION_TOKEN — injected
		// separately as the toolbox exec's `export` prefix — is present.
		fmt.Fprintf(errOut, "orun agent serve: dial-home identity — ORUN_CLOUD_API=%s ORUN_ORG_ID=%s ORUN_SESSION_ID=%s ORUN_SESSION_TOKEN=%s\n",
			orMissing(cloudAPI), orMissing(orgID), orMissing(sessionID), redactSecret(token))
		if err := checkServeIdentity(cloudAPI, orgID, sessionID, token); err != nil {
			return err
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

		// Host the attach plane and bridge it to the relay. The dial-home is
		// synchronous and MUST land before the agent runs: the first heartbeat
		// creates the cloud session lease, and a local run whose stream never
		// reaches the cloud is worse than useless — it burns a box while the
		// control plane sees a silent `provisioning` session it reclaims 30 min
		// later as `lease_lost`. So fail here, loudly, with a diagnostic.
		inputs := agent.NewInputQueue()
		srv := attach.NewServer(attach.SessionInfo{
			SessionID: sessionID, BriefID: brief.ID, AgentType: typeName,
			Task: task, RunKind: string(runKind), Harness: serveDriver,
		}, inputs)
		relayCtx, relayCancel := context.WithCancel(ctx)
		defer relayCancel()
		fmt.Fprintf(errOut, "orun agent serve: dialing home — session %s → relay %s\n", sessionID, relayBase)
		relaySession, err := attach.DialToRelay(relayCtx, srv, inputs, attach.RelayConfig{
			BaseURL: relayBase, Token: token, Log: errOut,
		})
		if err != nil {
			return fmt.Errorf("cloud dial-home failed: %w", err)
		}
		defer relaySession.Close()
		// The event-stream dial-home reached the relay and the token was
		// accepted — the box can talk to the cloud with this identity. NOTE:
		// this is the /events attach channel, not the session /heartbeat
		// endpoint the control plane uses to flip provisioning→running (see the
		// PR discussion); it proves reachability + auth, which is what makes a
		// stuck-in-provisioning session diagnosable.
		fmt.Fprintf(errOut, "orun agent serve: dial-home ok — relay reachable and session token accepted\n")

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
		res, err := agent.Run(ctx, store, opts)
		srv.Close("terminal")
		relaySession.Close()
		relayCancel()
		if err != nil {
			return err
		}
		fmt.Fprintf(cmd.ErrOrStderr(), "session %s ended: %s\n", res.SessionID, res.Outcome.Status)
		return nil
	},
}

// orMissing renders a non-secret env value for the dial-home diagnostic, or a
// loud <MISSING> so an empty identity var is unmistakable in the logs.
func orMissing(v string) string {
	if v == "" {
		return "<MISSING>"
	}
	return v
}

// redactSecret renders ORUN_SESSION_TOKEN as present/absent + length only —
// enough to tell the env-propagation split (token present, identity empty) from
// a total env miss, without ever logging the credential.
func redactSecret(v string) string {
	if v == "" {
		return "<MISSING>"
	}
	return fmt.Sprintf("present(len=%d)", len(v))
}

// checkServeIdentity validates the four dial-home vars and, when the identity
// trio is empty but the token is present, names the control-plane
// env-propagation split explicitly so the failure routes itself: the box-create
// sandbox env is not reaching the serve process, and the fix belongs in
// orun-cloud's bootstrap, not here.
func checkServeIdentity(cloudAPI, orgID, sessionID, token string) error {
	var missing []string
	if cloudAPI == "" {
		missing = append(missing, "ORUN_CLOUD_API")
	}
	if orgID == "" {
		missing = append(missing, "ORUN_ORG_ID")
	}
	if sessionID == "" {
		missing = append(missing, "ORUN_SESSION_ID (or --session)")
	}
	if token == "" {
		missing = append(missing, "ORUN_SESSION_TOKEN")
	}
	if len(missing) == 0 {
		return nil
	}
	if token != "" && (cloudAPI == "" || orgID == "" || sessionID == "") {
		return fmt.Errorf("dial-home identity missing (%s) while ORUN_SESSION_TOKEN is present — this is the control-plane env-propagation split. "+
			"The control plane sets these as box-create sandbox env, but they are not reaching the `orun agent serve` process. "+
			"The token is injected separately as the toolbox exec's `export` prefix; the identity vars are only sandbox env and the toolbox session-exec is not carrying them in. "+
			"The fix belongs in orun-cloud (re-export the identity vars in the bootstrap exec prefix), not in orun",
			strings.Join(missing, ", "))
	}
	return fmt.Errorf("missing sandbox env required for cloud dial-home: %s", strings.Join(missing, ", "))
}

func registerAgentServeCommand(parent *cobra.Command) {
	agentServeCmd.Flags().StringVar(&serveSessionID, "session", "", "session id (defaults to ORUN_SESSION_ID)")
	agentServeCmd.Flags().StringVar(&serveType, "type", "", "agent type (defaults to ORUN_AGENT_TYPE)")
	agentServeCmd.Flags().StringVar(&serveTask, "task", "", "task key (defaults to ORUN_TASK_KEY)")
	agentServeCmd.Flags().StringVar(&serveDriver, "driver", "claude-code", "driver id")
	parent.AddCommand(agentServeCmd)
}
