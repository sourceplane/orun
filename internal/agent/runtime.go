package agent

import (
	"context"
	"fmt"

	"github.com/sourceplane/orun/internal/agent/driver"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
)

// runtime.go — the delegation loop (specs/orun-agents/design.md §1): assemble
// a brief, launch a driver, stream its events into the append-only session
// log, and return the outcome. AG2 wires the loop end-to-end over the driver
// seam; the Claude Code driver + session sealing on terminal state land in
// AG4. The loop holds no tool semantics and asserts no work status.

// RunResult is the outcome of a delegation loop.
type RunResult struct {
	SessionID string
	BriefID   string
	Segments  []string
	Outcome   nodes.AgentOutcome
}

// RunOptions configures one run.
type RunOptions struct {
	SessionID string // "as_"-prefixed; caller-supplied for determinism
	Driver    driver.Driver
	Brief     AssembledBrief
	Workdir   string
	Branch    string
	// Approve decides pending approval requests. When nil, ask-gated tool
	// calls are auto-denied (the safe default for a headless/local run).
	Approve func(driver.Event) driver.Verdict
	// Policy gates tool calls the driver reports; when a tool resolves to
	// DecisionAsk the runtime consults Approve. Zero value = deny-by-default.
	Policy ToolPolicy
	// Observe, when set, is called for every event appended to the session
	// log, in order — the live-visibility seam the TUI Agent mode and the
	// cloud DO relay consume. It must not block; the runtime calls it
	// synchronously during the fold.
	Observe func(SessionEvent)
}

// SessionEvent is one observed session-log entry (the runtime's live view of
// what it just recorded). It mirrors the sealed nodes.AgentSessionEvent shape
// without the storage concerns.
type SessionEvent struct {
	Seq     int
	Kind    string
	Payload map[string]any
}

// Run executes the loop: launch the driver on the brief, fold its normalized
// event stream into the session log, and produce the outcome. Blocks until the
// driver terminates (EventDone or channel close).
func Run(ctx context.Context, store objectstore.ObjectStore, opts RunOptions) (RunResult, error) {
	if opts.SessionID == "" {
		return RunResult{}, fmt.Errorf("agent: run sessionID empty")
	}
	if opts.Driver == nil {
		return RunResult{}, fmt.Errorf("agent: run driver nil")
	}
	log := NewSessionLog(opts.SessionID)
	if opts.Observe != nil {
		log.Observe(opts.Observe)
	}
	log.Append(nodes.SessionEventStateChanged, map[string]any{"state": "running"}, "")

	events := make(chan driver.Event, 16)
	steer := make(chan driver.Message)
	approve := make(chan driver.Verdict, 4)
	io := driver.IO{Events: events, Steer: steer, Approve: approve}

	proc, err := opts.Driver.Launch(ctx, driver.Brief{
		ID:           opts.Brief.ID,
		Instructions: opts.Brief.Instructions,
		Workdir:      opts.Workdir,
		Branch:       opts.Branch,
	}, io)
	if err != nil {
		return RunResult{}, err
	}

	outcome := nodes.AgentOutcome{Status: "failed"}
	waitErr := make(chan error, 1)
	go func() { waitErr <- proc.Wait() }()

	for {
		select {
		case <-ctx.Done():
			log.Append(nodes.SessionEventError, map[string]any{"error": "canceled"}, "")
			outcome.Status = "canceled"
			if _, err := log.Seal(ctx, store); err != nil {
				return RunResult{}, err
			}
			return RunResult{SessionID: opts.SessionID, BriefID: opts.Brief.ID, Segments: log.Segments(), Outcome: outcome}, ctx.Err()
		case e, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			foldEvent(log, opts, approve, e, &outcome)
		case err := <-waitErr:
			// Drain any buffered events the driver emitted before exit.
			for {
				select {
				case e, ok := <-events:
					if !ok {
						events = nil
					} else {
						foldEvent(log, opts, approve, e, &outcome)
					}
					continue
				default:
				}
				break
			}
			log.Append(nodes.SessionEventStateChanged, map[string]any{"state": outcome.Status}, "")
			if _, sErr := log.Seal(ctx, store); sErr != nil {
				return RunResult{}, sErr
			}
			return RunResult{SessionID: opts.SessionID, BriefID: opts.Brief.ID, Segments: log.Segments(), Outcome: outcome}, err
		}
	}
}

// foldEvent maps one normalized driver event into the session log, enforcing
// tool policy on the way. Returns true when the event is terminal (EventDone).
func foldEvent(log *SessionLog, opts RunOptions, approve chan<- driver.Verdict, e driver.Event, outcome *nodes.AgentOutcome) bool {
	switch e.Kind {
	case driver.EventMessage:
		log.Append(nodes.SessionEventMessageAgent, textPayload(e), e.RequestID)
	case driver.EventToolCall:
		tool, _ := e.Fields["tool"].(string)
		dec := opts.Policy.Decide(tool)
		log.Append(nodes.SessionEventToolCall, map[string]any{"tool": tool, "decision": dec.String()}, "")
		if dec == DecisionDeny {
			log.Append(nodes.SessionEventError, map[string]any{"tool": tool, "error": "denied by tool policy"}, "")
		}
	case driver.EventToolResult:
		log.Append(nodes.SessionEventToolResult, textPayload(e), e.RequestID)
	case driver.EventApproval:
		log.Append(nodes.SessionEventApprovalRequested, textPayload(e), "")
		v := driver.Verdict{RequestID: e.RequestID, Approved: false, Reason: "auto-denied (no approver)"}
		if opts.Approve != nil {
			v = opts.Approve(e)
		}
		log.Append(nodes.SessionEventApprovalResolved, map[string]any{"approved": v.Approved, "reason": v.Reason}, "")
		select {
		case approve <- v:
		default:
		}
	case driver.EventArtifact:
		log.Append(nodes.SessionEventArtifactProduced, e.Fields, "")
	case driver.EventCost:
		log.Append(nodes.SessionEventCostSample, e.Fields, "")
	case driver.EventError:
		log.Append(nodes.SessionEventError, textPayload(e), "")
	case driver.EventDone:
		*outcome = terminalOutcome(e, *outcome)
		return true
	}
	return false
}

func terminalOutcome(e driver.Event, prev nodes.AgentOutcome) nodes.AgentOutcome {
	out := prev
	if s, ok := e.Fields["status"].(string); ok && validTerminal(s) {
		out.Status = s
	} else {
		out.Status = "completed"
	}
	if pr, ok := e.Fields["pr"].(string); ok {
		out.PR = pr
	}
	if br, ok := e.Fields["branch"].(string); ok {
		out.Branch = br
	}
	return out
}

func validTerminal(s string) bool {
	switch s {
	case "completed", "failed", "canceled", "expired":
		return true
	default:
		return false
	}
}

func textPayload(e driver.Event) map[string]any {
	if e.Text == "" {
		return nil
	}
	return map[string]any{"text": e.Text}
}
