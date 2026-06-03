package runworktree

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// harness builds a Manager over a fresh on-disk object/ref store with a
// controllable clock, and returns a real revision id to attach executions to.
type harness struct {
	m     *Manager
	store *objectstore.LocalStore
	refs  *refstore.LocalRefStore
	clk   *fakeClock
	root  string
	rev   objectstore.ObjectID
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newHarness(t *testing.T, opts ...Option) harness {
	t.Helper()
	root := t.TempDir()
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Clock: clock.Fixed{}})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	clk := &fakeClock{t: time.Date(2026, 6, 3, 12, 0, 0, 0, time.UTC)}
	rev, err := store.PutBlob(context.Background(), []byte(`{"kind":"PlanRevision-stub"}`))
	if err != nil {
		t.Fatalf("rev blob: %v", err)
	}
	all := append([]Option{WithClock(clk)}, opts...)
	m := NewManager(store, refs, root, all...)
	return harness{m: m, store: store, refs: refs, clk: clk, root: root, rev: rev}
}

func (h harness) open(t *testing.T, execID string) *WorkTree {
	t.Helper()
	wt, err := h.m.Open(context.Background(), OpenInput{
		ExecutionID:  execID,
		ExecutionKey: "run-001",
		RevisionID:   h.rev,
		TriggerID:    "trg_01TEST",
	})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	return wt
}

// readExecution loads and decodes the execution.json from a sealed execution
// tree root.
func readExecution(t *testing.T, s objectstore.ObjectStore, root objectstore.ObjectID) nodes.ExecutionRun {
	t.Helper()
	entries, err := s.GetTree(context.Background(), root)
	if err != nil {
		t.Fatalf("get exec tree: %v", err)
	}
	for _, e := range entries {
		if e.Name == "execution.json" {
			_, body, err := s.Get(context.Background(), e.ID)
			if err != nil {
				t.Fatalf("get execution.json: %v", err)
			}
			ex, err := nodes.Decode[nodes.ExecutionRun](body)
			if err != nil {
				t.Fatalf("decode execution: %v", err)
			}
			return ex
		}
	}
	t.Fatalf("execution.json not found in tree %s", root)
	return nodes.ExecutionRun{}
}

func TestOpenRunSeal(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	wt := h.open(t, "exec_full")

	// The working dir, snapshot, lock, and live ref all exist.
	if _, err := os.Stat(filepath.Join(wt.Dir(), snapshotFile)); err != nil {
		t.Fatalf("snapshot missing: %v", err)
	}
	if r, err := h.refs.Read(ctx, liveRefName("exec_full")); err != nil || r.Target != string(h.rev) {
		t.Fatalf("live ref = %+v, %v", r, err)
	}

	if err := wt.StartJob("svc0@deploy"); err != nil {
		t.Fatalf("start job: %v", err)
	}
	if err := wt.StartStep("svc0@deploy", "build"); err != nil {
		t.Fatalf("start step: %v", err)
	}
	if err := wt.FinishStep("svc0@deploy", "build", nodes.StatusSucceeded, 0, []byte("build ok\n")); err != nil {
		t.Fatalf("finish step: %v", err)
	}
	if err := wt.FinishStep("svc0@deploy", "test", nodes.StatusSucceeded, 0, []byte("tests ok\n")); err != nil {
		t.Fatalf("finish step 2: %v", err)
	}
	if err := wt.FinishJob("svc0@deploy", nodes.StatusSucceeded, ""); err != nil {
		t.Fatalf("finish job: %v", err)
	}

	id, err := wt.Seal(ctx, nodes.StatusSucceeded, time.Time{})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	// Published refs both point at the sealed id.
	latest, err := h.refs.Read(ctx, "executions/latest")
	if err != nil || latest.Target != string(id) {
		t.Fatalf("executions/latest = %+v, %v", latest, err)
	}
	byID, err := h.refs.Read(ctx, "executions/by-id/exec_full")
	if err != nil || byID.Target != string(id) {
		t.Fatalf("executions/by-id = %+v, %v", byID, err)
	}

	// Live handle and working tree are gone after seal.
	if _, err := h.refs.Read(ctx, liveRefName("exec_full")); err == nil {
		t.Fatalf("live ref survived seal")
	}
	if _, err := os.Stat(wt.Dir()); !os.IsNotExist(err) {
		t.Fatalf("working tree survived seal: %v", err)
	}

	// The sealed execution is correct.
	ex := readExecution(t, h.store, id)
	if ex.Status != nodes.StatusSucceeded {
		t.Fatalf("sealed status = %q", ex.Status)
	}
	if ex.ExecutionID != "exec_full" || ex.RevisionID != string(h.rev) || ex.TriggerID != "trg_01TEST" {
		t.Fatalf("sealed attribution wrong: %+v", ex)
	}
	if ex.Summary.JobsTotal != 1 || ex.Summary.JobsSucceeded != 1 || ex.Summary.StepsTotal != 2 {
		t.Fatalf("summary = %+v", ex.Summary)
	}
	if ex.FinishedAt == nil {
		t.Fatalf("finishedAt not set")
	}
}

