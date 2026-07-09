package driver

import "context"

// Stub is a deterministic, in-process driver: it emits a fixed event script
// derived from the brief and terminates. It runs the whole runtime loop with
// no external binary, model key, or network — the substrate for the AG2 loop
// tests and a `--driver stub` smoke run. It is NOT the conformance oracle
// (that is AG4); it is the minimal honest implementation of the seam.
type Stub struct {
	// Script, when set, overrides the default event sequence.
	Script []Event
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
			select {
			case <-ctx.Done():
				io.Events <- Event{Kind: EventError, Text: "canceled"}
				return
			case io.Events <- e:
			}
		}
	}()
	return p, nil
}

type stubProc struct{ done chan struct{} }

func (p *stubProc) Wait() error {
	<-p.done
	return nil
}
