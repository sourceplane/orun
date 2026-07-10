package main

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/agent/attach"
	"github.com/sourceplane/orun/internal/agent/live"
	"github.com/spf13/cobra"
)

// command_agent_live.go — the session-host surface (orun-agents-live AL2):
// `orun agent ps` lists live bodies from the registry (sweeping the dead),
// `orun agent attach` joins one as a line-mode head (the TUI head lands with
// AL3 and reuses the same client), `orun agent kill` ends one gracefully (or
// force-reaps it).

// agentLiveDir is the live registry directory (design §3.2) — ephemeral
// state beside the object store, never content.
func agentLiveDir() string {
	return filepath.Join(storeDir(), ".orun", "agents", "live")
}

var agentPsCmd = &cobra.Command{
	Use:   "ps",
	Short: "List live agent sessions on this machine",
	Args:  cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		entries, err := live.List(agentLiveDir())
		if err != nil {
			return err
		}
		out := cmd.OutOrStdout()
		if len(entries) == 0 {
			fmt.Fprintln(out, "no live sessions — start one with `orun agent run` (add --detach to leave it running)")
			return nil
		}
		fmt.Fprintf(out, "%-24s %-14s %-10s %-10s %-8s %s\n", "SESSION", "TYPE", "TASK", "STATE", "AGE", "PID")
		for _, e := range entries {
			age := time.Since(e.StartedAt).Round(time.Second)
			fmt.Fprintf(out, "%-24s %-14s %-10s %-10s %-8s %d\n",
				e.SessionID, agentDash(e.AgentType), agentDash(e.Task), e.State, age, e.PID)
		}
		fmt.Fprintln(out, "\nattach: orun agent attach <session>   end: orun agent kill <session>")
		return nil
	},
}

var agentKillForce bool

var agentKillCmd = &cobra.Command{
	Use:   "kill <sessionId>",
	Short: "End a live agent session (graceful; --force reaps the body)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		dir := agentLiveDir()
		e, err := live.Resolve(dir, id)
		if err != nil {
			return fmt.Errorf("no live session %q (see `orun agent ps`)", id)
		}
		out := cmd.OutOrStdout()
		if agentKillForce {
			if p, ferr := os.FindProcess(e.PID); ferr == nil {
				_ = p.Kill()
			}
			live.Remove(dir, id)
			fmt.Fprintf(out, "session %s force-killed (pid %d); sealed segments up to the last seal survive\n", id, e.PID)
			return nil
		}
		c, err := attach.DialSocket(e.Socket, int(^uint(0)>>1), "cli") // cursor at max: no replay, control only
		if err != nil {
			return fmt.Errorf("dial %s: %w (body dead? try --force)", e.Socket, err)
		}
		defer c.Detach()
		if err := c.End(); err != nil {
			return fmt.Errorf("end: %w", err)
		}
		fmt.Fprintf(out, "session %s ending (graceful) — the body seals and exits\n", id)
		return nil
	},
}