func TestSealIsContentAddressed(t *testing.T) {
	ctx := context.Background()
	seal := func(execID string) objectstore.ObjectID {
		h := newHarness(t)
		wt := h.open(t, execID)
		_ = wt.StartJob("j")
		_ = wt.FinishStep("j", "s", nodes.StatusSucceeded, 0, []byte("same"))
		_ = wt.FinishJob("j", nodes.StatusSucceeded, "")
		// Pin times so the two runs are byte-identical content.
		start := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
		wt.snap.StartedAt = start
		for i := range wt.snap.Jobs {
			wt.snap.Jobs[i].StartedAt = nil
			wt.snap.Jobs[i].FinishedAt = nil
			for k := range wt.snap.Jobs[i].Attempts {
				wt.snap.Jobs[i].Attempts[k].StartedAt = nil
				wt.snap.Jobs[i].Attempts[k].FinishedAt = nil
				for s := range wt.snap.Jobs[i].Attempts[k].Steps {
					wt.snap.Jobs[i].Attempts[k].Steps[s].StartedAt = nil
					wt.snap.Jobs[i].Attempts[k].Steps[s].FinishedAt = nil
				}
			}
		}
		id, err := wt.Seal(ctx, nodes.StatusSucceeded, start)
		if err != nil {
			t.Fatalf("seal: %v", err)
		}
		return id
	}
	// Same execution id + identical content ⇒ identical sealed id.
	if a, b := seal("exec_same"), seal("exec_same"); a != b {
		t.Fatalf("identical content sealed to different ids: %s vs %s", a, b)
	}
}

func TestOpenConflictWhenLive(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	_ = h.open(t, "exec_dup")
	_, err := h.m.Open(ctx, OpenInput{ExecutionID: "exec_dup", RevisionID: h.rev})
	if !isConflict(err) {
		t.Fatalf("expected conflict reopening a live execution, got %v", err)
	}
}

func TestOpenReclaimsStale(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, WithStaleAfter(time.Minute))
	first := h.open(t, "exec_stale")
	_ = first.StartJob("j")

	h.clk.advance(2 * time.Minute) // heartbeat goes stale
	wt, err := h.m.Open(ctx, OpenInput{ExecutionID: "exec_stale", RevisionID: h.rev})
	if err != nil {
		t.Fatalf("reopen stale: %v", err)
	}
	// Reclaimed: a fresh snapshot with no jobs.
	if got := wt.Snapshot(); len(got.Jobs) != 0 {
		t.Fatalf("reclaimed tree not fresh: %+v", got.Jobs)
	}
}

func TestOpenMintsID(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t, WithExecIDGen(func() string { return "exec_minted" }))
	wt, err := h.m.Open(ctx, OpenInput{RevisionID: h.rev})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if wt.ExecutionID() != "exec_minted" {
		t.Fatalf("exec id = %q", wt.ExecutionID())
	}
}

func TestOpenRejectsBadRevision(t *testing.T) {
	h := newHarness(t)
	if _, err := h.m.Open(context.Background(), OpenInput{ExecutionID: "x", RevisionID: "not-an-id"}); err == nil {
		t.Fatalf("expected error for malformed revision id")
	}
}

func TestNonTerminalGuards(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	wt := h.open(t, "exec_guard")
	_ = wt.StartJob("j")
	if err := wt.FinishStep("j", "s", nodes.StatusRunning, 0, nil); err == nil {
		t.Fatalf("FinishStep accepted non-terminal status")
	}
	if err := wt.FinishJob("j", nodes.StatusRunning, ""); err == nil {
		t.Fatalf("FinishJob accepted non-terminal status")
	}
	if _, err := wt.Seal(ctx, nodes.StatusRunning, time.Time{}); err == nil {
		t.Fatalf("Seal accepted non-terminal status")
	}
}

