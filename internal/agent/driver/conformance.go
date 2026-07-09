package driver

import (
	"context"
	"fmt"
	"time"
)

// conformance.go — the AgentDriver conformance oracle (specs/orun-agents/
// design.md §3, AG4). "Any binary" is a contract, not a hope: a driver must
// drive the full session lifecycle — launch → stream events → accept a
// terminal — over the normalized protocol. Conformance checks the seam, not a
// specific harness, so a second driver (a stub, or a real CLI agent adapter)
// passes it with no change to internal/agent.
//
// The protocol a conforming driver honors:
//   1. Launch is non-blocking; it returns a Proc and streams Events on io.Events.
//   2. Every emission is a normalized Event (the EventKind vocabulary).
//   3. The stream ends with an EventDone (carrying the outcome) OR by the
//      driver closing/stopping such that Proc.Wait returns; the runtime treats
//      both as terminal.
//   4. Proc.Wait blocks until the session is over and reports the terminal
//      error, if any.
//   5. ctx cancellation is honored: a canceled run stops promptly.

// ConformanceReport is the result of running the oracle against a driver.
type ConformanceReport struct {
	Driver     string
	Events     []EventKind
	SawDone    bool
	WaitErr    error
	Violations []string
}

// OK reports whether the driver satisfied every checked property.
func (r ConformanceReport) OK() bool { return len(r.Violations) == 0 }

func (r ConformanceReport) String() string {
	if r.OK() {
		return fmt.Sprintf("driver %q: conformant (%d events)", r.Driver, len(r.Events))
	}
	return fmt.Sprintf("driver %q: %d violation(s): %v", r.Driver, len(r.Violations), r.Violations)
}

// CheckConformance drives d through one full lifecycle for the given brief and
// reports what it observed. It is the reusable oracle: tests (and a CI gate)
// assert report.OK(). It does not import the test package, so any caller —
// including a future real-driver smoke test — can run it.
func CheckConformance(ctx context.Context, d Driver, b Brief) ConformanceReport {
	rep := ConformanceReport{Driver: d.ID()}
	if d.ID() == "" {
		rep.Violations = append(rep.Violations, "driver ID() is empty")
	}

	events := make(chan Event, 16)
	steer := make(chan Message)
	approve := make(chan Verdict, 1)
	io := IO{Events: events, Steer: steer, Approve: approve}

	proc, err := d.Launch(ctx, b, io)
	if err != nil {
		rep.Violations = append(rep.Violations, "Launch returned error: "+err.Error())
		return rep
	}
	if proc == nil {
		rep.Violations = append(rep.Violations, "Launch returned a nil Proc")
		return rep
	}

	waitDone := make(chan error, 1)
	go func() { waitDone <- proc.Wait() }()

	// Consume the event stream with a generous safety deadline so a
	// misbehaving driver that never terminates is reported, not hung.
	timeout := time.After(5 * time.Second)
	draining := true
	for draining {
		select {
		case e, ok := <-events:
			if !ok {
				events = nil
				continue
			}
			if !knownEventKind(e.Kind) {
				rep.Violations = append(rep.Violations, "unknown event kind: "+string(e.Kind))
			}
			rep.Events = append(rep.Events, e.Kind)
			if e.Kind == EventDone {
				rep.SawDone = true
			}
		case werr := <-waitDone:
			rep.WaitErr = werr
			// Drain anything buffered after Wait returned.
			for {
				select {
				case e, ok := <-events:
					if !ok {
						events = nil
						continue
					}
					rep.Events = append(rep.Events, e.Kind)
					if e.Kind == EventDone {
						rep.SawDone = true
					}
					continue
				default:
				}
				break
			}
			draining = false
		case <-timeout:
			rep.Violations = append(rep.Violations, "driver did not terminate within the conformance deadline")
			draining = false
		}
	}

	if len(rep.Events) == 0 {
		rep.Violations = append(rep.Violations, "driver emitted no events")
	}
	return rep
}

func knownEventKind(k EventKind) bool {
	switch k {
	case EventMessage, EventToolCall, EventToolResult, EventApproval,
		EventArtifact, EventCost, EventError, EventDone:
		return true
	default:
		return false
	}
}
