package agent

// inputs.go — the head-input seam (specs/orun-agents-live/design.md §2, AL0).
// Heads (the TUI, the cloud console, a second terminal) inject exactly three
// inputs into a live run — steer, verdict, interrupt — plus the graceful end.
// The runtime consumes the queue inside its select loop, so input order is
// log order, and every accepted input lands in the session log as an
// attributed event (message_user / approval_resolved / harness_event) — the
// proof system absorbs interactivity instead of leaking around it.

import (
	"errors"
	"sync"
)

var (
	// ErrSessionDone reports an input submitted after the loop exited.
	ErrSessionDone = errors.New("agent: session is over")
	// ErrNotPending reports a verdict for an approval request that is not
	// awaiting one (already resolved, or never asked).
	ErrNotPending = errors.New("agent: approval request not pending")
)

type inputKind int

const (
	inputSteer inputKind = iota
	inputVerdict
	inputInterrupt
	inputEnd
)

type input struct {
	kind      inputKind
	text      string
	requestID string
	approved  bool
	reason    string
	principal string
	reply     chan error
}

// InputQueue carries head inputs into a running loop. Create one with
// NewInputQueue, set it on RunOptions.Inputs, and hand it to whatever serves
// heads (the attach server). Methods are safe for concurrent use; each blocks
// until the loop accepts (or rejects) the input, so the caller's return is
// the ack. After the session ends every method returns ErrSessionDone.
type InputQueue struct {
	ch        chan input
	done      chan struct{}
	closeOnce sync.Once
}

// NewInputQueue returns a queue ready to be set on RunOptions.Inputs.
func NewInputQueue() *InputQueue {
	return &InputQueue{ch: make(chan input, 16), done: make(chan struct{})}
}

// Steer queues text as a user turn, attributed to principal. It returns once
// the loop accepted the message; the message_user event is appended when the
// message is actually injected into the driver (the log records what the
// agent received, in order).
func (q *InputQueue) Steer(text, principal string) error {
	return q.submit(input{kind: inputSteer, text: text, principal: principal})
}

// Verdict resolves a pending approval request. The first valid verdict wins;
// a verdict for a non-pending request returns ErrNotPending (the attach
// server relays it as ack{ok:false, reason:"not_pending"}).
func (q *InputQueue) Verdict(requestID string, approved bool, reason, principal string) error {
	return q.submit(input{kind: inputVerdict, requestID: requestID, approved: approved, reason: reason, principal: principal})
}

// Interrupt stops the current turn — not the session.
func (q *InputQueue) Interrupt(principal string) error {
	return q.submit(input{kind: inputInterrupt, principal: principal})
}

// End requests the graceful terminal: the body concludes the driver, seals,
// and exits with a canceled outcome (unless the driver reports its own).
func (q *InputQueue) End(principal string) error {
	return q.submit(input{kind: inputEnd, principal: principal})
}

func (q *InputQueue) submit(in input) error {
	in.reply = make(chan error, 1)
	select {
	case <-q.done:
		return ErrSessionDone
	case q.ch <- in:
	}
	select {
	case err := <-in.reply:
		return err
	case <-q.done:
		// The loop may have replied in the same instant it exited; prefer
		// the real answer when one is buffered.
		select {
		case err := <-in.reply:
			return err
		default:
			return ErrSessionDone
		}
	}
}

// close marks the session over; called by the runtime when the loop exits.
func (q *InputQueue) close() { q.closeOnce.Do(func() { close(q.done) }) }
