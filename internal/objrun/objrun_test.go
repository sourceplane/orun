package objrun

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/runner"
	"github.com/sourceplane/orun/internal/runworktree"
)

func countObjectFiles(t *testing.T, dir string) int {
	t.Helper()
	n := 0
	_ = filepath.WalkDir(dir, func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			n++
		}
		return nil
	})
	return n
}

func TestBeginNoopOnEmptyInput(t *testing.T) {
	root := t.TempDir()
	if s, err := Begin(context.Background(), root, nil, "x"); s != nil || err != nil {
		t.Fatalf("Begin(nil plan) = %v, %v; want nil, nil", s, err)
	}
	plan := &model.Plan{Metadata: model.PlanMetadata{Name: "demo"}}
	if s, err := Begin(context.Background(), root, plan, ""); s != nil || err != nil {
		t.Fatalf("Begin(empty execID) = %v, %v; want nil, nil", s, err)
	}
	// Nil session methods are no-ops.
	var nilSess *Session
	nilSess.InstallHooks(&runner.Runner{})
	if id, err := nilSess.Finish(context.Background(), &runner.Runner{}, nil); id != "" || err != nil {
		t.Fatalf("nil Finish = %q, %v; want \"\", nil", id, err)
	}
	if nilSess.RevisionID() != "" {
		t.Fatalf("nil RevisionID should be empty")
	}
}

func TestLivePathSeals(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	execID := "exec-live-1"
	plan := &model.Plan{Metadata: model.PlanMetadata{Name: "demo"}}

	sess, err := Begin(ctx, root, plan, execID)
	if err != nil {
		t.Fatalf("Begin: %v", err)
	}
	if sess == nil {
		t.Fatalf("Begin returned nil session")
	}

	// A live working tree + in-flight handle exist mid-run.
	if _, err := os.Stat(filepath.Join(root, "run")); err != nil {
		t.Fatalf("working tree dir missing: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "refs", "executions", "live")); err != nil {
		t.Fatalf("live ref missing: %v", err)
	}

	// Install hooks on a bare runner; AfterStateUpdate is a safe no-op without
	// live state, so drive the job/step state into the working tree directly.
	r := &runner.Runner{}
	sess.InstallHooks(r)
	if r.Hooks == nil || r.Hooks.AfterStateUpdate == nil || r.Hooks.AfterStepLog == nil {
		t.Fatalf("hooks not installed")
	}
	r.Hooks.AfterStateUpdate() // no-op (no live state) — must not panic
	if err := sess.wt.Project([]runworktree.ProjectedJob{
		{JobID: "api@deploy", Status: nodes.StatusSucceeded, Steps: []runworktree.ProjectedStep{{StepID: "build", Status: nodes.StatusSucceeded}}},
	}); err != nil {
		t.Fatalf("project: %v", err)
	}
	r.Hooks.AfterStepLog("api@deploy", "build", "build output\n")

	id, err := sess.Finish(ctx, r, nil)
	if err != nil {
		t.Fatalf("Finish: %v", err)
	}
	if id == "" {
		t.Fatalf("Finish returned empty sealed id")
	}

	// Sealed: objects + executions/latest written; working tree + live handle gone.
	if n := countObjectFiles(t, filepath.Join(root, "objects")); n == 0 {
		t.Fatalf("no objects written")
	}
	if _, err := os.Stat(filepath.Join(root, "refs", "executions", "latest.json")); err != nil {
		t.Fatalf("executions/latest not published: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "run")); len(entries) != 0 {
		t.Fatalf("working tree survived seal: %d entries", len(entries))
	}
	if _, err := os.Stat(filepath.Join(root, "refs", "executions", "live", "exec-live-1.json")); !os.IsNotExist(err) {
		t.Fatalf("live handle survived seal")
	}
}

func TestSealImportsTerminalRun(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	plan := &model.Plan{Metadata: model.PlanMetadata{Name: "demo"}, Jobs: []model.PlanJob{{ID: "build"}}}

	id, err := Seal(ctx, root, plan, ImportInput{
		ExecID: "gh-7-1-abc",
		Status: "completed", // source vocabulary, folded to succeeded
		Jobs: []SealJob{{
			JobID:  "build",
			Status: "success",
			Steps: []SealStep{
				{StepID: "compile", Status: "success", Log: []byte("compiling\n")},
				{StepID: "test", Status: "skipped"},
			},
		}},
		Links: []SealLink{{Label: "GitHub Actions", URL: "https://example/run/7"}},
	})
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if id == "" {
		t.Fatalf("Seal returned empty id")
	}

	// Published under both executions/latest and executions/by-id/<id>; the
	// live working tree is gone.
	if _, err := os.Stat(filepath.Join(root, "refs", "executions", "latest.json")); err != nil {
		t.Fatalf("executions/latest not published: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "refs", "executions", "by-id", "gh-7-1-abc.json")); err != nil {
		t.Fatalf("executions/by-id not published: %v", err)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "run")); len(entries) != 0 {
		t.Fatalf("working tree survived seal: %d entries", len(entries))
	}

	// Re-importing the same run succeeds (no overwrite protection to trip over).
	if _, err := Seal(ctx, root, plan, ImportInput{
		ExecID: "gh-7-1-abc", Status: "completed",
		Jobs: []SealJob{{JobID: "build", Status: "success"}},
	}); err != nil {
		t.Fatalf("re-Seal: %v", err)
	}
}

func TestSealRequiresExecIDAndPlan(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	if _, err := Seal(ctx, root, nil, ImportInput{ExecID: "x"}); err == nil {
		t.Fatal("expected error for nil plan")
	}
	plan := &model.Plan{Metadata: model.PlanMetadata{Name: "demo"}}
	if _, err := Seal(ctx, root, plan, ImportInput{}); err == nil {
		t.Fatal("expected error for empty exec id")
	}
}

func TestProjectFromExecStateSorted(t *testing.T) {
	st := &execmodel.ExecState{Jobs: map[string]*execmodel.JobState{
		"b@deploy": {Status: "running", Steps: map[string]string{"z": "running", "a": "success"}},
		"a@deploy": {Status: "success", Steps: map[string]string{"build": "success"}},
	}}
	out := projectFromExecState(st)
	if len(out) != 2 || out[0].JobID != "a@deploy" || out[1].JobID != "b@deploy" {
		t.Fatalf("jobs not sorted: %+v", out)
	}
	// Steps within a job are sorted too.
	if out[1].Steps[0].StepID != "a" || out[1].Steps[1].StepID != "z" {
		t.Fatalf("steps not sorted: %+v", out[1].Steps)
	}
	// Status mapping folded onto the node vocabulary.
	if out[0].Status != "succeeded" || out[1].Status != "running" {
		t.Fatalf("status mapping wrong: %+v", out)
	}
}

func TestPlanHashStableAndComponentInvariant(t *testing.T) {
	plan := &model.Plan{Metadata: model.PlanMetadata{Name: "demo"}}
	h1, err := PlanHash(plan)
	if err != nil {
		t.Fatalf("PlanHash: %v", err)
	}
	// Self-referential metadata must not change the hash.
	plan.Metadata.Checksum = "deadbeef"
	h2, err := PlanHash(plan)
	if err != nil {
		t.Fatalf("PlanHash: %v", err)
	}
	if h1 != h2 {
		t.Fatalf("plan hash not stable across checksum: %s vs %s", h1, h2)
	}
}
