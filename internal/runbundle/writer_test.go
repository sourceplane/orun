package runbundle

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
)

func TestWritePlanShard_CreatesShardDirectory(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	plan := &model.Plan{
		APIVersion: "orun/v1",
		Kind:       "Plan",
		Metadata: model.PlanMetadata{
			Name:        "test-plan",
			Checksum:    "sha256-a1b2c3d4e5f6a1b2c3d4e5f6a1b2c3d4e5f6a1b2",
			GeneratedAt: "2026-05-23T12:00:00Z",
			Trigger: &model.PlanTrigger{
				Mode:     "github-actions",
				Provider: "github",
				Event:    "pull_request",
			},
		},
		Jobs: []model.PlanJob{
			{ID: "test-job", Name: "Test Job", Steps: []model.PlanStep{
				{ID: "step1", Run: "echo hello"},
			}},
		},
	}

	shard, err := WritePlanShard(ctx, WritePlanShardOptions{
		ExecID: "gh-12345-1-a1b2c3",
		Plan:   plan,
		Source: ShardSource{
			Type:       "github-actions",
			Repository: "sourceplane/orun",
			RunID:      "12345",
			SHA:        "abc123def456",
			Ref:        "refs/heads/main",
		},
		OutputDir: filepath.Join(tmpDir, "plan-shard"),
	})
	if err != nil {
		t.Fatalf("WritePlanShard failed: %v", err)
	}

	if shard == nil {
		t.Fatal("shard is nil")
	}
	if shard.Role != ShardRolePlan {
		t.Errorf("role = %q, want %q", shard.Role, ShardRolePlan)
	}
	if shard.ExecID != "gh-12345-1-a1b2c3" {
		t.Errorf("execID = %q, want %q", shard.ExecID, "gh-12345-1-a1b2c3")
	}

	// Verify directory contents
	expectedFiles := []string{
		"manifest.json",
		"plan.json",
		"trigger.json",
		"git.json",
		"checksums.json",
	}
	for _, f := range expectedFiles {
		path := filepath.Join(shard.Dir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s does not exist", path)
		}
	}
}

