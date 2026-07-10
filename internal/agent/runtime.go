package agent

import (
	"context"
	"fmt"

	"github.com/sourceplane/orun/internal/agent/driver"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// runtime.go — the delegation loop (specs/orun-agents/design.md §1): assemble
// a brief, launch a driver, stream its events into the append-only session
// log, and return the outcome. AG2 wired the loop end-to-end over the driver
// seam; AL0 (specs/orun-agents-live/) adds the head-input seam — steer,
// verdict, interrupt, end — consumed inside the same select loop so input
// order is log order. The loop holds no tool semantics and asserts no work
// status.

// RunResult is the outcome of a delegation loop.
type RunResult struct {
	SessionID  string
	BriefID    string
	Segments   []string
	Outcome    nodes.AgentOutcome
	SnapshotID string // the sealed AgentSessionSnapshot id, when opts.Seal was set
}

// RunOptions configures one run.
type RunOptions struct {
	SessionID string // "as_"-prefixed; caller-supplied for determinism
	Driver    driver.Driver
	Brief     AssembledBrief
	Workdir   string
	Branch    string
	// Approve decides pending approval requests synchronously. It is the
	// headless fallback: when Inputs is set it is never consulted (verdicts
	// arrive from heads); when both are nil, ask-gated tool calls are
	// auto-denied (the safe default for an unattended run).
	Approve func(driver.Event) driver.Verdict
	// Inputs, when set, is the head-input seam (AL0): steers, verdicts,
	// interrupts, and end requests submitted by attached heads are consumed
	// by the loop, logged as attributed events, and forwarded to the driver.
	Inputs *InputQueue
	// Policy gates tool calls the driver reports; when a tool resolves to
	// DecisionAsk the runtime consults Approve. Zero value = deny-by-default.
	Policy ToolPolicy
	// Observe, when set, is called for every event appended to the session
	// log, in order — the live-visibility seam the TUI Agent mode and the
	// cloud DO relay consume. It must not block; the runtime calls it
	// synchronously during the fold.
	Observe func(SessionEvent)
	// ObserveDelta, when set, receives wire-only EventDelta emissions
	// (streaming partial text). Deltas never enter the session log
	// (attach-protocol.md §4); this is their only exit. Must not block.
	ObserveDelta func(driver.Event)
	// Seal, when set with a ref store, makes Run seal an AgentSessionSnapshot
	// on terminal state and point refs/agents/sessions/<id> at it (AG4). The
	// SessionID/Segments/Outcome are filled by the runtime; the caller
	// supplies the run identity (agent type, brief, catalog, principal).
	Seal *SealInput
	Refs refstore.RefStore
}

// SessionEvent is one observed session-log entry (the runtime's live view of
// what it just recorded). It mirrors the sealed nodes.AgentSessionEvent shape
// without the storage concerns.
type SessionEvent struct {
	Seq     int
	Kind    string
	Payload map[string]any
	Ref     string // transcript-chunk blob id, when the event carries one
}

// steerDelivery reports a queued steer the delivery goroutine handed to the
// driver; the loop logs message_user at that moment (the log records what the
// agent actually received, in order).
type steerDelivery struct {
	text      string
	principal string
}

// runLoop is the loop's mutable state, threaded through the fold so the
// select body stays readable.
type runLoop struct {
	log       *SessionLog
	opts      RunOptions
	approve   chan driver.Verdict
	steerCh   chan driver.Message
	interrupt chan struct{}
	cancelRun context.CancelFunc

	pendingAsk map[string]bool // approval requestIDs awaiting a head verdict
	steerQ     []steerDelivery // accepted steers awaiting driver delivery
	delivering bool
	delivered  chan steerDelivery

	outcome nodes.AgentOutcome
	runCtx  context.Context
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

	runCtx, cancelRun := context.WithCancel(ctx)
	defer cancelRun()

	events := make(chan driver.Event, 16)
	l := &runLoop{
		log:        log,
		opts:       opts,
		approve:    make(chan driver.Verdict, 4),
		steerCh:    make(chan driver.Message),
		interrupt:  make(chan struct{}, 1),
		cancelRun:  cancelRun,
		pendingAsk: map[string]bool{},
		delivered:  make(chan steerDelivery, 1),
		outcome:    nodes.AgentOutcome{Status: "failed"},
		runCtx:     runCtx,
	}
	if opts.Inputs != nil {
		defer opts.Inputs.close()
	}
	io := driver.IO{Events: events, Steer: l.steerCh, Approve: l.approve, Interrupt: l.interrupt}

	proc, err := opts.Driver.Launch(runCtx, driver.Brief{
		ID:           opts.Brief.ID,
		Instructions: opts.Brief.Instructions,
		Workdir:      opts.Workdir,
		Branch:       opts.Branch,
	}, io)
	if err != nil {
		return RunResult{}, err
	}

	waitErr := make(chan error, 1)
	go func() { waitErr <- proc.Wait() }()

	var inputCh chan input
	if opts.Inputs != nil {
		inputCh = opts.Inputs.ch
	}

	for {
		select {
		case <-ctx.Done():
			log.Append(nodes.SessionEventError, map[string]any{"error": "canceled"}, "")
			l.outcome.Status = "canceled"
			if _, err := log.Seal(ctx, store); err != nil {
				return RunResult{}, err
			}
			return finish(ctx, store, opts, log, l.outcome, ctx.Err())
		case in := <-inputCh:
			l.handleInput(in)
		case d := <-l.delivered:
			log.Append(nodes.SessionEventMessageUser,
				map[string]any{"text": d.text, "principal": d.principal}, "")
			l.delivering = false
			l.tryDeliver()
		case e, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			l.foldEvent(e)
		case err := <-waitErr:
			// Drain any buffered events the driver emitted before exit.
			for {
				select {
				case e, ok := <-events:
					if !ok {
						events = nil
					} else {
						l.foldEvent(e)
					}
					continue
				default:
				}
				break
			}
			log.Append(nodes.SessionEventStateChanged, map[string]any{"state": l.outcome.Status}, "")
			if _, sErr := log.Seal(ctx, store); sErr != nil {
				return RunResult{}, sErr
			}
			return finish(ctx, store, opts, log, l.outcome, err)
		}
	}
}

// handleInput folds one head input: log it (attributed), forward it to the
// driver, and ack the submitter. Runs inside the loop, so input order is log
// order.
func (l *runLoop) handleInput(in input) {
	switch in.kind {
	case inputSteer:
		l.steerQ = append(l.steerQ, steerDelivery{text: in.text, principal: in.principal})
		l.tryDeliver()
		in.reply <- nil
	case inputVerdict:
		if !l.pendingAsk[in.requestID] {
			in.reply <- ErrNotPending
			return
		}
		delete(l.pendingAsk, in.requestID)
		l.log.Append(nodes.SessionEventApprovalResolved, map[string]any{
			"requestId": in.requestID,
			"approved":  in.approved,
			"reason":    in.reason,
			"principal": in.principal,
		}, "")
		v := driver.Verdict{RequestID: in.requestID, Approved: in.approved, Reason: in.reason}
		select {
		case l.approve <- v:
		case <-l.runCtx.Done():
		}
		in.reply <- nil
	case inputInterrupt:
		l.log.Append(nodes.SessionEventHarness,
			map[string]any{"phase": "interrupted", "principal": in.principal}, "")
		select {
		case l.interrupt <- struct{}{}:
		default: // one interrupt already pending — coalesce
		}
		in.reply <- nil
	case inputEnd:
		l.log.Append(nodes.SessionEventHarness,
			map[string]any{"phase": "end_requested", "principal": in.principal}, "")
		l.outcome.Status = "canceled"
		l.cancelRun()
		in.reply <- nil
	}
}

// tryDeliver hands the next queued steer to the driver without ever blocking
// the loop: one in-flight delivery at a time, completion re-enters the loop
// on l.delivered (where message_user is logged).
func (l *runLoop) tryDeliver() {
	if l.delivering || len(l.steerQ) == 0 {
		return
	}
	next := l.steerQ[0]
	l.steerQ = l.steerQ[1:]
	l.delivering = true
	go func() {
		select {
		case l.steerCh <- driver.Message{Text: next.text}:
			l.delivered <- next
		case <-l.runCtx.Done():
		}
	}()
}

// foldEvent maps one normalized driver event into the session log, enforcing
// tool policy on the way.
func (l *runLoop) foldEvent(e driver.Event) {
	opts := l.opts
	log := l.log
	switch e.Kind {
	case driver.EventDelta:
		// Wire-only: relayed to heads, never logged (attach-protocol.md §4).
		if opts.ObserveDelta != nil {
			opts.ObserveDelta(e)
		}
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
		log.Append(nodes.SessionEventApprovalRequested, approvalPayload(e), "")
		if opts.Inputs != nil {
			// A head answers via InputQueue.Verdict; the request stays
			// pending until then (or the session ends with it unresolved).
			if e.RequestID != "" {
				l.pendingAsk[e.RequestID] = true
			}
			return
		}
		v := driver.Verdict{RequestID: e.RequestID, Approved: false, Reason: "auto-denied (no approver)"}
		if opts.Approve != nil {
			v = opts.Approve(e)
		}
		log.Append(nodes.SessionEventApprovalResolved, map[string]any{
			"requestId": e.RequestID, "approved": v.Approved, "reason": v.Reason,
		}, "")
		select {
		case l.approve <- v:
		default:
		}
	case driver.EventArtifact:
		log.Append(nodes.SessionEventArtifactProduced, e.Fields, "")
	case driver.EventCost:
		log.Append(nodes.SessionEventCostSample, e.Fields, "")
	case driver.EventError:
		log.Append(nodes.SessionEventError, textPayload(e), "")
	case driver.EventDone:
		l.outcome = terminalOutcome(e, l.outcome)
	}
}

// finish assembles the RunResult and, when opts.Seal + opts.Refs are set,
// seals an AgentSessionSnapshot on terminal state and points the session ref
// at it (AG4). A seal failure is surfaced; runErr (the driver's terminal
// error) takes precedence when both occur.
func finish(ctx context.Context, store objectstore.ObjectStore, opts RunOptions, log *SessionLog, outcome nodes.AgentOutcome, runErr error) (RunResult, error) {
	res := RunResult{
		SessionID: opts.SessionID,
		BriefID:   opts.Brief.ID,
		Segments:  log.Segments(),
		Outcome:   outcome,
	}
	if opts.Seal != nil && opts.Refs != nil {
		in := *opts.Seal
		in.SessionID = opts.SessionID
		in.Segments = log.Segments()
		in.Outcome = outcome
		if in.Brief == "" {
			in.Brief = opts.Brief.ID
		}
		id, err := SealSession(ctx, store, opts.Refs, in)
		if err != nil {
			if runErr != nil {
				return res, runErr
			}
			return res, err
		}
		res.SnapshotID = string(id)
	}
	return res, runErr
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

// approvalPayload carries the request id + tool detail heads need to render
// an approval card: the driver's Fields, plus text and the request id.
func approvalPayload(e driver.Event) map[string]any {
	p := map[string]any{}
	for k, v := range e.Fields {
		p[k] = v
	}
	if e.Text != "" {
		p["text"] = e.Text
	}
	if e.RequestID != "" {
		p["requestId"] = e.RequestID
	}
	if len(p) == 0 {
		return nil
	}
	return p
}
