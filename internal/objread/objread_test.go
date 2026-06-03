package objread

import (
	"context"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/runworktree"
)

type env struct {
	store *objectstore.LocalStore
	refs  *refstore.LocalRefStore
	mgr   *runworktree.Manager
	root  string
	rev   objectstore.ObjectID
	clk   *fakeClock
}

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time          { return c.t }
func (c *fakeClock) advance(d time.Duration) { c.t = c.t.Add(d) }

func newEnv(t *testing.T) env {
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
	clk := &fakeClock{t: time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC)}
	rev, err := store.PutBlob(context.Background(), []byte(`{"kind":"rev"}`))
	if err != nil {
		t.Fatalf("rev: %v", err)
	}
	mgr := runworktree.NewManager(store, refs, root, runworktree.WithClock(clk))
	return env{store: store, refs: refs, mgr: mgr, root: root, rev: rev, clk: clk}
}

// sealOne opens, projects, logs, and seals one execution; returns its id.
func (e env) sealOne(t *testing.T, execID string) {
	t.Helper()
	ctx := context.Background()
	wt, err := e.mgr.Open(ctx, runworktree.OpenInput{ExecutionID: execID, ExecutionKey: "run-1", RevisionID: e.rev, TriggerID: "trg_01X"})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := wt.Project([]runworktree.ProjectedJob{
		{JobID: "svc@deploy", Status: nodes.StatusSucceeded, Steps: []runworktree.ProjectedStep{
			{StepID: "build", Status: nodes.StatusSucceeded},
			{StepID: "test", Status: nodes.StatusSucceeded},
		}},
	}); err != nil {
		t.Fatalf("project: %v", err)
	}
	if err := wt.SetStepLog("svc@deploy", "build", []byte("BUILD LOG")); err != nil {
		t.Fatalf("log: %v", err)
	}
	if _, err := wt.Seal(ctx, nodes.StatusSucceeded, time.Time{}); err != nil {
		t.Fatalf("seal: %v", err)
	}
}

// openLive opens a live (unsealed) execution with one running job + a streamed log.
func (e env) openLive(t *testing.T, execID string) {
	t.Helper()
	ctx := context.Background()
	wt, err := e.mgr.Open(ctx, runworktree.OpenInput{ExecutionID: execID, RevisionID: e.rev})
	if err != nil {
		t.Fatalf("open live: %v", err)
	}
	if err := wt.Project([]runworktree.ProjectedJob{
		{JobID: "api@build", Status: nodes.StatusRunning, Steps: []runworktree.ProjectedStep{
			{StepID: "compile", Status: nodes.StatusRunning},
		}},
	}); err != nil {
		t.Fatalf("project live: %v", err)
	}
	if err := wt.SetStepLog("api@build", "compile", []byte("LIVE LOG")); err != nil {
		t.Fatalf("log live: %v", err)
	}
}

func TestGetSealed(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.sealOne(t, "exec_sealed")
	r := New(e.store, e.refs, e.root)

	v, err := r.Get(ctx, "executions/latest")
	if err != nil {
		t.Fatalf("get latest: %v", err)
	}
	if v.Live {
		t.Fatalf("sealed exec reported live")
	}
	if v.ExecutionID != "exec_sealed" || v.Status != nodes.StatusSucceeded {
		t.Fatalf("header wrong: %+v", v)
	}
	if len(v.Jobs) != 1 || v.Jobs[0].JobID != "svc@deploy" {
		t.Fatalf("jobs wrong: %+v", v.Jobs)
	}
	steps := v.Jobs[0].Attempts[0].Steps
	if len(steps) != 2 || steps[0].StepID != "build" || !steps[0].HasLog {
		t.Fatalf("steps wrong: %+v", steps)
	}
	// Sealed log read via content blob.
	log, err := r.StepLog(ctx, v, "svc@deploy", "build")
	if err != nil || string(log) != "BUILD LOG" {
		t.Fatalf("sealed step log = %q, %v", log, err)
	}
	// A step with no log returns nil.
	if log, err := r.StepLog(ctx, v, "svc@deploy", "test"); err != nil || log != nil {
		t.Fatalf("expected nil log, got %q %v", log, err)
	}
}

func TestResolveVariants(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.sealOne(t, "exec_abc")
	r := New(e.store, e.refs, e.root)

	for _, ref := range []string{"executions/by-id/exec_abc", "exec_abc", ""} {
		v, err := r.Get(ctx, ref)
		if err != nil {
			t.Fatalf("get %q: %v", ref, err)
		}
		if v.ExecutionID != "exec_abc" {
			t.Fatalf("get %q resolved to %q", ref, v.ExecutionID)
		}
	}
	if _, err := r.Get(ctx, "executions/by-id/nope"); err == nil {
		t.Fatalf("expected not-found for missing execution")
	}
}

