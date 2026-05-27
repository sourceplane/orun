package runbundle

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/state"
)

func TestSynthesize_HappyPath(t *testing.T) {
	planShard := planShardFixture(t, "gh-1-1-abc", "plan123", []string{"job1", "job2"})
	jobShards := []*JobShard{
		jobShardFixture("gh-1-1-abc", "plan123", "uid-job1", "job1", "completed"),
		jobShardFixture("gh-1-1-abc", "plan123", "uid-job2", "job2", "completed"),
	}

	exec, err := Synthesize(planShard, jobShards)
	if err != nil {
		t.Fatalf("Synthesize failed: %v", err)
	}

	if exec.ExecID != "gh-1-1-abc" {
		t.Errorf("ExecID = %q, want %q", exec.ExecID, "gh-1-1-abc")
	}
	if exec.Status != "completed" {
		t.Errorf("Status = %q, want %q", exec.Status, "completed")
	}
	if exec.Partial {
		t.Error("expected Partial=false")
	}
	if exec.Counts.Total != 2 {
		t.Errorf("Counts.Total = %d, want 2", exec.Counts.Total)
	}
	if exec.Counts.Completed != 2 {
		t.Errorf("Counts.Completed = %d, want 2", exec.Counts.Completed)
	}
	if len(exec.Jobs) != 2 {
		t.Errorf("Jobs = %d, want 2", len(exec.Jobs))
	}
}

func TestSynthesize_PartialMissingJobs(t *testing.T) {
	planShard := planShardFixture(t, "gh-1-1-abc", "plan123", []string{"job1", "job2", "job3"})
	jobShards := []*JobShard{
		jobShardFixture("gh-1-1-abc", "plan123", "uid-job1", "job1", "completed"),
		// job2 and job3 are missing
	}

	exec, err := Synthesize(planShard, jobShards)
	if err != nil {
		t.Fatalf("Synthesize failed: %v", err)
	}

	if exec.Status != "partial" {
		t.Errorf("Status = %q, want %q", exec.Status, "partial")
	}
	if !exec.Partial {
		t.Error("expected Partial=true")
	}
	if exec.Counts.Total != 3 {
		t.Errorf("Counts.Total = %d, want 3", exec.Counts.Total)
	}
	if exec.Counts.Completed != 1 {
		t.Errorf("Counts.Completed = %d, want 1", exec.Counts.Completed)
	}
	if exec.Counts.Pending != 2 {
		t.Errorf("Counts.Pending = %d, want 2", exec.Counts.Pending)
	}
}

func TestSynthesize_FailedJob(t *testing.T) {
	planShard := planShardFixture(t, "gh-1-1-abc", "plan123", []string{"job1", "job2"})
	jobShards := []*JobShard{
		jobShardFixture("gh-1-1-abc", "plan123", "uid-job1", "job1", "completed"),
		jobShardFixture("gh-1-1-abc", "plan123", "uid-job2", "job2", "failed"),
	}

	exec, err := Synthesize(planShard, jobShards)
	if err != nil {
		t.Fatalf("Synthesize failed: %v", err)
	}

	if exec.Status != "failed" {
		t.Errorf("Status = %q, want %q", exec.Status, "failed")
	}
	if exec.Counts.Failed != 1 {
		t.Errorf("Counts.Failed = %d, want 1", exec.Counts.Failed)
	}
}

func TestSynthesize_AllCancelled(t *testing.T) {
	planShard := planShardFixture(t, "gh-1-1-abc", "plan123", []string{"job1"})
	jobShards := []*JobShard{
		jobShardFixture("gh-1-1-abc", "plan123", "uid-job1", "job1", "cancelled"),
	}

	exec, err := Synthesize(planShard, jobShards)
	if err != nil {
		t.Fatalf("Synthesize failed: %v", err)
	}

	if exec.Status != "cancelled" {
		t.Errorf("Status = %q, want %q", exec.Status, "cancelled")
	}
	if exec.Counts.Cancelled != 1 {
		t.Errorf("Counts.Cancelled = %d, want 1", exec.Counts.Cancelled)
	}
}

