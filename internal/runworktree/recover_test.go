package runworktree

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/nodes"
)

func TestRecoverNoDir(t *testing.T) {
	h := newHarness(t)
	res, err := h.m.RecoverStale(context.Background())
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("expected no recovery, got %+v", res)
	}
}

func TestRecoverLeavesFreshAlone(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, WithStaleAfter(time.Hour))
	wt := h.open(t, "exec_alive")
	_ = wt.StartJob("j")
	res, err := h.m.RecoverStale(ctx)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("fresh tree should be left alone, got %+v", res)
	}
	if _, err := os.Stat(wt.Dir()); err != nil {
		t.Fatalf("fresh working tree was removed: %v", err)
	}
}

func TestRecoverMidRunSealsFailed(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, WithStaleAfter(time.Minute))
	wt := h.open(t, "exec_crash")
	_ = wt.StartJob("svc@deploy")
	_ = wt.StartStep("svc@deploy", "build")
	_ = wt.FinishStep("svc@deploy", "build", nodes.StatusSucceeded, 0, []byte("partial"))
	// A second step is left running — the process "crashes" here.
	_ = wt.StartStep("svc@deploy", "test")

	h.clk.advance(2 * time.Minute) // heartbeat goes stale

	res, err := h.m.RecoverStale(ctx)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if len(res) != 1 {
		t.Fatalf("expected 1 recovery, got %+v", res)
	}
	r := res[0]
	if r.ExecutionID != "exec_crash" || r.Status != nodes.StatusFailed || r.WasComplete {
		t.Fatalf("recovery result wrong: %+v", r)
	}

	// The recovered run is a valid sealed execution, status failed, and the
	// still-running leaf step was folded to a terminal status.
	ex := readExecution(t, h.store, r.SealedID)
	if ex.Status != nodes.StatusFailed {
		t.Fatalf("recovered status = %q", ex.Status)
	}
	latest, err := h.refs.Read(ctx, "executions/latest")
	if err != nil || latest.Target != string(r.SealedID) {
		t.Fatalf("executions/latest = %+v, %v", latest, err)
	}
	// Live handle and working tree cleaned up.
	if _, err := h.refs.Read(ctx, liveRefName("exec_crash")); err == nil {
		t.Fatalf("live ref survived recovery")
	}
	if _, err := os.Stat(wt.Dir()); !os.IsNotExist(err) {
		t.Fatalf("working tree survived recovery: %v", err)
	}
}

func TestRecoverCompleteRunIsIdempotentFinish(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, WithStaleAfter(time.Minute))
	wt := h.open(t, "exec_done")
	_ = wt.StartJob("j")
	_ = wt.FinishStep("j", "s", nodes.StatusSucceeded, 0, []byte("ok"))
	_ = wt.FinishJob("j", nodes.StatusSucceeded, "")
	// Simulate a crash that reached terminal status but never sealed/cleaned up:
	// force the snapshot terminal on disk, then leave the tree behind.
	wt.mu.Lock()
	wt.snap.Status = nodes.StatusSucceeded
	ft := h.clk.t
	wt.snap.FinishedAt = &ft
	_ = wt.persist(h.clk.t, "")
	wt.mu.Unlock()

	h.clk.advance(2 * time.Minute)
	res, err := h.m.RecoverStale(ctx)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if len(res) != 1 || res[0].Status != nodes.StatusSucceeded || !res[0].WasComplete {
		t.Fatalf("complete run should seal as succeeded/complete: %+v", res)
	}
	ex := readExecution(t, h.store, res[0].SealedID)
	if ex.Status != nodes.StatusSucceeded {
		t.Fatalf("recovered complete run status = %q", ex.Status)
	}
}

func TestRecoverSkipsGarbledTree(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	// A directory under run/ with no lockfile must be ignored, not crash recovery.
	junk := filepath.Join(h.root, runDir, "not-a-worktree")
	if err := os.MkdirAll(junk, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	res, err := h.m.RecoverStale(ctx)
	if err != nil {
		t.Fatalf("recover: %v", err)
	}
	if len(res) != 0 {
		t.Fatalf("garbled tree should be skipped, got %+v", res)
	}
}
