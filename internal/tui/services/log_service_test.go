package services

import (
	"context"
	"testing"
	"time"
)

func TestTailLogs_RequiresExecID(t *testing.T) {
	svc := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: orunDir(t.TempDir())})
	if _, err := svc.TailLogs(context.Background(), LogRequest{JobID: "j"}); err == nil {
		t.Fatal("expected ExecID-required error")
	}
}

func TestTailLogs_RejectsRemoteState(t *testing.T) {
	svc := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: orunDir(t.TempDir())})
	_, err := svc.TailLogs(context.Background(), LogRequest{ExecID: "e", JobID: "j", RemoteState: true})
	if err == nil {
		t.Fatal("expected remote-state fail-closed error")
	}
}

// TestTailLogs_OneShotReadsSealedLogs reads a completed run's step logs from the
// sealed content blobs.
func TestTailLogs_OneShotReadsSealedLogs(t *testing.T) {
	dir := t.TempDir()
	seedObjectExecution(t, dir, seedExec{
		ExecID:   "exec-1",
		PlanName: "demo",
		Jobs: []seedJob{
			{ID: "job-a", Component: "a", Steps: []seedStep{
				{ID: "step-1", Log: "line-one\nline-two\n"},
			}},
		},
	})

	svc := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: orunDir(dir)})
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

// TestTailLogs_OneShotStepFilter restricts the one-shot read to a single step.
func TestTailLogs_OneShotStepFilter(t *testing.T) {
	dir := t.TempDir()
	seedObjectExecution(t, dir, seedExec{
		ExecID: "exec-f",
		Jobs: []seedJob{
			{ID: "job-a", Component: "a", Steps: []seedStep{
				{ID: "build", Log: "BUILD\n"},
				{ID: "test", Log: "TEST\n"},
			}},
		},
	})

	svc := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: orunDir(dir)})
	ch, err := svc.TailLogs(context.Background(), LogRequest{ExecID: "exec-f", JobID: "job-a", StepID: "test"})
	if err != nil {
		t.Fatalf("TailLogs: %v", err)
	}
	var lines []string
	for ev := range ch {
		lines = append(lines, ev.Line)
	}
	if len(lines) != 1 || lines[0] != "TEST" {
		t.Fatalf("step filter failed: %v", lines)
	}
}

// TestTailLogs_FollowSupportedWhenAbsent verifies follow returns a live channel
// even before the working tree appears, and that cancelling closes it.
func TestTailLogs_FollowSupportedWhenAbsent(t *testing.T) {
	svc := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: orunDir(t.TempDir())})
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := svc.TailLogs(ctx, LogRequest{ExecID: "e", JobID: "j", Follow: true})
	if err != nil {
		t.Fatalf("Follow=true should be supported, got %v", err)
	}
	cancel()
	for range ch { // must terminate once the context is cancelled
	}
}

// TestTailLogs_FollowStreamsLateLogs is the regression guard for "logs don't
// show while running": a step log streamed into the live working tree *after*
// the tail starts must still be delivered, and cancelling must close the
// channel.
func TestTailLogs_FollowStreamsLateLogs(t *testing.T) {
	dir := t.TempDir()
	wt := seedLiveWorkTree(t, dir, seedExec{
		ExecID: "exec-2",
		Jobs: []seedJob{
			{ID: "job-b", Component: "b", Steps: []seedStep{{ID: "step-1"}}},
		},
	})

	svc := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: orunDir(dir)})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ch, err := svc.TailLogs(ctx, LogRequest{ExecID: "exec-2", JobID: "job-b", Follow: true})
	if err != nil {
		t.Fatalf("TailLogs: %v", err)
	}

	// Stream the step log only after the follow has started; SetStepLog both
	// writes the file and re-persists the snapshot with the LogFile set, so the
	// next follow tick discovers it.
	go func() {
		time.Sleep(150 * time.Millisecond)
		_ = wt.SetStepLog("job-b", "step-1", []byte("delayed-line\n"))
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
		if ok {
			for range ch {
			}
		}
	case <-time.After(2 * time.Second):
		t.Fatal("follow channel did not close after cancel")
	}
}