func TestSynthesize_SkippedJobs(t *testing.T) {
	planShard := planShardFixture(t, "gh-1-1-abc", "plan123", []string{"job1", "job2", "job3"})
	jobShards := []*JobShard{
		jobShardFixture("gh-1-1-abc", "plan123", "uid-job1", "job1", "completed"),
		jobShardFixture("gh-1-1-abc", "plan123", "uid-job2", "job2", "skipped"),
		jobShardFixture("gh-1-1-abc", "plan123", "uid-job3", "job3", "skipped"),
	}

	exec, err := Synthesize(planShard, jobShards)
	if err != nil {
		t.Fatalf("Synthesize failed: %v", err)
	}

	if exec.Status != "completed" {
		t.Errorf("Status = %q, want %q", exec.Status, "completed")
	}
	if exec.Counts.Completed != 1 {
		t.Errorf("Counts.Completed = %d, want 1", exec.Counts.Completed)
	}
	if exec.Counts.Skipped != 2 {
		t.Errorf("Counts.Skipped = %d, want 2", exec.Counts.Skipped)
	}
}

func TestSynthesize_NilPlanShard(t *testing.T) {
	_, err := Synthesize(nil, nil)
	if err == nil {
		t.Fatal("expected error for nil plan shard")
	}
}

func TestSynthesize_EmptyJobs(t *testing.T) {
	planShard := planShardFixture(t, "gh-1-1-abc", "plan123", nil)
	exec, err := Synthesize(planShard, nil)
	if err != nil {
		t.Fatalf("Synthesize failed: %v", err)
	}

	if exec.Status != "unknown" {
		t.Errorf("Status = %q, want %q", exec.Status, "unknown")
	}
}

func TestSynthesizedStatus(t *testing.T) {
	tests := []struct {
		name string
		exec *SynthesizedExecution
		want string
	}{
		{"nil", nil, "unknown"},
		{"completed", &SynthesizedExecution{Status: "completed"}, "completed"},
		{"failed", &SynthesizedExecution{Status: "failed"}, "failed"},
		{"partial", &SynthesizedExecution{Status: "partial", Partial: true}, "partial"},
		{"cancelled", &SynthesizedExecution{Status: "cancelled"}, "cancelled"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SynthesizedStatus(tt.exec)
			if got != tt.want {
				t.Errorf("SynthesizedStatus() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSynthesizedSummary(t *testing.T) {
	exec := &SynthesizedExecution{
		Status: "partial",
		Partial: true,
		Counts: JobCounts{Total: 10, Completed: 7, Failed: 1},
	}
	summary := SynthesizedSummary(exec)
	if summary != "partial  8/10 shards" {
		t.Errorf("SynthesizedSummary() = %q, want %q", summary, "partial  8/10 shards")
	}
}

// Test helpers

func planShardFixture(t *testing.T, execID, planID string, jobIDs []string) *PlanShard {
	t.Helper()

	var jobs []model.PlanJob
	for _, jid := range jobIDs {
		uid := "uid-" + jid
		jobs = append(jobs, model.PlanJob{
			ID:  jid,
			UID: uid,
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

func jobShardFixture(execID, planID, jobUID, jobID, status string) *JobShard {
	startedAt := "2026-05-23T12:00:00Z"
	finishedAt := "2026-05-23T12:05:00Z"

	var steps map[string]string
	if status == "completed" || status == "failed" {
		steps = map[string]string{"step1": status, "step2": "completed"}
	} else {
		steps = map[string]string{}
	}

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
			StartedAt:     startedAt,
			FinishedAt:    finishedAt,
			Source:        ShardSource{Type: "test"},
		},
		JobState: &state.JobState{
			Status:     status,
			StartedAt:  startedAt,
			FinishedAt: finishedAt,
			Steps:      steps,
		},
	}
}