func TestGetLive(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.openLive(t, "exec_live")
	r := New(e.store, e.refs, e.root)

	v, err := r.Get(ctx, "exec_live")
	if err != nil {
		t.Fatalf("get live: %v", err)
	}
	if !v.Live || v.Status != nodes.StatusRunning {
		t.Fatalf("live header wrong: %+v", v)
	}
	if len(v.Jobs) != 1 || v.Jobs[0].Attempts[0].Steps[0].StepID != "compile" {
		t.Fatalf("live jobs wrong: %+v", v.Jobs)
	}
	log, err := r.StepLog(ctx, v, "api@build", "compile")
	if err != nil || string(log) != "LIVE LOG" {
		t.Fatalf("live step log = %q, %v", log, err)
	}
}

func TestListLiveThenSealed(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.sealOne(t, "exec_old")
	e.clk.advance(time.Hour)
	e.openLive(t, "exec_running")

	r := New(e.store, e.refs, e.root)
	list, err := r.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 executions, got %d: %+v", len(list), list)
	}
	if !list[0].Live || list[0].ExecutionID != "exec_running" {
		t.Fatalf("live run should be first: %+v", list[0])
	}
	if list[1].Live || list[1].ExecutionID != "exec_old" {
		t.Fatalf("sealed run should be second: %+v", list[1])
	}
	// List headers carry summaries but no job detail.
	if list[1].Summary.JobsSucceeded != 1 || list[1].Jobs != nil {
		t.Fatalf("sealed header summary/jobs wrong: %+v", list[1])
	}
}

func TestStepLogUnknownStep(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.sealOne(t, "exec_x")
	r := New(e.store, e.refs, e.root)
	v, _ := r.Get(ctx, "exec_x")
	if _, err := r.StepLog(ctx, v, "svc@deploy", "ghost"); err == nil {
		t.Fatalf("expected not-found for unknown step")
	}
}

func TestLiveSummaryCountsFailures(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	wt, err := e.mgr.Open(ctx, runworktree.OpenInput{ExecutionID: "exec_mixed", RevisionID: e.rev})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := wt.Project([]runworktree.ProjectedJob{
		{JobID: "a", Status: nodes.StatusSucceeded, Steps: []runworktree.ProjectedStep{{StepID: "s1", Status: nodes.StatusSucceeded}}},
		{JobID: "b", Status: nodes.StatusFailed, Steps: []runworktree.ProjectedStep{{StepID: "s2", Status: nodes.StatusFailed}}},
	}); err != nil {
		t.Fatalf("project: %v", err)
	}
	r := New(e.store, e.refs, e.root)
	v, err := r.Get(ctx, "exec_mixed")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if v.Summary.JobsTotal != 2 || v.Summary.JobsSucceeded != 1 || v.Summary.JobsFailed != 1 || v.Summary.StepsTotal != 2 {
		t.Fatalf("live summary wrong: %+v", v.Summary)
	}
}

func TestStepLogByFolder(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	e.sealOne(t, "exec_byfolder")
	r := New(e.store, e.refs, e.root)
	v, err := r.Get(ctx, "exec_byfolder")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	// Look the step log up by the sanitized job folder rather than the job id.
	folder := v.Jobs[0].Folder
	log, err := r.StepLog(ctx, v, folder, "build")
	if err != nil || string(log) != "BUILD LOG" {
		t.Fatalf("step log by folder = %q, %v", log, err)
	}
}

func TestSanitizeIDSegDegenerate(t *testing.T) {
	if got := sanitizeIDSeg("///"); got != "x" {
		t.Fatalf("sanitizeIDSeg(///) = %q", got)
	}
	if got := sanitizeIDSeg("a/b@c"); got != "a-b-c" {
		t.Fatalf("sanitizeIDSeg = %q", got)
	}
}

func TestGetNonExecutionObject(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	// Point an execution ref at a plain blob (not an execution tree).
	if err := e.refs.Update(ctx, "executions/by-id/bogus", "", string(e.rev)); err != nil {
		t.Fatalf("seed ref: %v", err)
	}
	r := New(e.store, e.refs, e.root)
	if _, err := r.Get(ctx, "executions/by-id/bogus"); err == nil {
		t.Fatalf("expected error reading a non-execution object")
	}
	// List skips the unreadable entry rather than failing.
	list, err := r.List(ctx)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	for _, v := range list {
		if v.ExecutionID == "bogus" {
			t.Fatalf("bogus entry should have been skipped")
		}
	}
}

func TestGetTreeWithoutExecutionJSON(t *testing.T) {
	ctx := context.Background()
	e := newEnv(t)
	// A tree that has entries but no execution.json.
	treeID, err := e.store.PutTree(ctx, []objectstore.TreeEntry{
		{Name: "foo.json", Kind: objectstore.KindBlob, ID: e.rev},
	})
	if err != nil {
		t.Fatalf("put tree: %v", err)
	}
	if err := e.refs.Update(ctx, "executions/by-id/notexec", "", string(treeID)); err != nil {
		t.Fatalf("seed ref: %v", err)
	}
	r := New(e.store, e.refs, e.root)
	if _, err := r.Get(ctx, "executions/by-id/notexec"); err == nil {
		t.Fatalf("expected error for tree without execution.json")
	}
}
