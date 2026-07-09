package agent

import (
	"context"
	"sort"

	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
)

// SessionLog is the append-only session event log (specs/orun-agents/
// data-model.md §3.2). It accumulates events with a monotonic seq and seals
// them into prev-chained AgentSessionSegments. There is no status/lifecycle
// event kind — the honesty invariant is enforced by the closed vocabulary,
// not by the runtime.
type SessionLog struct {
	sessionID string
	seq       int
	pending   []nodes.AgentSessionEvent
	segments  []string // sealed segment ids, in order
	prev      string
	observe   func(SessionEvent)
}

// NewSessionLog starts a log for a session (id must be "as_"-prefixed).
func NewSessionLog(sessionID string) *SessionLog {
	return &SessionLog{sessionID: sessionID}
}

// Observe registers a callback invoked for every appended event (live
// visibility). Nil clears it.
func (l *SessionLog) Observe(fn func(SessionEvent)) { l.observe = fn }

// Append records one event, allocating its seq. kind must be in the closed
// vocabulary (nodes.ValidSessionEventKind); an unknown kind is dropped rather
// than corrupting the log — callers map driver events through EventToSession.
func (l *SessionLog) Append(kind string, payload map[string]any, ref string) {
	if !nodes.ValidSessionEventKind(kind) {
		return
	}
	l.pending = append(l.pending, nodes.AgentSessionEvent{
		Seq:     l.seq,
		Kind:    kind,
		Ref:     ref,
		Payload: payload,
	})
	if l.observe != nil {
		l.observe(SessionEvent{Seq: l.seq, Kind: kind, Payload: payload})
	}
	l.seq++
}

// Len reports the number of events recorded (across sealed + pending).
func (l *SessionLog) Len() int { return l.seq }

// Seal writes the pending events as one AgentSessionSegment (prev-chained to
// the previous), clears the pending buffer, and returns the segment id. A
// no-op when there is nothing pending.
func (l *SessionLog) Seal(ctx context.Context, store objectstore.ObjectStore) (string, error) {
	if len(l.pending) == 0 {
		return "", nil
	}
	from := l.pending[0].Seq
	to := l.pending[len(l.pending)-1].Seq
	seg := nodes.AgentSessionSegment{
		Kind:       nodes.KindAgentSessionSegment,
		APIVersion: "orun.io/v1",
		SessionID:  l.sessionID,
		FromSeq:    from,
		ToSeq:      to,
		Entries:    l.pending,
		Prev:       l.prev,
	}
	id, err := nodes.AssembleAgentSessionSegment(ctx, store, seg)
	if err != nil {
		return "", err
	}
	l.prev = string(id)
	l.segments = append(l.segments, string(id))
	l.pending = nil
	return string(id), nil
}

// Segments returns the sealed segment ids in order.
func (l *SessionLog) Segments() []string { return append([]string(nil), l.segments...) }

// sortStrings is a tiny shared helper (kept here to avoid an extra import in
// brief.go).
func sortStrings(s []string) { sort.Strings(s) }
