package attach

import (
	"errors"
	"sync"

	"github.com/sourceplane/orun/internal/agent"
	"github.com/sourceplane/orun/internal/agent/driver"
	"github.com/sourceplane/orun/internal/nodes"
)

// server.go — the body-side core (specs/orun-agents-live/design.md §2): one
// Server per live session, wired to the runtime through RunOptions.Observe /
// ObserveDelta / Inputs. It multiplexes any number of heads: each attach
// replays the event history from the head's cursor, then follows live; head
// inputs route into the runtime's InputQueue and are acked synchronously.
//
// Backpressure (attach-protocol.md §6.2): per-head queues are bounded — past
// the soft limit deltas are dropped (they are cosmetic by contract); past the
// hard limit the head is disconnected with bye{lagged} and simply re-attaches
// from its cursor.

const (
	softLimit = 1024 // frames queued before deltas are shed for this head
	hardLimit = 4096 // frames queued before the head is disconnected
)

// ErrClosed reports an attach to a server whose session already ended.
var ErrClosed = errors.New("attach: session is over")

// SessionInfo is the hello metadata — what a head needs to label the session
// before any events arrive.
type SessionInfo struct {
	SessionID string
	BriefID   string
	AgentType string
	Task      string
	RunKind   string
	Harness   string
	Model     string
}

type storedEvent struct {
	seq     int
	kind    string
	payload map[string]any
	ref     string
}

// Server is the attach plane for one live session.
type Server struct {
	mu     sync.Mutex
	info   SessionInfo
	state  string
	events []storedEvent
	heads  map[*HeadConn]struct{}
	inputs *agent.InputQueue
	closed bool
}

// NewServer creates the attach plane for a session. inputs may be nil for a
// watch-only body (heads can attach but every input is refused as terminal).
func NewServer(info SessionInfo, inputs *agent.InputQueue) *Server {
	return &Server{info: info, state: "running", heads: map[*HeadConn]struct{}{}, inputs: inputs}
}

// Observe is the RunOptions.Observe hook: records the event for replay and
// fans it out to attached heads. Called synchronously by the loop; it never
// blocks (bounded per-head queues, lagged heads are dropped).
func (s *Server) Observe(ev agent.SessionEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, storedEvent{seq: ev.Seq, kind: ev.Kind, payload: ev.Payload, ref: ev.Ref})
	if ev.Kind == nodes.SessionEventStateChanged {
		if st, ok := ev.Payload["state"].(string); ok {
			s.state = st
		}
	}
	f := EventFrame(ev.Seq, ev.Kind, "", ev.Payload, ev.Ref)
	for h := range s.heads {
		h.enqueue(f, false)
	}
}

// ObserveDelta is the RunOptions.ObserveDelta hook: fans streaming text out
// to attached heads. Deltas are never recorded — a head that attaches later
// gets the turn's final message_agent instead.
func (s *Server) ObserveDelta(e driver.Event) {
	turn := 0
	switch v := e.Fields["turn"].(type) {
	case int:
		turn = v
	case float64:
		turn = int(v)
	}
	f := DeltaFrame(turn, e.Text)
	s.mu.Lock()
	defer s.mu.Unlock()
	for h := range s.heads {
		h.enqueue(f, true)
	}
}

// Attach connects a head: hello, replay of events with seq > from, live
// marker, then the live feed. principal is the transport's authenticated
// identity — a head never self-declares it.
func (s *Server) Attach(from int, surface, principal string) (*HeadConn, error) {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil, ErrClosed
	}
	h := &HeadConn{s: s, surface: surface, principal: principal, notify: make(chan struct{}, 1)}
	h.enqueue(HelloFrame(s.info, s.state, len(s.events)-1), false)
	for _, ev := range s.events {
		if ev.seq > from {
			h.enqueue(EventFrame(ev.seq, ev.kind, "", ev.payload, ev.ref), false)
		}
	}
	h.enqueue(LiveFrame(from), false)
	s.heads[h] = struct{}{}
	presence := s.presenceLocked()
	for other := range s.heads {
		other.enqueue(presence, true)
	}
	s.mu.Unlock()
	return h, nil
}

// Close ends the attach plane (the body reached a terminal state): every head
// gets a bye and its feed drains to EOF. Idempotent.
func (s *Server) Close(reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	s.closed = true
	for h := range s.heads {
		h.enqueue(ByeFrame(reason), false)
		h.closeLocked()
		delete(s.heads, h)
	}
}

func (s *Server) presenceLocked() Frame {
	heads := make([]Head, 0, len(s.heads))
	for h := range s.heads {
		heads = append(heads, Head{Principal: h.principal, Surface: h.surface})
	}
	sortHeads(heads)
	return PresenceFrame(heads)
}

func (s *Server) drop(h *HeadConn, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.heads[h]; !ok {
		return
	}
	h.enqueue(ByeFrame(reason), false)
	h.closeLocked()
	delete(s.heads, h)
	presence := s.presenceLocked()
	for other := range s.heads {
		other.enqueue(presence, true)
	}
}