func TestWritePlanShard_NoPlanReturnsError(t *testing.T) {
	ctx := context.Background()
	_, err := WritePlanShard(ctx, WritePlanShardOptions{
		ExecID:    "test",
		OutputDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error for nil plan")
	}
}

func TestWritePlanShard_NoOutputDirReturnsError(t *testing.T) {
	ctx := context.Background()
	_, err := WritePlanShard(ctx, WritePlanShardOptions{
		ExecID: "test",
		Plan:   &model.Plan{},
	})
	if err == nil {
		t.Fatal("expected error for empty output dir")
	}
}

func TestWritePlanShard_ManifestIsValidJSON(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	plan := &model.Plan{
		APIVersion: "orun/v1",
		Kind:       "Plan",
		Metadata: model.PlanMetadata{
			Name:     "test",
			Checksum: "sha256-abc123def456",
		},
	}

	shard, err := WritePlanShard(ctx, WritePlanShardOptions{
		ExecID:    "gh-1-1-abc",
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

	if manifest.Role != ShardRolePlan {
		t.Errorf("role = %q, want %q", manifest.Role, ShardRolePlan)
	}
	if manifest.ExecID != "gh-1-1-abc" {
		t.Errorf("execId = %q, want %q", manifest.ExecID, "gh-1-1-abc")
	}
}

func TestWriteJobShard_CreatesShardDirectory(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()
	logsDir := filepath.Join(tmpDir, "logs-source")
	os.MkdirAll(logsDir, 0755)
	os.WriteFile(filepath.Join(logsDir, "step1.log"), []byte("step1 output"), 0644)
	os.WriteFile(filepath.Join(logsDir, "step2.log"), []byte("step2 output"), 0644)

	js := &execmodel.JobState{
		Status:     "completed",
		StartedAt:  "2026-05-23T12:00:00Z",
		FinishedAt: "2026-05-23T12:05:00Z",
		Steps: map[string]string{
			"step1": "completed",
			"step2": "completed",
		},
	}

	shard, err := WriteJobShard(ctx, WriteJobShardOptions{
		ExecID:    "gh-12345-1-a1b2c3",
		PlanID:    "a1b2c3d4e5f6",
		JobUID:    "job-uid-001",
		JobID:     "network@development.validate-terraform",
		Component: "network",
		Env:       "development",
		Profile:   "default",
		Status:    "completed",
		Source: ShardSource{
			Type:       "github-actions",
			Repository: "sourceplane/orun",
			RunID:      "12345",
		},
		State:     js,
		LogsDir:   logsDir,
		OutputDir: filepath.Join(tmpDir, "job-shard"),
	})
	if err != nil {
		t.Fatalf("WriteJobShard failed: %v", err)
	}

	if shard == nil {
		t.Fatal("shard is nil")
	}
	if shard.Role != ShardRoleJob {
		t.Errorf("role = %q, want %q", shard.Role, ShardRoleJob)
	}

	// Verify directory contents
	expectedFiles := []string{
		"manifest.json",
		"job.json",
		"state.json",
		"steps.jsonl",
		"checksums.json",
		"logs/step1.log",
		"logs/step2.log",
	}
	for _, f := range expectedFiles {
		path := filepath.Join(shard.Dir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s does not exist", path)
		}
	}
}

func TestWriteJobShard_NoStateReturnsError(t *testing.T) {
	ctx := context.Background()
	_, err := WriteJobShard(ctx, WriteJobShardOptions{
		ExecID:    "test",
		OutputDir: t.TempDir(),
	})
	if err == nil {
		t.Fatal("expected error for nil state")
	}
}

func TestWriteJobShard_NoSteps(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	js := &execmodel.JobState{
		Status: "running",
		Steps:  map[string]string{},
	}

	shard, err := WriteJobShard(ctx, WriteJobShardOptions{
		ExecID:    "gh-1-1-abc",
		PlanID:    "plan123",
		JobUID:    "uid-1",
		JobID:     "test-job",
		Status:    "running",
		State:     js,
		OutputDir: filepath.Join(tmpDir, "job-shard"),
	})
	if err != nil {
		t.Fatalf("WriteJobShard failed: %v", err)
	}

	// steps.jsonl should not exist
	if _, err := os.Stat(filepath.Join(shard.Dir, "steps.jsonl")); !os.IsNotExist(err) {
		t.Error("steps.jsonl should not exist for empty steps")
	}

	// But manifest, job, state should exist
	for _, f := range []string{"manifest.json", "job.json", "state.json"} {
		path := filepath.Join(shard.Dir, f)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("expected file %s does not exist", path)
		}
	}
}

func TestWriteJobShard_NoLogsDir(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	js := &execmodel.JobState{
		Status: "completed",
		Steps:  map[string]string{"s1": "completed"},
	}

	shard, err := WriteJobShard(ctx, WriteJobShardOptions{
		ExecID:    "gh-1-1-abc",
		PlanID:    "plan123",
		JobUID:    "uid-1",
		JobID:     "test-job",
		Status:    "completed",
		State:     js,
		OutputDir: filepath.Join(tmpDir, "job-shard"),
	})
	if err != nil {
		t.Fatalf("WriteJobShard failed: %v", err)
	}

	// Should still have basic files
	manifest, err := ReadShardManifest(shard.Dir)
	if err != nil {
		t.Fatalf("ReadShardManifest failed: %v", err)
	}
	if manifest.Role != ShardRoleJob {
		t.Errorf("role = %q, want %q", manifest.Role, ShardRoleJob)
	}
}

func TestWritePlanShard_ChecksumsAreCorrect(t *testing.T) {
	ctx := context.Background()
	tmpDir := t.TempDir()

	plan := &model.Plan{
		APIVersion: "orun/v1",
		Kind:       "Plan",
		Metadata:   model.PlanMetadata{Name: "checksum-test"},
	}

	shard, err := WritePlanShard(ctx, WritePlanShardOptions{
		ExecID:    "gh-1-1-test",
		Plan:      plan,
		OutputDir: filepath.Join(tmpDir, "shard"),
	})
	if err != nil {
		t.Fatalf("WritePlanShard failed: %v", err)
	}

	// Read shard back — validates checksums internally
	_, err = ReadShardManifest(shard.Dir)
	if err != nil {
		t.Fatalf("ReadShardManifest with checksum validation failed: %v", err)
	}
}
