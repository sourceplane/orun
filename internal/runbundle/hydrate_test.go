package runbundle

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/state"
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

	// Verify directory layout
	execDir := filepath.Join(orunDir, "executions", "gh-1-1-abc")
	if _, err := os.Stat(execDir); os.IsNotExist(err) {
		t.Fatalf("execution directory not created at %s", execDir)
	}

	expectedFiles := []string{
		"metadata.json",
		"state.json",
		"github.json",
		"plan.json",
		"shards.json",
	}
	for _, f := range expectedFiles {
		path := filepath.Join(execDir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s does not exist", path)
		}
	}

	// Verify latest symlink
	latestLink := filepath.Join(orunDir, "executions", "latest")
	target, err := os.Readlink(latestLink)
	if err != nil {
		t.Fatalf("failed to read latest symlink: %v", err)
	}
	if target != "gh-1-1-abc" {
		t.Errorf("latest symlink = %q, want %q", target, "gh-1-1-abc")
	}
}

func TestHydrate_WithLogs(t *testing.T) {
	ctx := context.Background()
	orunDir := filepath.Join(t.TempDir(), ".orun")
	shardDir := filepath.Join(t.TempDir(), "job-shard")

	// Create a job shard with logs on disk
	logsDir := filepath.Join(shardDir, "logs")
	os.MkdirAll(logsDir, 0755)
	os.WriteFile(filepath.Join(logsDir, "step1.log"), []byte("step1 output"), 0644)
	os.WriteFile(filepath.Join(logsDir, "step2.log"), []byte("step2 output"), 0644)

	// Write the shard files
	js := &state.JobState{
		Status:     "completed",
		StartedAt:  "2026-05-23T12:00:00Z",
		FinishedAt: "2026-05-23T12:05:00Z",
		Steps:      map[string]string{"step1": "completed", "step2": "completed"},
	}
	writeJSON(filepath.Join(shardDir, "job.json"), map[string]string{"jobUid": "uid-job1", "jobId": "job1"})
	writeJSON(filepath.Join(shardDir, "state.json"), js)
	writeJSON(filepath.Join(shardDir, "manifest.json"), &RunBundleShardManifest{
		APIVersion:    manifestAPIVersion,
		Kind:          manifestKind,
		SchemaVersion: schemaVersion,
		Role:          ShardRoleJob,
		ExecID:        "gh-1-1-abc",
		PlanID:        "abc123",
		JobUID:        "uid-job1",
		JobID:         "job1",
		Status:        "completed",
		Files: map[string]string{
			"manifest":     "manifest.json",
			"job":          "job.json",
			"state":        "state.json",
			"checksums":    "checksums.json",
			"log:step1":    "logs/step1.log",
			"log:step2":    "logs/step2.log",
		},
	})

	planShard := planShardWithPlan(t, "gh-1-1-abc", "abc123", []string{"job1"})
	jobShards := []*JobShard{
		{
			Dir:      shardDir,
			Manifest: jobShardWithState("gh-1-1-abc", "abc123", "uid-job1", "job1", "completed").Manifest,
			JobState: js,
		},
	}

	// Update the manifest's Files to include logs
	jobShards[0].Manifest.Files = map[string]string{
		"manifest":  "manifest.json",
		"job":       "job.json",
		"state":     "state.json",
		"checksums": "checksums.json",
		"log:step1": "logs/step1.log",
		"log:step2": "logs/step2.log",
	}

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

	// Verify log files exist
	logPath := filepath.Join(orunDir, "executions", "gh-1-1-abc", "logs", "job1", "step1.log")
	if _, err := os.Stat(logPath); os.IsNotExist(err) {
		t.Errorf("expected log file %s does not exist", logPath)
	}

	logPath2 := filepath.Join(orunDir, "executions", "gh-1-1-abc", "logs", "job1", "step2.log")
	if _, err := os.Stat(logPath2); os.IsNotExist(err) {
		t.Errorf("expected log file %s does not exist", logPath2)
	}
}

