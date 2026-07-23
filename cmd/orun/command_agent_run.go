package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/agent"
	"github.com/sourceplane/orun/internal/agent/attach"
	"github.com/sourceplane/orun/internal/agent/driver"
	"github.com/sourceplane/orun/internal/agent/live"
	"github.com/sourceplane/orun/internal/agenttype"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/worklens"
	"github.com/sourceplane/orun/internal/workmcp"
	"github.com/spf13/cobra"
)

func init() {
	// The stub driver runs the whole loop with no external binary — the
	// local smoke path and the loop tests. Claude Code is the reference
	// driver (orun-agents-live AL1); it stays opt-in (--driver claude-code)
	// until the live smoke has soaked (risks Q2).
	driver.Register(&driver.Stub{})
	driver.Register(&driver.ClaudeCode{})
}

var (
	runTask         string
	runSpecSlug     string
	runType         string
	runDriver       string
	agentRunDryRun  bool
	agentRunJSON    bool
	agentRunDetach  bool
	agentRunSession string // hidden: the pre-minted session id a --detach parent hands its child
)

var agentRunCmd = &cobra.Command{
	Use:   "run",
	Short: "Delegate a task to a coding agent (headless one-shot)",
	Long: `Assemble a frozen, content-addressed brief (base literacy + the agent type's
persona + the task contract + the frozen affected set) and run it through the
chosen driver, streaming the driver's events into an append-only session log.

--dry-run seals and prints the brief without launching — the reviewable "here
is exactly what the agent will see". The contract is read from a pulled spec
snapshot under .orun/specs/<slug>/ (see 'orun spec pull').`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		store, refs, _, ok := openObjectStores()
		if !ok {
			return fmt.Errorf("no object store at .orun — run `orun plan` first")
		}
		ctx := cmd.Context()

		// Resolve the agent type: an authored agents/<type>.md (source of
		// truth), falling back to a sealed one by ref.
		var persona []byte
		var toolPolicy nodes.AgentToolPolicy
		var typeModel string
		if runType != "" {
			// Authored file wins; the shipped embedded copy is the fallback
			// (same resolution as serve — see agenttype.LoadNamed).
			d, issues := agenttype.LoadNamed(runType)
			if d == nil {
				return fmt.Errorf("agent type %q: %v", runType, issues)
			}
			persona = d.Body
			toolPolicy = d.Tools
			typeModel = d.Model
		}

		// Resolve the contract from a pulled spec snapshot, if present.
		var contract *worklens.Contract
		var specID string
		if runSpecSlug != "" {
			snap, err := readPulledSnapshot(runSpecSlug)
			if err != nil {
				return err
			}
			specID = snapshotID(snap)
			if runTask != "" {
				for i := range snap.Tasks {
					if snap.Tasks[i].Key == runTask {
						contract = snap.Tasks[i].Contract
					}
				}
				if contract == nil {
					return fmt.Errorf("task %q not in spec %q", runTask, runSpecSlug)
				}
			}
		}

		runKind := nodes.RunKindImplementation
		if runTask == "" {
			runKind = nodes.RunKindInteractive
		}
		var affected []string
		if contract != nil {
			affected = contract.Affects
		}

		brief, err := agent.AssembleBrief(ctx, store, agent.BriefInput{
			RunKind:  runKind,
			Task:     runTask,
			Persona:  persona,
			Contract: contract,
			SpecID:   specID,
			Affected: affected,
		})
		if err != nil {
			return err
		}
		// Persist the brief ref so it is addressable/replayable.
		_ = refs // brief is content-addressed in the store; ref wiring rides AG4 seal

		out := cmd.OutOrStdout()
		if agentRunDryRun {
			if agentRunJSON {
				enc := json.NewEncoder(out)
				enc.SetIndent("", "  ")
				return enc.Encode(map[string]any{"briefId": brief.ID, "runKind": runKind, "task": runTask})
			}
			fmt.Fprintf(out, "brief   %s\n", brief.ID)
			fmt.Fprintf(out, "runKind %s  task %s\n", runKind, agentOrDash(runTask))
			fmt.Fprintf(out, "\n%s\n", brief.Instructions)
			return nil
		}

		drv, err := driver.Get(runDriver)
		if err != nil {
			return err
		}
		// An interactive run (no --task) over the stub serves the input
		// channels instead of replaying a canned script — the vendor-free
		// interactive session (steer/approve/interrupt through any head).
		if runDriver == "stub" && runKind == nodes.RunKindInteractive {
			drv = &driver.Stub{Interactive: true}
		}
		// The Claude Code driver gets its hands wired: the orun MCP config,
		// written under .orun and filtered through the agent type's tool
		// policy (AL1). The harness-level gates mirror the policy; the
		// runtime fold remains the enforcement authority either way.
		var mcpConfigPath string
		if runDriver == driver.ClaudeCodeID {
			setup, mErr := agent.WriteMCPConfig(filepath.Join(".orun", "agent-mcp"),
				agent.NewToolPolicy(toolPolicy), workmcp.ToolNames(), nil)
			if mErr != nil {
				return mErr
			}
			mcpConfigPath = setup.ConfigPath
			drv = &driver.ClaudeCode{ExtraArgs: append(setup.HarnessArgs(), harnessModelArgs(typeModel)...)}
		}
		branch := ""
		if runTask != "" {
			branch = "agent/" + runTask + "-" + slugify(runType)
		}

		sessionID := agentRunSession
		if sessionID == "" {
			sessionID = newSessionID()
		}
		// --detach: fork the body into its own process group and return; the
		// child (same command, session id pinned) hosts the session. Closing
		// this terminal never kills the run — the tmux discipline (design
		// §3.1). Attach at will.
		if agentRunDetach {
			return detachBody(cmd, sessionID)
		}

		// Seal the session on terminal state so the run is discoverable and
		// replayable (AG4). A session pins its agent type by hash, so sealing
		// needs a sealed type — resolved from its ref; `orun agent import`
		// first. Without one (or for a typeless interactive run) the run still
		// executes and its segments are stored, just not indexed as a session.
		opts := agent.RunOptions{
			SessionID:     sessionID,
			Driver:        drv,
			Brief:         brief,
			Branch:        branch,
			Policy:        agent.NewToolPolicy(toolPolicy),
			MCPConfigPath: mcpConfigPath,
		}
		if runType != "" {
			if ref, rerr := refs.Read(ctx, agentTypeRef(runType)); rerr == nil && ref.Target != "" {
				opts.Refs = refs
				opts.Seal = &agent.SealInput{RunKind: runKind, AgentType: ref.Target, Brief: brief.ID, Principal: "usr_cli"}
			}
		}

		// The body hosts the attach plane while it runs (AL2): heads join on
		// the session socket; the live registry makes it discoverable
		// (`orun agent ps`). The run itself is unchanged when nobody attaches.
		inputs := agent.NewInputQueue()
		srv := attach.NewServer(attach.SessionInfo{
			SessionID: sessionID, BriefID: brief.ID, AgentType: runType,
			Task: runTask, RunKind: string(runKind), Harness: runDriver,
		}, inputs)
		liveDir := agentLiveDir()
		sock := filepath.Join(liveDir, sessionID+".sock")
		listener, err := attach.ServeSocket(srv, sock)
		if err != nil {
			return err
		}
		defer listener.Close()
		if err := live.Write(liveDir, live.Entry{
			SessionID: sessionID, PID: os.Getpid(), Socket: sock, State: "running",
			BriefID: brief.ID, AgentType: runType, Task: runTask, Driver: runDriver,
			StartedAt: time.Now(),
		}); err != nil {
			return err
		}
		defer live.Remove(liveDir, sessionID)
		defer srv.Close("terminal")

		opts.Inputs = inputs
		opts.ObserveDelta = srv.ObserveDelta
		opts.Observe = func(ev agent.SessionEvent) {
			srv.Observe(ev)
			if ev.Kind == "state_changed" {
				if st, ok := ev.Payload["state"].(string); ok {
					_ = live.UpdateState(liveDir, sessionID, st)
				}
			}
			if !agentRunJSON {
				renderEventLine(out, ev.Kind, ev.Payload)
			}
		}
		if !agentRunJSON {
			fmt.Fprintf(out, "session %s hosting — attach from another terminal: orun agent attach %s\n", sessionID, sessionID)
		}

		res, err := agent.Run(ctx, store, opts)
		if err != nil {
			return err
		}
		if agentRunJSON {
			enc := json.NewEncoder(out)
			enc.SetIndent("", "  ")
			return enc.Encode(res)
		}
		fmt.Fprintf(out, "session  %s (driver %s)\n", res.SessionID, runDriver)
		fmt.Fprintf(out, "brief    %s\n", res.BriefID)
		fmt.Fprintf(out, "outcome  %s", res.Outcome.Status)
		if res.Outcome.PR != "" {
			fmt.Fprintf(out, "  pr=%s", res.Outcome.PR)
		}
		fmt.Fprintln(out)
		fmt.Fprintf(out, "segments %d sealed\n", len(res.Segments))
		if res.SnapshotID != "" {
			fmt.Fprintf(out, "session  sealed %s (refs/%s)\n", res.SnapshotID, agent.SessionRef(res.SessionID))
			fmt.Fprintf(out, "replay   orun agent replay %s\n", res.SessionID)
		}
		return nil
	},
}

func agentOrDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func slugify(s string) string {
	if s == "" {
		return "run"
	}
	return s
}

// newSessionID mints an as_-prefixed session id for a local run; the cloud
// mints its own ULID session ids.
func newSessionID() string {
	var b [10]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "as_local"
	}
	return "as_" + hex.EncodeToString(b[:])
}

func readPulledSnapshot(slug string) (*worklens.SpecSnapshot, error) {
	p := filepath.Join(".orun", "specs", slug, "snapshot.json")
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, fmt.Errorf("no pulled spec %q (run `orun spec pull %s`): %w", slug, slug, err)
	}
	var snap worklens.SpecSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil, fmt.Errorf("spec %q snapshot: %w", slug, err)
	}
	return &snap, nil
}

func snapshotID(snap *worklens.SpecSnapshot) string {
	if snap == nil {
		return ""
	}
	if id, _, err := worklens.SealSpecSnapshot(*snap); err == nil {
		return id
	}
	return ""
}

func registerAgentRunCommand(parent *cobra.Command) {
	agentRunCmd.Flags().StringVar(&runTask, "task", "", "task key to implement (e.g. ORN-142)")
	agentRunCmd.Flags().StringVar(&runSpecSlug, "spec", "", "pulled spec slug supplying the task contract")
	agentRunCmd.Flags().StringVar(&runType, "type", "", "agent type (agents/<type>.md) — persona + tool policy")
	agentRunCmd.Flags().StringVar(&runDriver, "driver", "stub", "driver id (see `orun agent drivers`)")
	agentRunCmd.Flags().BoolVar(&agentRunDryRun, "dry-run", false, "seal and print the brief without launching")
	agentRunCmd.Flags().BoolVar(&agentRunJSON, "json", false, "JSON output")
	agentRunCmd.Flags().BoolVar(&agentRunDetach, "detach", false, "run the session body in its own process group and return (attach at will)")
	agentRunCmd.Flags().StringVar(&agentRunSession, "session-id", "", "pre-minted session id (internal: set by --detach)")
	_ = agentRunCmd.Flags().MarkHidden("session-id")
	parent.AddCommand(agentRunCmd)
	agentKillCmd.Flags().BoolVar(&agentKillForce, "force", false, "kill the body process and sweep the registry entry")
	parent.AddCommand(agentPsCmd)
	parent.AddCommand(agentAttachCmd)
	parent.AddCommand(agentKillCmd)

	parent.AddCommand(&cobra.Command{
		Use:   "drivers",
		Short: "List registered agent drivers",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			for _, id := range driver.IDs() {
				fmt.Fprintln(cmd.OutOrStdout(), id)
			}
			return nil
		},
	})

	parent.AddCommand(&cobra.Command{
		Use:   "doctor",
		Short: "Check the agent runtime environment (drivers, harness binary, MCP surface)",
		Long: `Report what the live plane needs to run for real: the registered drivers,
whether the Claude Code harness binary is reachable (and its version — the
driver's wire protocol is pinned by fixtures, so version drift is worth
knowing about before a session hits it), and the orun MCP tool surface a
brief's tool policy filters.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			out := cmd.OutOrStdout()
			fmt.Fprintf(out, "drivers    %v\n", driver.IDs())
			fmt.Fprintf(out, "mcp tools  %d (orun mcp serve)\n", len(workmcp.ToolNames()))
			path, err := exec.LookPath("claude")
			if err != nil {
				fmt.Fprintln(out, "claude     not found on PATH — `--driver claude-code` needs the Claude Code CLI")
				return nil
			}
			vctx, cancel := context.WithTimeout(cmd.Context(), 5*time.Second)
			defer cancel()
			ver, verr := exec.CommandContext(vctx, path, "--version").Output()
			if verr != nil {
				fmt.Fprintf(out, "claude     %s (version check failed: %v)\n", path, verr)
				return nil
			}
			fmt.Fprintf(out, "claude     %s (%s)\n", path, strings.TrimSpace(string(ver)))
			return nil
		},
	})
}
