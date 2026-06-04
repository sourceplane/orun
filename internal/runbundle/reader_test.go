package runbundle

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
)

func TestReadShardManifest_ValidPlanShard(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	plan := &model.Plan{
		APIVersion: "orun/v1",
		Kind:       "Plan",
		Metadata:   model.PlanMetadata{Name: "read-test", Checksum: "sha256-abc123"},
	}

	shard, err := WritePlanShard(ctx, WritePlanShardOptions{
		ExecID:    "gh-1-1-read",
		Plan:      plan,
		OutputDir: filepath.Join(tmpDir, "shard"),
	})
	if err != nil {
		t.Fatalf("WritePlanShard failed: %v", err)
	}

	manifest, err := ReadShardManifest(shard.Dir)
	if err != nil {
		t.Fatalf("ReadShardManifest failed: %v", err)
	}

	if manifest.ExecID != "gh-1-1-read" {
		t.Errorf("ExecID = %q, want %q", manifest.ExecID, "gh-1-1-read")
	}
	if manifest.Role != ShardRolePlan {
		t.Errorf("Role = %q, want %q", manifest.Role, ShardRolePlan)
	}
}

func TestReadShardManifest_MissingDirectory(t *testing.T) {
	_, err := ReadShardManifest("/tmp/nonexistent-shard-dir-12345")
	if err == nil {
		t.Fatal("expected error for missing directory")
	}
}

func TestReadShardManifest_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	os.WriteFile(filepath.Join(tmpDir, "manifest.json"), []byte("not json"), 0644)

	_, err := ReadShardManifest(tmpDir)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestReadShardManifest_MissingFiles(t *testing.T) {
	tmpDir := t.TempDir()

	// Write manifest with files that don't exist
	writeJSON(filepath.Join(tmpDir, "manifest.json"), &RunBundleShardManifest{
		APIVersion:    "orun.io/v1alpha1",
		Kind:          "RunBundleShard",
		SchemaVersion: "1.0.0",
		Role:          ShardRolePlan,
		ExecID:        "test",
		PlanID:        "abc123",
		Files: map[string]string{
			"plan": "plan.json",
		},
	})

	_, err := ReadShardManifest(tmpDir)
	if err == nil {
		t.Fatal("expected error for missing files")
	}
}

func TestReadShardManifest_UnsupportedSchemaVersion(t *testing.T) {
	tmpDir := t.TempDir()
	writeJSON(filepath.Join(tmpDir, "manifest.json"), &RunBundleShardManifest{
		APIVersion:    "orun.io/v1alpha1",
		Kind:          "RunBundleShard",
		SchemaVersion: "99.99.99",
		Role:          ShardRolePlan,
		ExecID:        "test",
		PlanID:        "abc123",
		Files:         map[string]string{},
	})

	_, err := ReadShardManifest(tmpDir)
	if err == nil {
		t.Fatal("expected error for unsupported schema version")
	}
}

func TestReadPlanShard_Valid(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	plan := &model.Plan{
		APIVersion: "orun/v1",
		Kind:       "Plan",
		Metadata:   model.PlanMetadata{Name: "plan-read-test"},
		Jobs: []model.PlanJob{
			{ID: "job1", Name: "Job 1"},
		},
	}

	shard, err := WritePlanShard(ctx, WritePlanShardOptions{
		ExecID:    "gh-1-1-test",
		Plan:      plan,
		OutputDir: filepath.Join(tmpDir, "shard"),
	})
	if err != nil {
		t.Fatalf("WritePlanShard failed: %v", err)
	}

	ps, err := ReadPlanShard(shard.Dir)
	if err != nil {
		t.Fatalf("ReadPlanShard failed: %v", err)
	}

	if ps.Manifest.Role != ShardRolePlan {
		t.Errorf("Role = %q, want %q", ps.Manifest.Role, ShardRolePlan)
	}
	if ps.Plan.Metadata.Name != "plan-read-test" {
		t.Errorf("Plan name = %q, want %q", ps.Plan.Metadata.Name, "plan-read-test")
	}
	if len(ps.Plan.Jobs) != 1 {
		t.Errorf("Jobs = %d, want 1", len(ps.Plan.Jobs))
	}
}

func TestReadPlanShard_WrongRoleShard(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	js := &execmodel.JobState{Status: "completed", Steps: map[string]string{}}
	shard, err := WriteJobShard(ctx, WriteJobShardOptions{
		ExecID:    "gh-1-1-test",
		PlanID:    "abc123",
		JobUID:    "uid-1",
		JobID:     "test-job",
		Status:    "completed",
		State:     js,
		OutputDir: filepath.Join(tmpDir, "job-shard"),
	})
	if err != nil {
		t.Fatalf("WriteJobShard failed: %v", err)
	}

	_, err = ReadPlanShard(shard.Dir)
	if err == nil {
		t.Fatal("expected error reading job shard as plan shard")
	}
}

