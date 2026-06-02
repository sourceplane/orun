package services

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/state"
)

func writeStepLog(t *testing.T, store *state.Store, execID, jobID, stepID, body string) {
	t.Helper()
	path := store.LogPath(execID, jobID, stepID)
	if err := os.MkdirAll(filepathDir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write log: %v", err)
	}
}

// filepathDir is a tiny local helper so the test does not pull filepath into
// scope under a name that collides with production helpers.
func filepathDir(p string) string {
	for i := len(p) - 1; i >= 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}

func TestTailLogs_RequiresExecID(t *testing.T) {
	svc := NewLiveOrunService(LiveServiceConfig{Store: state.NewStore(t.TempDir())})
	if _, err := svc.TailLogs(context.Background(), LogRequest{JobID: "j"}); err == nil {
		t.Fatal("expected ExecID-required error")
	}
}

func TestTailLogs_RejectsRemoteState(t *testing.T) {
	svc := NewLiveOrunService(LiveServiceConfig{Store: state.NewStore(t.TempDir())})
	_, err := svc.TailLogs(context.Background(), LogRequest{ExecID: "e", JobID: "j", RemoteState: true})
	if err == nil {
		t.Fatal("expected remote-state fail-closed error")
	}
}

func TestTailLogs_OneShotReadsExistingLogs(t *testing.T) {
	store := state.NewStore(t.TempDir())
	if _, err := store.CreateExecution("exec-1", &model.Plan{}); err != nil {
		t.Fatalf("create exec: %v", err)
	}
	writeStepLog(t, store, "exec-1", "job-a", "step-1", "line-one\nline-two\n")

	svc := NewLiveOrunService(LiveServiceConfig{Store: store})
	ch, err := svc.TailLogs(context.Background(), LogRequest{
		ExecID: "exec-1", JobID: "job-a", Follow: false,
	})
	if err != nil {
		t.Fatalf("TailLogs: %v", err)
	}

	var lines []string
	for ev := range ch { // one-shot: channel closes on its own
		lines = append(lines, ev.Line)
	}
	if len(lines) != 2 || lines[0] != "line-one" || lines[1] != "line-two" {
		t.Fatalf("unexpected lines: %v", lines)
	}
}

// TestTailLogs_FollowStreamsLateLogs is the regression guard for "logs don't
// show while running": a step log written *after* the tail starts must still
// be delivered, and cancelling the context must close the channel.
func TestTailLogs_FollowStreamsLateLogs(t *testing.T) {
	store := state.NewStore(t.TempDir())
	if _, err := store.CreateExecution("exec-2", &model.Plan{}); err != nil {
		t.Fatalf("create exec: %v", err)
	}

	svc := NewLiveOrunService(LiveServiceConfig{Store: store})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := svc.TailLogs(ctx, LogRequest{ExecID: "exec-2", JobID: "job-b", Follow: true})
	if err != nil {
		t.Fatalf("TailLogs: %v", err)
	}

	// Write the step log only after the follow has started.
	go func() {
		time.Sleep(150 * time.Millisecond)
		writeStepLog(t, store, "exec-2", "job-b", "step-1", "delayed-line\n")
	}()

	select {
	case ev, ok := <-ch:
		if !ok {
			t.Fatal("channel closed before delivering the late log line")
		}
		if ev.Line != "delayed-line" {
			t.Fatalf("got %q, want delayed-line", ev.Line)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for follow to pick up the late log")
	}

	// Cancelling must drain and close the channel.
	cancel()
	select {
	case _, ok := <-ch:
		// Either an already-buffered event then close, or an immediate close.
		if ok {
			// drain until closed
			for range ch {
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("follow channel did not close after cancel")
	}
}
