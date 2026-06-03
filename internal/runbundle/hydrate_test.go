package runbundle

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objread"
)

func TestHydrate_BasicHydration(t *testing.T) {
	ctx := context.Background()
	orunDir := filepath.Join(t.TempDir(), ".orun")

	planShard := planShardWithPlan(t, "gh-1-1-abc", "abc123", []string{"job1", "job2"})
	jobShards := []*JobShard{
		jobShardWithState("gh-1-1-abc", "abc123", "uid-job1", "job1", "completed"),
		jobShardWithState("gh-1-1-abc", "abc123", "uid-job2", "job2", "completed"),
	}

	result, err := Hydrate(ctx, planShard, jobShards, HydrateOptions{
		ExecID: "gh-1-1-abc",
		Source: ShardSource{Type: "test"},
	}, orunDir)
	if err != nil {
		t.Fatalf("Hydrate failed: %v", err)
	}

	if result.ExecID != "gh-1-1-abc" {
		t.Errorf("ExecID = %q, want %q", result.ExecID, "gh-1-1-abc")
	}
	if result.JobCount != 2 {
		t.Errorf("JobCount = %d, want 2", result.JobCount)
	}
	if result.RevisionID == "" {
		t.Error("RevisionID is empty; expected a sealed object id")
	}

	// The imported run must be readable from the object graph.
	view := getExecution(t, orunDir, "gh-1-1-abc")
	if view.Status != "succeeded" {
		t.Errorf("execution status = %q, want %q", view.Status, "succeeded")
	}
	if len(view.Jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(view.Jobs))
	}
	for _, j := range view.Jobs {
		if j.Status != "succeeded" {
			t.Errorf("job %s status = %q, want %q", j.JobID, j.Status, "succeeded")
		}
	}
}

func TestHydrate_WithLogs(t *testing.T) {
	ctx := context.Background()
	orunDir := filepath.Join(t.TempDir(), ".orun")
	shardDir := filepath.Join(t.TempDir(), "job-shard")

	// Create a job shard with per-step logs on disk (logs/<stepId>.log).
	logsDir := filepath.Join(shardDir, "logs")
	if err := os.MkdirAll(logsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "step1.log"), []byte("step1 output"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logsDir, "step2.log"), []byte("step2 output"), 0o644); err != nil {
		t.Fatal(err)
	}

	js := &execmodel.JobState{
		Status:     "completed",
		StartedAt:  "2026-05-23T12:00:00Z",
		FinishedAt: "2026-05-23T12:05:00Z",
		Steps:      map[string]string{"step1": "completed", "step2": "completed"},
	}
	jobShards := []*JobShard{{
		Dir: shardDir,
		Manifest: &RunBundleShardManifest{
			Role:   ShardRoleJob,
			ExecID: "gh-1-1-abc",
			PlanID: "abc123",
			JobUID: "uid-job1",
			JobID:  "job1",
			Status: "completed",
			Files: map[string]string{
				"log:step1": "logs/step1.log",
				"log:step2": "logs/step2.log",
			},
		},
		JobState: js,
	}}

	planShard := planShardWithPlan(t, "gh-1-1-abc", "abc123", []string{"job1"})
	result, err := Hydrate(ctx, planShard, jobShards, HydrateOptions{
		ExecID: "gh-1-1-abc",
		Source: ShardSource{Type: "test"},
	}, orunDir)
	if err != nil {
		t.Fatalf("Hydrate failed: %v", err)
	}
	if result.LogFiles != 2 {
		t.Errorf("LogFiles = %d, want 2", result.LogFiles)
	}

	// The logs must be readable as content blobs off the sealed execution.
	r, _ := openReader(t, orunDir)
	view := getExecution(t, orunDir, "gh-1-1-abc")
	for stepID, want := range map[string]string{"step1": "step1 output", "step2": "step2 output"} {
		got, err := r.StepLog(ctx, view, "job1", stepID)
		if err != nil {
			t.Fatalf("StepLog(%s): %v", stepID, err)
		}
		if string(got) != want {
			t.Errorf("StepLog(%s) = %q, want %q", stepID, got, want)
		}
	}
}

func TestHydrate_Idempotent(t *testing.T) {
	ctx := context.Background()
	orunDir := filepath.Join(t.TempDir(), ".orun")

	planShard := planShardWithPlan(t, "gh-1-1-abc", "abc123", []string{"job1"})
	jobShards := []*JobShard{
		jobShardWithState("gh-1-1-abc", "abc123", "uid-job1", "job1", "completed"),
	}

	// Re-importing the same run must succeed (the object graph is append-only /
	// idempotent — no overwrite protection to trip over) and stay readable.
	for i := 0; i < 2; i++ {
		if _, err := Hydrate(ctx, planShard, jobShards, HydrateOptions{
			ExecID: "gh-1-1-abc",
			Source: ShardSource{Type: "test"},
		}, orunDir); err != nil {
			t.Fatalf("Hydrate #%d failed: %v", i+1, err)
		}
	}
	view := getExecution(t, orunDir, "gh-1-1-abc")
	if len(view.Jobs) != 1 {
		t.Errorf("jobs = %d, want 1", len(view.Jobs))
	}
}