func TestDoubleSeal(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	wt := h.open(t, "exec_twice")
	_ = wt.StartJob("j")
	_ = wt.FinishJob("j", nodes.StatusSucceeded, "")
	if _, err := wt.Seal(ctx, nodes.StatusSucceeded, time.Time{}); err != nil {
		t.Fatalf("first seal: %v", err)
	}
	if _, err := wt.Seal(ctx, nodes.StatusSucceeded, time.Time{}); !isConflict(err) {
		t.Fatalf("second seal should conflict, got %v", err)
	}
}

func TestRetryAddsAttempt(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	wt := h.open(t, "exec_retry")
	_ = wt.StartJob("j")
	_ = wt.FinishStep("j", "s", nodes.StatusFailed, 1, []byte("boom"))
	if err := wt.StartAttempt("j"); err != nil {
		t.Fatalf("start attempt: %v", err)
	}
	_ = wt.FinishStep("j", "s", nodes.StatusSucceeded, 0, []byte("ok"))
	_ = wt.FinishJob("j", nodes.StatusSucceeded, "")
	id, err := wt.Seal(ctx, nodes.StatusSucceeded, time.Time{})
	if err != nil {
		t.Fatalf("seal: %v", err)
	}
	ex := readExecution(t, h.store, id)
	// Two attempts under the one job folder.
	var folder string
	for f := range ex.JobIDs {
		folder = f
	}
	jobTree, err := h.store.GetTree(ctx, objectstore.ObjectID(ex.JobIDs[folder]))
	if err != nil {
		t.Fatalf("job tree: %v", err)
	}
	for _, e := range jobTree {
		if e.Name == "attempts" {
			atts, _ := h.store.GetTree(ctx, e.ID)
			if len(atts) != 2 {
				t.Fatalf("expected 2 attempts, got %d", len(atts))
			}
		}
	}
}

func TestHeartbeatAndLinkAndSnapshot(t *testing.T) {
	h := newHarness(t)
	wt := h.open(t, "exec_hb")
	_ = wt.StartJob("j")
	h.clk.advance(30 * time.Second)
	if err := wt.Heartbeat("j"); err != nil {
		t.Fatalf("heartbeat: %v", err)
	}
	lock, err := readLock(wt.Dir())
	if err != nil {
		t.Fatalf("read lock: %v", err)
	}
	if lock.CurrentJob != "j" || !lock.LastHeartbeat.Equal(h.clk.t) {
		t.Fatalf("lock not updated: %+v", lock)
	}
	if err := wt.AddLink(nodes.ExecLink{Label: "CI", URL: "https://ci/1"}); err != nil {
		t.Fatalf("add link: %v", err)
	}
	snap := wt.Snapshot()
	if len(snap.Links) != 1 || snap.Links[0].Label != "CI" {
		t.Fatalf("links = %+v", snap.Links)
	}
	// Snapshot is a copy: mutating it does not affect the live tree.
	snap.Jobs[0].Status = "mutated"
	if wt.Snapshot().Jobs[0].Status == "mutated" {
		t.Fatalf("Snapshot did not return a copy")
	}
}

func TestSanitizeAndFolder(t *testing.T) {
	if got := sanitizeName("ns/repo@deploy.step"); got != "ns-repo-deploy.step" {
		t.Fatalf("sanitizeName = %q", got)
	}
	if got := sanitizeName("///"); got != "x" {
		t.Fatalf("degenerate sanitizeName = %q", got)
	}
	// jobFolder is deterministic and prefixed.
	if jobFolder("a") != jobFolder("a") || jobFolder("a") == jobFolder("b") {
		t.Fatalf("jobFolder not a stable function of input")
	}
	if got := jobFolder("a"); len(got) != 10 || got[:2] != "j-" {
		t.Fatalf("jobFolder shape = %q", got)
	}
}

func TestOpenWithDegenerateExecID(t *testing.T) {
	ctx := context.Background()
	h := newHarness(t)
	wt, err := h.m.Open(ctx, OpenInput{ExecutionID: "///", RevisionID: h.rev})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	// The dir name is sanitized to "x".
	if filepath.Base(wt.Dir()) != "x" {
		t.Fatalf("dir base = %q", filepath.Base(wt.Dir()))
	}
}
