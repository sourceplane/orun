package approval

import (
	"context"
	"testing"
	"time"
)

func TestAskDecideAwait(t *testing.T) {
	ws := t.TempDir()
	dir, err := Ask(ws, Request{Prompt: "ship it?", ExecID: "e1", JobID: "web@prod.deploy", StepID: "promote", RequestedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}

	pending, err := Pending(ws)
	if err != nil || len(pending) != 1 || pending[0].Prompt != "ship it?" {
		t.Fatalf("pending listing wrong: %v %v", pending, err)
	}

	// Decide concurrently while Await polls.
	go func() {
		time.Sleep(50 * time.Millisecond)
		if derr := Decide(ws, "web@prod.deploy", "promote", true, "sam"); derr != nil {
			t.Error(derr)
		}
	}()
	dec, err := Await(context.Background(), dir, 5*time.Second, 10*time.Millisecond)
	if err != nil || !dec.Approved || dec.By != "sam" {
		t.Fatalf("await: %+v %v", dec, err)
	}

	// Decided gates leave the pending list.
	pending, _ = Pending(ws)
	if len(pending) != 0 {
		t.Fatalf("decided gate still pending: %v", pending)
	}
}

func TestAwaitTimeout(t *testing.T) {
	ws := t.TempDir()
	dir, err := Ask(ws, Request{ExecID: "e1", JobID: "j", StepID: "s", RequestedAt: time.Now().UTC()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Await(context.Background(), dir, 30*time.Millisecond, 10*time.Millisecond); err != ErrTimeout {
		t.Fatalf("expected ErrTimeout, got %v", err)
	}
}

func TestDecideRejectAndNoPending(t *testing.T) {
	ws := t.TempDir()
	if _, err := Ask(ws, Request{ExecID: "e1", JobID: "j", StepID: "s", RequestedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if err := Decide(ws, "j", "s", false, "sam"); err != nil {
		t.Fatal(err)
	}
	if err := Decide(ws, "j", "absent", true, ""); err == nil {
		t.Fatalf("deciding a non-pending gate must error")
	}
}
