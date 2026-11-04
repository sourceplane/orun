package agent

import "sync"

// notices.go — events the body reports on its own, beside the driver's: the
// live preview's preview_changed (saas-parallel-baselining PB-12). The session
// log has one writer, the loop, so a reporter posts here and the loop appends
// in arrival order. Post never blocks, so a reporter running inside the loop
// (RunOptions.BeforeEnd) cannot wedge it.

// Notice is one body-reported event: a kind from the closed vocabulary and
// its payload.
type Notice struct {
	Kind    string
	Payload map[string]any
}

// Notices is an unbounded queue of body-reported events. Safe for concurrent
// use; the zero value is not usable — create one with NewNotices.
type Notices struct {
	mu      sync.Mutex
	pending []Notice
	ready   chan struct{}
}

// NewNotices returns an empty queue, ready to set on RunOptions.Notices.
func NewNotices() *Notices {
	return &Notices{ready: make(chan struct{}, 1)}
}

// Post queues one event. It never blocks; events posted after the loop ended
// are never logged.
func (n *Notices) Post(kind string, payload map[string]any) {
	n.mu.Lock()
	n.pending = append(n.pending, Notice{Kind: kind, Payload: payload})
	n.mu.Unlock()
	select {
	case n.ready <- struct{}{}:
	default:
	}
}

// take returns and clears what is queued.
func (n *Notices) take() []Notice {
	n.mu.Lock()
	defer n.mu.Unlock()
	out := n.pending
	n.pending = nil
	return out
}

// readyCh is nil for a nil queue, so the loop's select never fires on it.
func (n *Notices) readyCh() <-chan struct{} {
	if n == nil {
		return nil
	}
	return n.ready
}