func TestHydrate_ReadableViaObjectGraph(t *testing.T) {
	ctx := context.Background()
	orunDir := filepath.Join(t.TempDir(), ".orun")

	planShard := planShardWithPlan(t, "gh-readable-1-abc", "abc123", []string{"job1", "job2"})
	jobShards := []*JobShard{
		jobShardWithState("gh-readable-1-abc", "abc123", "uid-job1", "job1", "completed"),
		jobShardWithState("gh-readable-1-abc", "abc123", "uid-job2", "job2", "failed"),
	}

	if _, err := Hydrate(ctx, planShard, jobShards, HydrateOptions{
		ExecID: "gh-readable-1-abc",
		Source: ShardSource{Type: "test"},
	}, orunDir); err != nil {
		t.Fatalf("Hydrate failed: %v", err)
	}

	view := getExecution(t, orunDir, "gh-readable-1-abc")
	// One job failed → the execution folds to a failed terminal status.
	if view.Status != "failed" {
		t.Errorf("execution status = %q, want %q", view.Status, "failed")
	}
	byID := map[string]string{}
	for _, j := range view.Jobs {
		byID[j.JobID] = j.Status
	}
	if byID["job1"] != "succeeded" {
		t.Errorf("job1 status = %q, want %q", byID["job1"], "succeeded")
	}
	if byID["job2"] != "failed" {
		t.Errorf("job2 status = %q, want %q", byID["job2"], "failed")
	}
}

func TestHydrate_NilPlanShard(t *testing.T) {
	ctx := context.Background()
	_, err := Hydrate(ctx, nil, nil, HydrateOptions{ExecID: "test"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for nil plan shard")
	}
}

func TestHydrate_EmptyExecID(t *testing.T) {
	ctx := context.Background()
	planShard := planShardWithPlan(t, "test", "abc123", nil)
	_, err := Hydrate(ctx, planShard, nil, HydrateOptions{}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for empty ExecID")
	}
}

// Helpers

// openReader opens an objread.Reader over the object-model root Hydrate writes.
func openReader(t *testing.T, orunDir string) (*objread.Reader, string) {
	t.Helper()
	root := filepath.Join(orunDir, "objectmodel")
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Writer: "test"})
	if err != nil {
		t.Fatalf("open refs: %v", err)
	}
	return objread.New(store, refs, root), root
}

// getExecution reads a sealed execution (with its jobs) back by id.
func getExecution(t *testing.T, orunDir, execID string) objread.ExecutionView {
	t.Helper()
	r, _ := openReader(t, orunDir)
	view, err := r.Get(context.Background(), execID)
	if err != nil {
		t.Fatalf("Get(%s): %v", execID, err)
	}
	return view
}

func planShardWithPlan(t *testing.T, execID, planID string, jobIDs []string) *PlanShard {
	t.Helper()
	var jobs []model.PlanJob
	for _, jid := range jobIDs {
		jobs = append(jobs, model.PlanJob{
			ID:   jid,
			UID:  "uid-" + jid,
			Name: jid,
		})
	}
	return &PlanShard{
		Dir: "",
		Manifest: &RunBundleShardManifest{
			APIVersion:    manifestAPIVersion,
			Kind:          manifestKind,
			SchemaVersion: schemaVersion,
			Role:          ShardRolePlan,
			ExecID:        execID,
			PlanID:        planID,
			Source:        ShardSource{Type: "test"},
		},
		Plan: &model.Plan{
			APIVersion: "orun/v1",
			Kind:       "Plan",
			Metadata:   model.PlanMetadata{Name: "test-plan", Checksum: "sha256-" + planID},
			Jobs:       jobs,
		},
	}
}

func jobShardWithState(execID, planID, jobUID, jobID, status string) *JobShard {
	return &JobShard{
		Dir: "",
		Manifest: &RunBundleShardManifest{
			APIVersion:    manifestAPIVersion,
			Kind:          manifestKind,
			SchemaVersion: schemaVersion,
			Role:          ShardRoleJob,
			ExecID:        execID,
			PlanID:        planID,
			JobUID:        jobUID,
			JobID:         jobID,
			Status:        status,
			StartedAt:     "2026-05-23T12:00:00Z",
			FinishedAt:    "2026-05-23T12:05:00Z",
			Source:        ShardSource{Type: "test"},
		},
		JobState: &execmodel.JobState{
			Status:     status,
			StartedAt:  "2026-05-23T12:00:00Z",
			FinishedAt: "2026-05-23T12:05:00Z",
			Steps:      map[string]string{"step1": status},
		},
	}
}
