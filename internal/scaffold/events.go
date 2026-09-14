package scaffold

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// The build event stream (orun-bootstrap-engine BE-O6).
//
// # Why a stream at all
//
// A bootstrap takes about an hour and a person needs to know what it is doing.
// Today that job belongs to a model relaying a script's output, and orun-cloud's
// own build page records what that costs: its status strip is "the agent's
// latest line, verbatim", and two of its six row states cannot be drawn at all
// because the agent brief instructs the agent to strip the very prefixes that
// carry them.
//
// So the engine reports itself, in two registers that must not be confused:
//
//	detail      the machine's line. High volume, verbatim, for a log.
//	narration   the line the BASELINE authored, one per transition, for a feed.
//
// # The rule that keeps narration honest
//
// **Narration may never set state.** `State` is the truth; narration is the
// caption. A missing narration renders a generated line — never silence, and
// never a claim. Enforced at parse time (a narration string may not contain a
// bare state word outside an expression) and structurally here, because Event's
// State is set by the engine and narration is only ever text.

// EventState is what an event says happened.
type EventState string

const (
	EventStarted EventState = "started"
	EventRunning EventState = "running"
	EventDone    EventState = "done"
	EventFailed  EventState = "failed"
	EventWaiting EventState = "waiting"
	EventSkipped EventState = "skipped"
)

// Event is one thing the engine did.
type Event struct {
	// Schema versions the envelope. A baseline's end-to-end CI asserts against
	// this stream, which makes it an interface whether or not it is called one.
	Schema string `json:"schema" yaml:"schema"`
	RunID  string `json:"runId" yaml:"runId"`
	// Seq is monotonic within a run and is the ordering key — not the
	// timestamp, so a consumer that reconnects can resume from what it holds.
	Seq   int        `json:"seq" yaml:"seq"`
	At    string     `json:"at" yaml:"at"`
	Phase string     `json:"phase,omitempty" yaml:"phase,omitempty"`
	Step  string     `json:"step,omitempty" yaml:"step,omitempty"`
	State EventState `json:"state" yaml:"state"`
	// Narration is the baseline's own words. May be empty.
	Narration string `json:"narration,omitempty" yaml:"narration,omitempty"`
	// Detail is the machine's line. May be empty.
	Detail string `json:"detail,omitempty" yaml:"detail,omitempty"`
	// Meta carries facts a renderer may use — elapsed, counts, ids.
	Meta map[string]string `json:"meta,omitempty" yaml:"meta,omitempty"`
}

// EventSchema is the current envelope version.
const EventSchema = "bootstrap-event/v1"

// EventSink receives events. Nil is valid and means nobody is listening.
type EventSink interface {
	Emit(ctx context.Context, e Event)
}

// emitter stamps schema, run id and sequence so no caller has to.
type emitter struct {
	mu    sync.Mutex
	sink  EventSink
	runID string
	seq   int
	now   func() time.Time
}

func newEmitter(sink EventSink, runID string) *emitter {
	return &emitter{sink: sink, runID: runID, now: time.Now}
}

func (e *emitter) emit(ctx context.Context, ev Event) {
	if e == nil || e.sink == nil {
		return
	}
	e.mu.Lock()
	e.seq++
	ev.Schema = EventSchema
	ev.RunID = e.runID
	ev.Seq = e.seq
	ev.At = e.now().UTC().Format(time.RFC3339)
	e.mu.Unlock()
	e.sink.Emit(ctx, ev)
}

// CollectingSink keeps events in memory — for tests, and for a caller that
// wants the whole stream at the end rather than as it happens.
type CollectingSink struct {
	mu     sync.Mutex
	Events []Event
}

func (c *CollectingSink) Emit(_ context.Context, e Event) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Events = append(c.Events, e)
}

// Narrations returns just the narration lines, in order — what a feed shows.
func (c *CollectingSink) Narrations() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for _, e := range c.Events {
		if e.Narration != "" {
			out = append(out, e.Narration)
		}
	}
	return out
}

// generatedNarration is what a phase with no authored line gets. Never silence
// (which reads as a stalled build) and never a claim beyond the state.
func generatedNarration(phase string, state EventState) string {
	switch state {
	case EventStarted:
		return fmt.Sprintf("Starting %s.", phase)
	case EventDone:
		return fmt.Sprintf("%s is complete.", phase)
	case EventFailed:
		return fmt.Sprintf("%s did not complete.", phase)
	case EventWaiting:
		return fmt.Sprintf("%s is waiting.", phase)
	case EventSkipped:
		return fmt.Sprintf("%s is not needed for this build.", phase)
	}
	return phase
}

// transitionLine composes the "done, summary, now next" line a feed shows
// between phases. The YAML supplies the prose; the engine supplies the numbers;
// neither can lie about the other.
func transitionLine(doneNarration string, facts []string, nextNarration string) string {
	var b strings.Builder
	b.WriteString(doneNarration)
	if len(facts) > 0 {
		b.WriteString("\n    ")
		b.WriteString(strings.Join(facts, " · "))
	}
	if nextNarration != "" {
		b.WriteString("\n→ ")
		b.WriteString(nextNarration)
	}
	return b.String()
}

// runIDOf returns the caller's run id, or derives one from the clock. A derived
// id is still stable within a run, which is all the ordering needs.
func runIDOf(opts Options) string {
	if strings.TrimSpace(opts.RunID) != "" {
		return opts.RunID
	}
	return fmt.Sprintf("run_%d", time.Now().UTC().UnixNano())
}

// containsPhase reports whether a plan includes a phase by name.
func containsPhase(phases []PhasePlan, name string) bool {
	for _, p := range phases {
		if p.Name == name {
			return true
		}
	}
	return false
}

// phaseTitle is what a person calls the phase.
func phaseTitle(decl *Phase, name string) string {
	if decl != nil && strings.TrimSpace(decl.Title) != "" {
		return decl.Title
	}
	return name
}

// NarrateLine returns a phase's authored line for a state, safely on nil.
func (p *Phase) NarrateLine(state EventState) string {
	if p == nil {
		return ""
	}
	return p.Narrate.Line(state)
}

// phaseMeta are the facts a renderer may show beside the prose. The engine
// supplies these; the YAML supplies the words; neither can lie about the other.
func phaseMeta(decl *Phase, files map[string]PlacedFile) map[string]string {
	meta := map[string]string{"files": fmt.Sprint(len(files))}
	if decl != nil && decl.ExpectedMinutes > 0 {
		meta["expectedMinutes"] = fmt.Sprint(decl.ExpectedMinutes)
	}
	return meta
}

// emitHookNarrations emits one event per hook that authored a line.
func emitHookNarrations(ctx context.Context, em *emitter, phase string, groups ...[]Hook) {
	for _, group := range groups {
		for _, h := range group {
			if strings.TrimSpace(h.Narrate) == "" {
				continue
			}
			em.emit(ctx, Event{Phase: phase, Step: h.ID, State: EventDone, Narration: h.Narrate})
		}
	}
}
