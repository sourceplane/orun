// Package driver is the AgentDriver seam (specs/orun-agents/design.md §3): a
// coding agent (Claude Code first, any binary next) is an executor behind a
// narrow interface, the same swap-discipline as orun's run backends. The
// runtime never gives a driver raw credentials — it hands over a brief and the
// MCP config, and consumes a normalized event stream.
//
// AG2 ships the interface, the registry, the normalized event vocabulary, and
// a deterministic in-process stub driver for the loop tests. The Claude Code
// driver (headless stream-JSON) and the conformance oracle land in AG4.
package driver

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// EventKind is the normalized driver→runtime event vocabulary. It maps onto
// the session event log's closed kinds (nodes.ValidSessionEventKind); a driver
// speaks these regardless of its underlying harness wire format.
type EventKind string

const (
	EventMessage    EventKind = "message" // the agent said something (assistant turn)
	EventToolCall   EventKind = "tool_call"
	EventToolResult EventKind = "tool_result"
	EventApproval   EventKind = "approval_requested" // a gated tool needs a verdict
	EventArtifact   EventKind = "artifact"           // produced output (PR, file)
	EventCost       EventKind = "cost"               // token/cost sample
	EventError      EventKind = "error"
	EventDone       EventKind = "done" // terminal; carries the outcome

	// EventDelta is streaming partial text for the in-progress agent turn.
	// It is WIRE-ONLY (orun-agents-live/attach-protocol.md §4): the runtime
	// relays it to attached heads and never folds it into the session log —
	// the turn's final EventMessage supersedes every delta. Fields may carry
	// "turn" (int) so heads can bind deltas to the turn they stream into.
	EventDelta EventKind = "delta"
)

// Event is one normalized emission from a driver. Text is the human-readable
// payload; Fields carries structured detail (tool name, args digest, PR url,
// token counts). RequestID ties an approval request to its verdict.
type Event struct {
	Kind      EventKind
	Text      string
	Fields    map[string]any
	RequestID string
}

// Message is a user turn injected mid-run (steering).
type Message struct{ Text string }

// Verdict answers an approval request.
type Verdict struct {
	RequestID string
	Approved  bool
	Reason    string
}

// IO is the driver's channels. The driver sends Events and reads Steer/Approve;
// MCPConfigPath points at the tool config the driver hands its agent. Closing
// Events (or emitting EventDone) ends the run.
//
// Interrupt, when non-nil, delivers turn-interrupt requests (the head pressed
// Esc): the driver should stop the current turn — not the session — and keep
// serving Steer. A driver that cannot interrupt mid-turn may ignore the
// channel; the runtime logs the request either way.
type IO struct {
	Events        chan<- Event
	Steer         <-chan Message
	Approve       <-chan Verdict
	Interrupt     <-chan struct{}
	MCPConfigPath string
}

// Brief is the frozen input a driver runs from — the rendered instructions and
// the working directory. It is intentionally decoupled from the nodes.*
// records: a driver needs the prompt and the cwd, not the object graph.
type Brief struct {
	ID           string // the sealed AgentBrief id (for logging/provenance)
	Instructions string // literacy + persona + contract, rendered
	Workdir      string
	Task         string // task key, e.g. ORN-142 (empty for design/interactive)
	Branch       string // task-keyed branch the driver should push
}

// Proc is a running driver session.
type Proc interface {
	// Wait blocks until the session ends and returns the terminal error, if
	// any. Events are delivered on IO.Events until then.
	Wait() error
}

// Driver launches a coding agent for a brief. Launch must be non-blocking:
// it starts the agent and returns a Proc; events flow on io.Events.
type Driver interface {
	ID() string
	Launch(ctx context.Context, b Brief, io IO) (Proc, error)
}

var (
	mu       sync.RWMutex
	registry = map[string]Driver{}
)

// Register makes a driver available by id. Panics on a duplicate id (a
// programming error, like a duplicate cobra command).
func Register(d Driver) {
	mu.Lock()
	defer mu.Unlock()
	if _, dup := registry[d.ID()]; dup {
		panic("agent/driver: duplicate driver id " + d.ID())
	}
	registry[d.ID()] = d
}

// Get returns the driver registered under id.
func Get(id string) (Driver, error) {
	mu.RLock()
	defer mu.RUnlock()
	d, ok := registry[id]
	if !ok {
		return nil, fmt.Errorf("agent/driver: no driver %q (have %v)", id, ids())
	}
	return d, nil
}

// IDs returns the registered driver ids, sorted.
func IDs() []string {
	mu.RLock()
	defer mu.RUnlock()
	return ids()
}

func ids() []string {
	out := make([]string, 0, len(registry))
	for id := range registry {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}
