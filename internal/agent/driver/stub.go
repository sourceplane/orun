package driver

import (
	"context"
	"fmt"
	"strings"
)

// Stub is a deterministic, in-process driver: it emits a fixed event script
// derived from the brief and terminates. It runs the whole runtime loop with
// no external binary, model key, or network — the substrate for the AG2 loop
// tests and a `--driver stub` smoke run. It is NOT the conformance oracle
// (that is AG4); it is the minimal honest implementation of the seam.
//
// With Interactive set, the stub becomes the live plane's CI workhorse
// (orun-agents-live AL0): after its script (minus any terminal event) it
// serves the input channels — echoing steers, requesting approval on
// "/ask <tool>", acknowledging interrupts — until a "/done" steer or ctx
// cancellation ends the session. Deterministic given a deterministic input
// sequence, so interactive runs stay fixture-testable.
type Stub struct {
	// Script, when set, overrides the default event sequence.
	Script []Event
	// Interactive keeps the session open after the script, serving
	// Steer/Approve/Interrupt until a "/done" steer arrives.
	Interactive bool
}

func (s *Stub) ID() string { return "stub" }

func (s *Stub) Launch(ctx context.Context, b Brief, io IO) (Proc, error) {
	events := s.Script
	if events == nil {
		pr := "local://stub/" + b.Branch
		events = []Event{
			{Kind: EventMessage, Text: "reading brief " + b.ID},
			{Kind: EventToolCall, Text: "catalog_affected", Fields: map[string]any{"tool": "catalog_affected"}},
			{Kind: EventToolResult, Text: "affected set resolved", Fields: map[string]any{"tool": "catalog_affected"}},
			{Kind: EventMessage, Text: "implementing " + b.Task},
			{Kind: EventCost, Text: "cost sample", Fields: map[string]any{"tokens": 1234}},
			{Kind: EventArtifact, Text: "opened PR", Fields: map[string]any{"pr": pr, "branch": b.Branch}},
			{Kind: EventDone, Text: "completed", Fields: map[string]any{"status": "completed", "pr": pr, "branch": b.Branch}},
		}
	}
	p := &stubProc{done: make(chan struct{})}
	go func() {
		defer close(p.done)
		for _, e := range events {
			if s.Interactive && e.Kind == EventDone {
				continue // interactive sessions end on "/done", not the script
			}
			select {
			case <-ctx.Done():
				io.Events <- Event{Kind: EventError, Text: "canceled"}
				return
			case io.Events <- e:
			}
		}
		if s.Interactive {
			s.serve(ctx, io)
		}
	}()
	return p, nil
}

// serve is the interactive tail: a deterministic conversational loop over the
// driver IO. Recognized steers:
//
//	"/done"        → emit EventDone (completed) and end the session
//	"/ask <tool>"  → emit EventApproval and block for the verdict
//	anything else  → echo as an agent message
func (s *Stub) serve(ctx context.Context, io IO) {
	askSeq := 0
	for {
		select {
		case <-ctx.Done():
			io.Events <- Event{Kind: EventError, Text: "canceled"}
			return
		case <-io.Interrupt:
			io.Events <- Event{Kind: EventMessage, Text: "turn interrupted"}
		case m := <-io.Steer:
			switch {
			case m.Text == "/done":
				io.Events <- Event{Kind: EventDone, Text: "completed", Fields: map[string]any{"status": "completed"}}
				return
			case strings.HasPrefix(m.Text, "/ask "):
				tool := strings.TrimPrefix(m.Text, "/ask ")
				askSeq++
				req := fmt.Sprintf("req-%d", askSeq)
				io.Events <- Event{Kind: EventApproval, Text: "requesting " + tool, RequestID: req,
					Fields: map[string]any{"tool": tool, "requestId": req}}
				select {
				case <-ctx.Done():
					io.Events <- Event{Kind: EventError, Text: "canceled"}
					return
				case v := <-io.Approve:
					verdict := "denied"
					if v.Approved {
						verdict = "approved"
					}
					io.Events <- Event{Kind: EventMessage, Text: tool + " " + verdict}
				}
			default:
				io.Events <- Event{Kind: EventDelta, Text: "echo: ", Fields: map[string]any{"turn": 1}}
				io.Events <- Event{Kind: EventMessage, Text: "echo: " + m.Text}
			}
		}
	}
}

type stubProc struct{ done chan struct{} }

func (p *stubProc) Wait() error {
	<-p.done
	return nil
}