func TestHydrate_OverwriteProtection(t *testing.T) {
	ctx := context.Background()
	orunDir := filepath.Join(t.TempDir(), ".orun")

	planShard := planShardWithPlan(t, "gh-1-1-abc", "abc123", []string{"job1"})
	jobShards := []*JobShard{
		jobShardWithState("gh-1-1-abc", "abc123", "uid-job1", "job1", "completed"),
	}

	// First hydration succeeds
	_, err := Hydrate(ctx, planShard, jobShards, HydrateOptions{
		ExecID: "gh-1-1-abc",
		Source: ShardSource{Type: "test"},
	}, orunDir)
	if err != nil {
		t.Fatalf("first Hydrate failed: %v", err)
	}

	// Second hydration without Overwrite should fail
	_, err = Hydrate(ctx, planShard, jobShards, HydrateOptions{
		ExecID: "gh-1-1-abc",
		Source: ShardSource{Type: "test"},
	}, orunDir)
	if err == nil {
		t.Fatal("expected error for existing directory without Overwrite")
	}

	// With Overwrite, it should succeed
	_, err = Hydrate(ctx, planShard, jobShards, HydrateOptions{
		ExecID:    "gh-1-1-abc",
		Source:    ShardSource{Type: "test"},
		Overwrite: true,
	}, orunDir)
	if err != nil {
		t.Fatalf("Hydrate with Overwrite failed: %v", err)
	}
}

func TestHydrate_NilPlanShard(t *testing.T) {
	ctx := context.Background()
	_, err := Hydrate(ctx, nil, nil, HydrateOptions{ExecID: "test"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for nil plan shard")
	}
}

func TestHydrate_StateIsReadableByStore(t *testing.T) {
	ctx := context.Background()
	orunDir := filepath.Join(t.TempDir(), ".orun")

	planShard := planShardWithPlan(t, "gh-readable-1-abc", "abc123", []string{"job1", "job2"})
	jobShards := []*JobShard{
		jobShardWithState("gh-readable-1-abc", "abc123", "uid-job1", "job1", "completed"),
		jobShardWithState("gh-readable-1-abc", "abc123", "uid-job2", "job2", "failed"),
	}

	_, err := Hydrate(ctx, planShard, jobShards, HydrateOptions{
		ExecID: "gh-readable-1-abc",
		Source: ShardSource{Type: "test"},
	}, orunDir)
	if err != nil {
		t.Fatalf("Hydrate failed: %v", err)
	}

	// Verify state.Store can read the hydrated state
	store := state.NewStore(filepath.Dir(orunDir))
	loaded, err := store.LoadState("gh-readable-1-abc")
	if err != nil {
		t.Fatalf("LoadState failed: %v", err)
	}
	if loaded.ExecID != "gh-readable-1-abc" {
		t.Errorf("ExecID = %q, want %q", loaded.ExecID, "gh-readable-1-abc")
	}
	if len(loaded.Jobs) != 2 {
		t.Errorf("Jobs = %d, want 2", len(loaded.Jobs))
	}
	if loaded.Jobs["job1"].Status != "completed" {
		t.Errorf("job1 status = %q, want %q", loaded.Jobs["job1"].Status, "completed")
	}
	if loaded.Jobs["job2"].Status != "failed" {
		t.Errorf("job2 status = %q, want %q", loaded.Jobs["job2"].Status, "failed")
	}

	// Verify metadata
	meta, err := store.LoadMetadata("gh-readable-1-abc")
	if err != nil {
		t.Fatalf("LoadMetadata failed: %v", err)
	}
	if meta.ExecID != "gh-readable-1-abc" {
		t.Errorf("ExecID = %q, want %q", meta.ExecID, "gh-readable-1-abc")
	}
	if meta.JobTotal != 2 {
		t.Errorf("JobTotal = %d, want 2", meta.JobTotal)
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

func planShardWithPlan(t *testing.T, execID, planID string, jobIDs []string) *PlanShard {
	t.Helper()
	var jobs []model.PlanJob
	for _, jid := range jobIDs {
		jobs = append(jobs, model.PlanJob{
			ID:   jid,
			UID: "uid-" + jid,
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
		JobState: &state.JobState{
			Status:     status,
			StartedAt:  "2026-05-23T12:00:00Z",
			FinishedAt: "2026-05-23T12:05:00Z",
			Steps:      map[string]string{"step1": status},
		},
	}
}