var agentAttachCmd = &cobra.Command{
	Use:   "attach <sessionId>",
	Short: "Attach this terminal to a live agent session (replay, follow, steer, approve)",
	Long: `Attach a head to a live session: replays the event history, then follows
live. Everything you type is a steer (a user turn, attributed and sealed into
the session log). Commands:

  /approve <requestId> [reason]   answer a pending approval
  /deny <requestId> [reason]
  /interrupt                      stop the current turn (not the session)
  /end                            graceful terminal: the body seals and exits
  /detach                         leave; the session keeps running (also Ctrl+D)

The session survives detach — this terminal is a head, not the body.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		id := args[0]
		// Local first: a live body on this machine attaches over its socket.
		e, err := live.Resolve(agentLiveDir(), id)
		if err == nil {
			c, derr := attach.DialSocket(e.Socket, -1, "cli")
			if derr != nil {
				return fmt.Errorf("dial %s: %w", e.Socket, derr)
			}
			return runLineHead(cmd, socketHead{c})
		}
		if !errors.Is(err, live.ErrNotFound) {
			return err
		}
		// Not local: a remote as_… session resolves over the cloud relay
		// (orun-agents-live AL4). Same head, transport swapped.
		return attachRemote(cmd, id)
	},
}

// attachRemote attaches the line head to a remote session over the cloud
// relay: cliauth bearer → SSE feed + input POSTs (design §6.2).
func attachRemote(cmd *cobra.Command, sessionID string) error {
	cloudAPI := os.Getenv("ORUN_CLOUD_API")
	orgID := os.Getenv("ORUN_ORG_ID")
	token := os.Getenv("ORUN_SESSION_TOKEN")
	if cloudAPI == "" || orgID == "" {
		return fmt.Errorf("no live local session %q, and no cloud attach config (set ORUN_CLOUD_API + ORUN_ORG_ID; see `orun agent ps`)", sessionID)
	}
	base := fmt.Sprintf("%s/v1/organizations/%s/agents/sessions/%s", cloudAPI, orgID, sessionID)
	c, err := attach.DialRelay(cmd.Context(), base, token, -1, nil)
	if err != nil {
		return fmt.Errorf("attach %s over relay: %w", sessionID, err)
	}
	return runLineHead(cmd, c)
}

// lineHead is the transport-agnostic head surface the line renderer drives:
// the local SocketClient and the remote RelayHeadClient both satisfy it, so
// `orun agent attach` renders a local and a Daytona session identically.
type lineHead interface {
	Frames() <-chan attach.Frame
	Steer(text string) error
	Verdict(requestID string, approved bool, reason string) error
	Interrupt() error
	End() error
	Detach()
}

// socketHead adapts *attach.SocketClient to lineHead (it already has the
// methods; the wrapper just names the interface at the call site).
type socketHead struct{ *attach.SocketClient }

// runLineHead is the AL2 head: a plain-text render of the feed plus a stdin
// command loop. The AL3 TUI head replaces this as the default surface; line
// mode stays for pipes and minimal terminals.
func runLineHead(cmd *cobra.Command, c lineHead) error {
	out := cmd.OutOrStdout()

	// stdin → inputs
	go func() {
		sc := bufio.NewScanner(cmd.InOrStdin())
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" {
				continue
			}
			if err := lineCommand(c, line); err != nil {
				if errors.Is(err, errDetachRequested) {
					c.Detach()
					return
				}
				fmt.Fprintf(out, "! %v\n", err)
			}
		}
		c.Detach() // EOF (Ctrl+D)
	}()

	for f := range c.Frames() {
		renderFrame(out, f)
		if f.T == attach.TBye {
			break
		}
	}
	return nil
}

var errDetachRequested = errors.New("detach requested")

func lineCommand(c lineHead, line string) error {
	if !strings.HasPrefix(line, "/") {
		return c.Steer(line)
	}
	fields := strings.Fields(line)
	switch fields[0] {
	case "/approve", "/deny":
		if len(fields) < 2 {
			return fmt.Errorf("usage: %s <requestId> [reason]", fields[0])
		}
		reason := strings.Join(fields[2:], " ")
		return c.Verdict(fields[1], fields[0] == "/approve", reason)
	case "/interrupt":
		return c.Interrupt()
	case "/end":
		return c.End()
	case "/detach":
		return errDetachRequested
	default:
		// Not a head command — it is conversation. Steers pass through
		// verbatim (a driver may well have its own slash-words).
		return c.Steer(line)
	}
}

func renderFrame(out interface{ Write([]byte) (int, error) }, f attach.Frame) {
	p := func(format string, a ...any) { fmt.Fprintf(out, format+"\n", a...) }
	switch f.T {
	case attach.THello:
		p("attached %s  type=%s task=%s state=%s (driver %s)", f.SessionID, agentDash(f.AgentType), agentDash(f.Task), f.State, agentDash(f.Harness))
		p("type to steer; /approve <id> · /deny <id> · /interrupt · /end · /detach")
	case attach.TLive:
		p("── live ──")
	case attach.TEvent:
		renderEventLine(out, f.Kind, f.Payload)
	case attach.TBye:
		p("── detached (%s) ──", f.Reason)
	case attach.TPresence:
		names := make([]string, 0, len(f.Heads))
		for _, h := range f.Heads {
			names = append(names, h.Principal+"/"+h.Surface)
		}
		p("· heads: %s", strings.Join(names, ", "))
	case attach.TError:
		p("! protocol error: %s %s", f.Code, f.Message)
	}
}

// renderEventLine renders one session event for line output; shared by the
// attach head and `orun agent run`'s inline view.
func renderEventLine(out interface{ Write([]byte) (int, error) }, kind string, payload map[string]any) {
	p := func(format string, a ...any) { fmt.Fprintf(out, format+"\n", a...) }
	str := func(k string) string { s, _ := payload[k].(string); return s }
	switch kind {
	case "message_agent":
		p("agent> %s", str("text"))
	case "message_user":
		p("%s> %s", agentDash(str("principal")), str("text"))
	case "tool_call":
		p("  [tool] %s (%s)", str("tool"), str("decision"))
	case "tool_result":
		p("  [result] %s", truncateLine(str("text"), 160))
	case "approval_requested":
		p("  [approval needed] requestId=%s tool=%s — /approve %s or /deny %s", str("requestId"), str("tool"), str("requestId"), str("requestId"))
	case "approval_resolved":
		verdict := "denied"
		if b, _ := payload["approved"].(bool); b {
			verdict = "approved"
		}
		p("  [approval] %s %s by %s", str("requestId"), verdict, agentDash(str("principal")))
	case "artifact_produced":
		pr, _ := payload["pr"].(string)
		p("  [artifact] pr=%s", pr)
	case "state_changed":
		p("  [state] %s", str("state"))
	case "harness_event":
		p("  [harness] %s", agentDash(str("phase")))
	case "cost_sample":
		p("  [cost] tokens=%v", payload["tokens"])
	case "error":
		p("  [error] %s", str("text"))
	}
}

func agentDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func truncateLine(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
