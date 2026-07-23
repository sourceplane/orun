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
		// FIRST line, unconditionally: which binary is actually running. A stale
		// baked-in image once ran an old orun for four sessions while we shipped
		// fixes into a void; the process announcing its own version is the
		// cheapest insurance against ever wondering again. Matches `orun
		// --version` ("dev" in local builds, the tag in releases).
		fmt.Fprintf(errOut, "orun agent serve: orun version %s\n", version)
		sessionID := serveSessionID
		if sessionID == "" {
			sessionID = os.Getenv("ORUN_SESSION_ID")
		}
		cloudAPI := os.Getenv("ORUN_CLOUD_API")
		orgID := os.Getenv("ORUN_ORG_ID")
		token := os.Getenv("ORUN_SESSION_TOKEN")

		// Dial-home identity is create-time sandbox env the control plane set on
		// the box. Log (redacted) which of the four actually reached this
		// process — the first line to read when a session never leaves
		// `provisioning`. serve needs all four to build the cloud URL and
		// heartbeat; if the identity trio is empty while ORUN_SESSION_TOKEN
		// (injected separately as the toolbox exec's `export` prefix) is present,
		// that points at the toolbox exec not inheriting sandbox env — an
		// orun-cloud bootstrap issue. (NOTE: a full identity here is necessary
		// but not sufficient — the session only flips provisioning→running once
		// serve POSTs /heartbeat, which is the actual root-cause fix.)
		fmt.Fprintf(errOut, "orun agent serve: dial-home identity — ORUN_CLOUD_API=%s ORUN_ORG_ID=%s ORUN_SESSION_ID=%s ORUN_SESSION_TOKEN=%s\n",
			orMissing(cloudAPI), orMissing(orgID), orMissing(sessionID), redactSecret(token))
		if err := checkServeIdentity(cloudAPI, orgID, sessionID, token); err != nil {
			return err
		}
		// Model credential diagnostics (redacted): which of the harness env
		// vars actually reached this process. The provision path injects either
		// ANTHROPIC_API_KEY (an Anthropic connection) or ANTHROPIC_BASE_URL +
		// ANTHROPIC_AUTH_TOKEN (a gateway connection), plus ANTHROPIC_MODEL —
		// a missing credential fails only on the harness's FIRST TURN, after
		// the session already reads `running`, so name it at boot instead.
		fmt.Fprintf(errOut, "orun agent serve: model env — ANTHROPIC_API_KEY=%s ANTHROPIC_AUTH_TOKEN=%s ANTHROPIC_BASE_URL=%s ANTHROPIC_MODEL=%s\n",
			redactSecret(os.Getenv("ANTHROPIC_API_KEY")), redactSecret(os.Getenv("ANTHROPIC_AUTH_TOKEN")),
			orMissing(os.Getenv("ANTHROPIC_BASE_URL")), orMissing(os.Getenv("ANTHROPIC_MODEL")))
		if os.Getenv("ANTHROPIC_API_KEY") == "" && os.Getenv("ANTHROPIC_AUTH_TOKEN") == "" {
			fmt.Fprintf(errOut, "orun agent serve: WARNING no model credential in env — the harness will fail its first turn (check the workspace's model connection in Settings › AI providers)\n")
		}
		relayBase := fmt.Sprintf("%s/v1/organizations/%s/agents/sessions/%s", cloudAPI, orgID, sessionID)
		ctx := cmd.Context()

		// Heartbeat FIRST — it is the session's sole liveness (contract). The
		// first beat is the only thing that flips provisioning→running, stamps
		// started_at, and sets the lease to now+15m; POST /events never touches
		// the lease. Send it before we pull the brief or start the agent, and
		// fail loudly if it never lands — a run whose session the cloud never
		// marks running is dead on arrival and would be swept as lease_lost with
		// no logs. The loop then beats every 5m and refreshes the 15m token; a
		// terminal beat (console kill, lapsed lease) cancels the run.
		hbCtx, hbCancel := context.WithCancel(ctx)
		defer hbCancel()
		runCtx, runCancel := context.WithCancel(ctx)
		defer runCancel()
		fmt.Fprintf(errOut, "orun agent serve: sending first heartbeat — session %s → %s\n", sessionID, relayBase)
		hb, hbErr := attach.StartHeartbeat(hbCtx, attach.HeartbeatConfig{
			BaseURL: relayBase, Token: token, Log: errOut,
		}, func(reason string) {
			fmt.Fprintf(errOut, "orun agent serve: session ended by cloud (heartbeat terminal): %s\n", reason)
			runCancel()
		})
		if hbErr != nil {
			return fmt.Errorf("session heartbeat failed: %w", hbErr)
		}

		// Identity/type/task resolve from env+flags (no I/O), so the event relay
		// can go up BEFORE any brief pull or driver setup. The relay is the
		// console's eyes and the steer intake — it must NOT be gated behind
		// object-store or driver work that could block. (This is why a prior run
		// showed a live heartbeat but a totally dark relay: serve had reached the
		// heartbeat but not yet DialToRelay.)
		typeName := serveType
		if typeName == "" {
			typeName = os.Getenv("ORUN_AGENT_TYPE")
		}
		task := serveTask
		if task == "" {
			task = os.Getenv("ORUN_TASK_KEY")
		}
		runKind := nodes.RunKindImplementation
		if task == "" {
			runKind = nodes.RunKindInteractive
		}

		// Event relay: the DURABLE console log. POST /events fills the DB the
		// console polls (GET /events); the write-path is resilient (retry + loud
		// log + drop) and never gates the run — liveness is the heartbeat above.
		// Its bearer tracks the heartbeat's token refreshes (TokenFn). pumpDown
		// drains the input return-queue and acks steers so the console's
		// fail-visible POST /input resolves.
		inputs := agent.NewInputQueue()
		srv := attach.NewServer(attach.SessionInfo{
			SessionID: sessionID, AgentType: typeName,
			Task: task, RunKind: string(runKind), Harness: serveDriver,
		}, inputs)
		relayCtx, relayCancel := context.WithCancel(ctx)
		defer relayCancel()
		fmt.Fprintf(errOut, "orun agent serve: connecting event relay — %s\n", relayBase)
		relaySession, rerr := attach.DialToRelay(relayCtx, srv, inputs, attach.RelayConfig{
			BaseURL: relayBase, Token: token, TokenFn: hb.Token, Log: errOut,
		})
		if rerr != nil {
			fmt.Fprintf(errOut, "orun agent serve: WARNING event relay unavailable (%v) — session continues on heartbeat; console tail degraded\n", rerr)
		} else {
			fmt.Fprintf(errOut, "orun agent serve: event relay connected\n")
		}

		// Now the (potentially blocking) brief + driver setup — the relay is
		// already streaming and draining steers regardless of how this goes.
		//
		// A cloud serve boots into a bare sandbox: the orun-cloud bootstrap
		// installs the binary and execs `orun agent serve` with NO prior
		// `orun plan`, so there is no `.orun` object store on the box. That is
		// expected, not an error — the brief here is assembled in-process from
		// env (RunKind/Task/Persona), not pulled from a sealed graph, so serve
		// only needs a WRITABLE store to seal that synthesized brief into.
		// Initialize an empty local store when none exists (openObjectModel
		// MkdirAll's it) instead of dying with "no object store"; a pre-seeded
		// store from a real plan is used as-is. Without this every cloud session
		// reaches `running` (heartbeat lands) then serve exits 1 here, leaving
		// the console conversation empty and steers with no consumer.
		store, refs, _, ok := openObjectStores()
		if !ok {
			var oerr error
			if store, refs, _, oerr = openObjectModel(); oerr != nil {
				return fmt.Errorf("initialize object store at .orun: %w", oerr)
			}
			fmt.Fprintf(errOut, "orun agent serve: no prior plan — initialized empty object store at .orun\n")
		}
		var persona []byte
		var toolPolicy nodes.AgentToolPolicy
		var typeModel string
		if typeName != "" {
			d, issues := agenttype.Load(filepath.Join("agents", typeName+".md"))
			if d == nil {
				return fmt.Errorf("agent type %q: %v", typeName, issues)
			}
			persona = d.Body
			toolPolicy = d.Tools
			typeModel = d.Model
		}
		fmt.Fprintf(errOut, "orun agent serve: assembling brief (type=%q task=%q)\n", typeName, task)
		brief, err := agent.AssembleBrief(ctx, store, agent.BriefInput{
			RunKind: runKind, Task: task, Persona: persona,
		})
		if err != nil {
			return err
		}
		fmt.Fprintf(errOut, "orun agent serve: brief %s ready\n", brief.ID)
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
			drv = &driver.ClaudeCode{ExtraArgs: append(setup.HarnessArgs(), harnessModelArgs(typeModel)...)}
		}
		if serveDriver == "stub" && runKind == nodes.RunKindInteractive {
			drv = &driver.Stub{Interactive: true}
		}
		branch := ""
		if task != "" {
			branch = "agent/" + task + "-" + slugify(typeName)
		}

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
		fmt.Fprintf(errOut, "orun agent serve: starting agent loop — session %s\n", sessionID)
		res, err := agent.Run(runCtx, store, opts)
		srv.Close("terminal")
		relaySession.Close() // nil-safe when the relay was unavailable
		relayCancel()
		hbCancel()
		if err != nil {
			fmt.Fprintf(errOut, "orun agent serve: agent loop ended with error: %v\n", err)
			return err
		}
		fmt.Fprintf(errOut, "orun agent serve: session %s ended: %s\n", res.SessionID, res.Outcome.Status)
		return nil
	},
}

// harnessModelArgs pins the agent type's `model:` frontmatter on the harness
// (--model). This was declared-but-never-applied for four releases: agenttype
// parsed the field, HarnessArgs only ever emitted tool gates, and the claude
// CLI silently ran its own default model. The env pin wins when present —
// ANTHROPIC_MODEL is the provision path's explicit choice (profile model or
// the connection's pinned model), and the --model flag would override it.
func harnessModelArgs(model string) []string {
	if model == "" || os.Getenv("ANTHROPIC_MODEL") != "" {
		return nil
	}
	return []string{"--model", model}
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
		return fmt.Errorf("dial-home identity missing (%s) while ORUN_SESSION_TOKEN is present — "+
			"serve cannot build the cloud URL and cannot heartbeat. "+
			"If this recurs in-sandbox, suspect the Daytona toolbox exec not inheriting box-create sandbox env: "+
			"the token arrives via the exec `export` prefix while the identity vars are only sandbox env — "+
			"that would be an orun-cloud bootstrap fix (export the identity vars in the exec prefix)",
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
