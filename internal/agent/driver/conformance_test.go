package driver

import (
	"context"
	"testing"
)

// echoDriver is a second, independent driver whose only job is to prove the
// seam: it emits a message + a done and terminates. If the conformance oracle
// passes echoDriver with no change to internal/agent, "any binary" is real.
type echoDriver struct{}

func (echoDriver) ID() string { return "echo" }

func (echoDriver) Launch(ctx context.Context, b Brief, io IO) (Proc, error) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		io.Events <- Event{Kind: EventMessage, Text: "echo: " + b.ID}
		io.Events <- Event{Kind: EventDone, Fields: map[string]any{"status": "completed"}}
	}()
	return echoProc{done}, nil
}

type echoProc struct{ done chan struct{} }

func (p echoProc) Wait() error { <-p.done; return nil }

func TestConformance_Stub(t *testing.T) {
	rep := CheckConformance(context.Background(), &Stub{}, Brief{ID: "sha256:x", Task: "ORN-1", Branch: "agent/ORN-1"})
	if !rep.OK() {
		t.Fatalf("stub not conformant: %s", rep)
	}
	if !rep.SawDone {
		t.Fatal("stub never emitted EventDone")
	}
}

func TestConformance_SecondDriverPassesUnchanged(t *testing.T) {
	// The whole point of AG4: a different driver passes the identical oracle.
	rep := CheckConformance(context.Background(), echoDriver{}, Brief{ID: "sha256:y"})
	if !rep.OK() {
		t.Fatalf("echo driver not conformant: %s", rep)
	}
	if !rep.SawDone || len(rep.Events) < 2 {
		t.Fatalf("unexpected stream: %+v", rep.Events)
	}
}

// badDriver violates the protocol (nil Proc) — the oracle must catch it, so a
// green conformance result actually means something.
type badDriver struct{}

func (badDriver) ID() string                                      { return "bad" }
func (badDriver) Launch(context.Context, Brief, IO) (Proc, error) { return nil, nil }

func TestConformance_CatchesViolations(t *testing.T) {
	rep := CheckConformance(context.Background(), badDriver{}, Brief{ID: "z"})
	if rep.OK() {
		t.Fatal("oracle passed a nil-Proc driver")
	}
}

func TestRegistryRoundTrip(t *testing.T) {
	if _, err := Get("no-such-driver"); err == nil {
		t.Fatal("Get of an unknown driver should error")
	}
}