func sortHeads(hs []Head) {
	for i := 1; i < len(hs); i++ {
		for j := i; j > 0 && (hs[j].Principal < hs[j-1].Principal ||
			(hs[j].Principal == hs[j-1].Principal && hs[j].Surface < hs[j-1].Surface)); j-- {
			hs[j], hs[j-1] = hs[j-1], hs[j]
		}
	}
}

// HeadConn is one attached head: the in-process client, and the core the
// socket/relay transports (AL2/AL4) pump frames through.
type HeadConn struct {
	s         *Server
	surface   string
	principal string

	mu     sync.Mutex
	queue  []Frame
	closed bool
	notify chan struct{}
}

// enqueue appends a frame to the head's bounded queue. droppable frames
// (deltas, presence) are shed first under pressure; a head past the hard
// limit is disconnected (it re-attaches from its cursor).
func (h *HeadConn) enqueue(f Frame, droppable bool) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	n := len(h.queue)
	if droppable && n >= softLimit {
		h.mu.Unlock()
		return
	}
	if n >= hardLimit {
		// Disconnect inline: mark closed with a bye so Recv drains to it.
		h.queue = append(h.queue, ByeFrame(ReasonLagged))
		h.closed = true
		h.mu.Unlock()
		h.wake()
		return
	}
	h.queue = append(h.queue, f)
	h.mu.Unlock()
	h.wake()
}

func (h *HeadConn) wake() {
	select {
	case h.notify <- struct{}{}:
	default:
	}
}

// closeLocked is called with the server mutex held (via Close/drop).
func (h *HeadConn) closeLocked() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	h.wake()
}

// Recv returns the next body→head frame, blocking until one is available.
// ok=false means the connection is over and the queue is drained.
func (h *HeadConn) Recv() (Frame, bool) {
	for {
		h.mu.Lock()
		if len(h.queue) > 0 {
			f := h.queue[0]
			h.queue = h.queue[1:]
			h.mu.Unlock()
			return f, true
		}
		closed := h.closed
		h.mu.Unlock()
		if closed {
			return Frame{}, false
		}
		<-h.notify
	}
}

// TryRecv returns the next frame without blocking.
func (h *HeadConn) TryRecv() (Frame, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.queue) == 0 {
		return Frame{}, false
	}
	f := h.queue[0]
	h.queue = h.queue[1:]
	return f, true
}

// Submit handles one head→body frame (the transport-facing entrypoint): the
// input routes into the runtime and the ack comes back on the head's feed,
// correlated by the frame's ref. A version mismatch is refused loud.
func (h *HeadConn) Submit(f Frame) {
	if f.V != Version {
		h.enqueue(ErrorFrame(CodeVersion, "unsupported protocol version"), false)
		h.s.drop(h, CodeVersion)
		return
	}
	switch f.T {
	case TSteer:
		h.ack(f.Ref, h.steer(f.Text))
	case TVerdict:
		approved := f.Approved != nil && *f.Approved
		h.ack(f.Ref, h.verdict(f.RequestID, approved, f.Reason))
	case TInterrupt:
		h.ack(f.Ref, h.interrupt())
	case TEnd:
		h.ack(f.Ref, h.end())
	case TDetach:
		h.Detach()
	case TPong:
		// keepalive answered; nothing to do
	default:
		// Unknown head frames are ignored (forward compatibility).
	}
}

func (h *HeadConn) ack(ref string, err error) {
	switch {
	case err == nil:
		h.enqueue(AckFrame(ref, true, ""), false)
	case errors.Is(err, agent.ErrNotPending):
		h.enqueue(AckFrame(ref, false, ReasonNotPending), false)
	default:
		h.enqueue(AckFrame(ref, false, ReasonTerminal), false)
	}
}

// Steer/Verdict/Interrupt/End are the in-process convenience surface: the
// same inputs Submit routes, with the ack returned synchronously as an error.
func (h *HeadConn) Steer(text string) error { return h.steer(text) }

// Verdict answers a pending approval request.
func (h *HeadConn) Verdict(requestID string, approved bool, reason string) error {
	return h.verdict(requestID, approved, reason)
}

// Interrupt stops the current turn.
func (h *HeadConn) Interrupt() error { return h.interrupt() }

// End requests the graceful terminal.
func (h *HeadConn) End() error { return h.end() }

func (h *HeadConn) steer(text string) error {
	if h.s.inputs == nil {
		return agent.ErrSessionDone
	}
	return h.s.inputs.Steer(text, h.principal)
}

func (h *HeadConn) verdict(requestID string, approved bool, reason string) error {
	if h.s.inputs == nil {
		return agent.ErrSessionDone
	}
	return h.s.inputs.Verdict(requestID, approved, reason, h.principal)
}

func (h *HeadConn) interrupt() error {
	if h.s.inputs == nil {
		return agent.ErrSessionDone
	}
	return h.s.inputs.Interrupt(h.principal)
}

func (h *HeadConn) end() error {
	if h.s.inputs == nil {
		return agent.ErrSessionDone
	}
	return h.s.inputs.End(h.principal)
}

// Detach closes this head politely; the session continues.
func (h *HeadConn) Detach() { h.s.drop(h, "detach") }

// Principal reports the transport-authenticated identity this head's inputs
// are attributed to.
func (h *HeadConn) Principal() string { return h.principal }
