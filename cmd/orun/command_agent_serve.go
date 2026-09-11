package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/agent"
	"github.com/sourceplane/orun/internal/agent/attach"
	"github.com/sourceplane/orun/internal/agent/driver"
	"github.com/sourceplane/orun/internal/agent/ground"
	"github.com/sourceplane/orun/internal/agenttype"
	"github.com/sourceplane/orun/internal/nodes"
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
  ORUN_CLOUD_API    the api-edge base URL (also seeded to the harness as
                    ORUN_BACKEND_URL when that is unset, so the orun MCP
                    server and every in-sandbox 'orun' verb find the platform)
  ORUN_ORG_ID       the workspace (org_…) id
  ORUN_SESSION_ID   the as_… session id (overridable with --session)
  ORUN_SESSION_TOKEN the session bearer (the service-principal credential)

A grounded session (orun-grounded-sessions GS0) also carries its repository
binding, and serve clones it before the loop starts:
  ORUN_REPO_REMOTE    the https clone URL (never carries a credential)
  ORUN_REPO_FULL_NAME the owner/repo full name
  ORUN_REPO_REF       the ref to clone at
Absent ORUN_REPO_REMOTE the session is ungrounded and boots exactly as before.`,
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
		// GS2: publish every token this session holds — the boot one and each
		// refresh — so any `orun` verb the harness spawns authenticates as the
		// live session with no configuration. The lease that kills the session
		// kills this credential too; there is no second thing to revoke.
		tokenFile := ground.TokenFilePath(os.Getenv)
		publishToken := func(tok string) {
			if wErr := ground.WriteSessionToken(tokenFile, tok); wErr != nil {
				fmt.Fprintf(errOut, "orun agent serve: WARNING could not publish the session token (%v) — in-sandbox `orun` verbs will not authenticate\n", wErr)
			}
		}
		publishToken(token)
		hb, hbErr := attach.StartHeartbeat(hbCtx, attach.HeartbeatConfig{
			BaseURL: relayBase, Token: token, Log: errOut, OnToken: publishToken,
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

		// The task-keyed branch is a pure function of type+task, so it is
		// computed here rather than beside RunOptions: grounding needs it to
		// create the branch, and the loop needs the same string.
		branch := ""
		if task != "" {
			branch = "agent/" + task + "-" + slugify(typeName)
		}

		// Grounding (GS0): a bound repository becomes a real checkout before
		// any brief or driver work. It runs AFTER the heartbeat and relay so a
		// failure here is reportable — the console sees the session fail with
		// the stage that broke instead of a silent dark box — and BEFORE the
		// brief so the driver receives the final workdir. A grounded session
		// that cannot reach its repository is not the session that was asked
		// for, so this is terminal, never a degrade-to-ungrounded.
		workdir := ""
		if repo := ground.Detect(os.Getenv); repo != nil {
			fmt.Fprintf(errOut, "orun agent serve: grounding session on %s\n", repo.FullName)
			dir, gErr := ground.Ground(ctx, repo, ground.Options{
				Branch: branch,
				Log:    errOut,
				// GS1: the helper rides the clone command as repo-local config —
				// no global git state, and no window where a fetch could prompt.
				// `!` marks it a shell command; git runs it for every operation
				// on this repo and gets a freshly minted, scoped token back.
				CredentialHelper: "!" + orunBinaryPath() + " git-credential",
			})
			if gErr != nil {
				fmt.Fprintf(errOut, "orun agent serve: grounding failed: %v\n", gErr)
				return gErr
			}
			workdir = dir
			fmt.Fprintf(errOut, "orun agent serve: grounded at %s\n", workdir)
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
			// LoadNamed: authored agents/<type>.md wins; the shipped copy
			// embedded in the binary is the fallback — a bare cloud sandbox
			// has no checkout, and a type-less serve deny-by-defaults every
			// tool the session tries.
			d, issues := agenttype.LoadNamed(typeName)
			if d == nil {
				return fmt.Errorf("agent type %q: %v", typeName, issues)
			}
			persona = d.Body
			toolPolicy = d.Tools
			typeModel = d.Model
			fmt.Fprintf(errOut, "orun agent serve: agent type %s loaded (%s) — tools allow=%d ask=%d deny=%d\n",
				typeName, d.Path, len(toolPolicy.Allow), len(toolPolicy.Ask), len(toolPolicy.Deny))
		} else {
			fmt.Fprintf(errOut, "orun agent serve: WARNING no agent type (--type / ORUN_AGENT_TYPE) — the tool policy is deny-by-default, so every tool call will be denied\n")
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
				agent.NewToolPolicy(toolPolicy), mcpPolicyToolNames(), nil)
			if mErr != nil {
				return mErr
			}
			// Absolute: the harness runs with cmd.Dir = the brief's workdir,
			// which on a grounded session is the checkout, not serve's cwd —
			// a relative --mcp-config would resolve inside the repo and the
			// tool plane would silently vanish.
			mcpConfigPath = setup.ConfigPath
			if abs, aErr := filepath.Abs(mcpConfigPath); aErr == nil {
				mcpConfigPath = abs
			}
			// IS6: hosted skills materialize into the harness workdir as
			// native skill files (the checkout on a grounded session);
			// pins recorded for the PR manifest. Best-effort by design.
			skillWorkdir := workdir
			if skillWorkdir == "" {
				skillWorkdir = "."
			}
			materializeHarnessSkills(ctx, "", "", skillWorkdir, errOut)
			cc := &driver.ClaudeCode{ExtraArgs: append(setup.HarnessArgs(), harnessModelArgs(typeModel)...)}
			// GS1: seed a token for tools that read GITHUB_TOKEN (gh, API
			// callers). Compatibility only — git itself never reads this; it
			// goes through the credential helper, which mints per operation and
			// so never goes stale. This one does, at the token's TTL.
			// GS2: the workspace handle, the rotating credential's location and
			// the backend URL, so `orun cloud check`, state reads, policy-gated
			// secret resolves — and the `orun mcp serve` the harness spawns —
			// all work in-sandbox with zero flags.
			cc.Env = append(cc.Env, harnessPlatformEnv(os.Getenv, tokenFile)...)
			if workdir != "" {
				if tok, tErr := mintHarnessGitToken(ctx); tErr == nil {
					cc.Env = append(cc.Env, "GITHUB_TOKEN="+tok)
				} else {
					fmt.Fprintf(errOut, "orun agent serve: WARNING no GITHUB_TOKEN seeded for the harness (%v) — git still works via the credential helper\n", tErr)
				}
			}
			drv = cc
		}
		if serveDriver == "stub" && runKind == nodes.RunKindInteractive {
			drv = &driver.Stub{Interactive: true}
		}
		opts := agent.RunOptions{
			SessionID:     sessionID,
			Driver:        drv,
			Brief:         brief,
			Workdir:       workdir,
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

// orunBinaryPath is this process's own path — what the git credential helper
// config invokes. `orun` may not be on the PATH git runs with, so the config
// names an absolute binary rather than trusting lookup.
func orunBinaryPath() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "orun"
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

// mintHarnessGitToken fetches one repo-scoped token for the harness env
// (GS1 compatibility seeding). It reuses the credential helper's own cache, so
// boot and the first git operation share a mint rather than spending two.
func mintHarnessGitToken(ctx context.Context) (string, error) {
	session, err := ground.SessionFromEnv(os.Getenv)
	if err != nil {
		return "", err
	}
	cachePath := ground.CachePath(os.Getenv)
	if tok, ok := ground.ReadCachedToken(cachePath, time.Now()); ok {
		return tok.Token, nil
	}
	tok, err := ground.MintRepoToken(ctx, nil, session)
	if err != nil {
		return "", err
	}
	if wErr := ground.WriteCachedToken(cachePath, tok); wErr != nil {
		// A working token in hand beats a cache.
		_ = wErr
	}
	return tok.Token, nil
}

// harnessPlatformEnv is the platform plumbing the harness needs — and every
// `orun` it spawns: the MCP server named in the driver config, the flows' own
// `orun task …` verbs — to reach the workspace with zero flags (GS2): the
// rotating credential's location, the workspace handle, and the backend URL.
//
// The backend URL is the one that was missing. serve dials home on
// ORUN_CLOUD_API, but the CLI resolves its platform backend from
// ORUN_BACKEND_URL (flag > env > intent.yaml > ~/.orun/config.yaml), and a
// sandbox has none of the other three: no intent.yaml on a fresh product, no
// `orun auth login`. So `orun mcp serve` booted "degraded: no backend URL",
// the platform tool plane never mounted, and a baseline brief's Step 1b was
// skipped for want of `epic_create` (observed live). The two names are the
// same host — api-edge serves /v1/organizations/… for both — so the seed is
// exact, and an explicit ORUN_BACKEND_URL in the sandbox env still wins.
func harnessPlatformEnv(getenv func(string) string, tokenFile string) []string {
	env := []string{"ORUN_TOKEN_FILE=" + tokenFile}
	if ws := strings.TrimSpace(getenv("ORUN_WORKSPACE")); ws != "" {
		env = append(env, "ORUN_WORKSPACE="+ws)
	}
	if strings.TrimSpace(getenv(backendURLEnvVar)) == "" {
		if api := strings.TrimSpace(getenv("ORUN_CLOUD_API")); api != "" {
			env = append(env, backendURLEnvVar+"="+api)
		}
	}
	return env
}