func TestReadJobShard_Valid(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	js := &execmodel.JobState{
		Status:     "completed",
		StartedAt:  "2026-05-23T12:00:00Z",
		FinishedAt: "2026-05-23T12:05:00Z",
		Steps: map[string]string{
			"step1": "completed",
			"step2": "failed",
		},
		LastError: "step2 failed",
	}

	shard, err := WriteJobShard(ctx, WriteJobShardOptions{
		ExecID:    "gh-12345-1-a1b2c3",
		PlanID:    "plan123",
		JobUID:    "uid-001",
		JobID:     "test@dev.validate",
		Status:    "completed",
		State:     js,
		OutputDir: filepath.Join(tmpDir, "job-shard"),
	})
	if err != nil {
		t.Fatalf("WriteJobShard failed: %v", err)
	}

	shardRead, err := ReadJobShard(shard.Dir)
	if err != nil {
		t.Fatalf("ReadJobShard failed: %v", err)
	}

	if shardRead.Manifest.Role != ShardRoleJob {
		t.Errorf("Role = %q, want %q", shardRead.Manifest.Role, ShardRoleJob)
	}
	if shardRead.Manifest.ExecID != "gh-12345-1-a1b2c3" {
		t.Errorf("ExecID = %q, want %q", shardRead.Manifest.ExecID, "gh-12345-1-a1b2c3")
	}
	if shardRead.JobState.Status != "completed" {
		t.Errorf("Status = %q, want %q", shardRead.JobState.Status, "completed")
	}
	if len(shardRead.JobState.Steps) != 2 {
		t.Errorf("Steps = %d, want 2", len(shardRead.JobState.Steps))
	}
	if shardRead.JobState.LastError != "step2 failed" {
		t.Errorf("LastError = %q, want %q", shardRead.JobState.LastError, "step2 failed")
	}
}

func TestReadJobShard_WrongRoleShard(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	plan := &model.Plan{
		APIVersion: "orun/v1",
		Kind:       "Plan",
		Metadata:   model.PlanMetadata{Name: "test"},
	}

	shard, err := WritePlanShard(ctx, WritePlanShardOptions{
		ExecID:    "gh-1-1-test",
		Plan:      plan,
		OutputDir: filepath.Join(tmpDir, "plan-shard"),
	})
	if err != nil {
		t.Fatalf("WritePlanShard failed: %v", err)
	}

	_, err = ReadJobShard(shard.Dir)
	if err == nil {
		t.Fatal("expected error reading plan shard as job shard")
	}
}

func TestReadPlanShard_RoundTrip(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	originalPlan := &model.Plan{
		APIVersion: "orun/v1",
		Kind:       "Plan",
		Metadata: model.PlanMetadata{
			Name:        "round-trip-test",
			Description: "Testing write-then-read",
			Checksum:    "sha256-abcdef1234567890abcdef1234567890abcdef12",
			GeneratedAt: "2026-05-23T12:00:00Z",
		},
		Execution: model.PlanExecution{
			Concurrency: 5,
			FailFast:    false,
		},
		Jobs: []model.PlanJob{
			{
				ID:          "job1",
				Name:        "Job One",
				Component:   "network",
				Environment: "dev",
				Steps: []model.PlanStep{
					{ID: "s1", Run: "echo step1", Order: 1},
					{ID: "s2", Run: "echo step2", Order: 2},
				},
			},
			{
				ID:          "job2",
				Name:        "Job Two",
				Component:   "api",
				Environment: "dev",
				Steps: []model.PlanStep{
					{ID: "s1", Run: "echo deploy", Order: 1},
				},
			},
		},
	}

	shard, err := WritePlanShard(ctx, WritePlanShardOptions{
		ExecID:    "gh-roundtrip-1-abc123",
		Plan:      originalPlan,
		OutputDir: filepath.Join(tmpDir, "shard"),
	})
	if err != nil {
		t.Fatalf("WritePlanShard failed: %v", err)
	}

	ps, err := ReadPlanShard(shard.Dir)
	if err != nil {
		t.Fatalf("ReadPlanShard failed: %v", err)
	}

	if ps.Plan.Metadata.Name != originalPlan.Metadata.Name {
		t.Errorf("Name = %q, want %q", ps.Plan.Metadata.Name, originalPlan.Metadata.Name)
	}
	if len(ps.Plan.Jobs) != len(originalPlan.Jobs) {
		t.Errorf("Jobs count = %d, want %d", len(ps.Plan.Jobs), len(originalPlan.Jobs))
	}
	for i, job := range ps.Plan.Jobs {
		if job.ID != originalPlan.Jobs[i].ID {
			t.Errorf("Job[%d].ID = %q, want %q", i, job.ID, originalPlan.Jobs[i].ID)
		}
	}
}

func TestReadShardFiles_PathTraversalRejected(t *testing.T) {
	tmpDir := t.TempDir()

	// Write manifest with path traversal in file path
	writeJSON(filepath.Join(tmpDir, "manifest.json"), &RunBundleShardManifest{
		APIVersion:    "orun.io/v1alpha1",
		Kind:          "RunBundleShard",
		SchemaVersion: "1.0.0",
		Role:          ShardRolePlan,
		ExecID:        "test",
		PlanID:        "abc123",
		Files: map[string]string{
			"outside": "../../etc/passwd",
		},
	})

	err := ValidateShardFiles(tmpDir, &RunBundleShardManifest{
		APIVersion:    "orun.io/v1alpha1",
		Kind:          "RunBundleShard",
		SchemaVersion: "1.0.0",
		Role:          ShardRolePlan,
		ExecID:        "test",
		PlanID:        "abc123",
		Files: map[string]string{
			"outside": "../../etc/passwd",
		},
	})
	if err == nil {
		t.Fatal("expected error for path traversal")
	}
}
