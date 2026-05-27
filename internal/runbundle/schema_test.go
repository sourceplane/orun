package runbundle

import (
	"encoding/json"
	"testing"
)

func TestRunBundleShardManifestRoundTrip(t *testing.T) {
	m := &RunBundleShardManifest{
		APIVersion:    "orun.io/v1alpha1",
		Kind:          "RunBundleShard",
		SchemaVersion: "1.0.0",
		Role:          ShardRolePlan,
		ExecID:        "gh-12345-1-a1b2c3",
		PlanID:        "a1b2c3d4e5f6",
		Component:     "network",
		Environment:   "development",
		Status:        "created",
		Source: ShardSource{
			Type:       "github-actions",
			Repository: "sourceplane/orun",
			RunID:      "12345",
			RunAttempt: "1",
		},
		Files: map[string]string{
			"manifest": "manifest.json",
			"plan":     "plan.json",
		},
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded RunBundleShardManifest
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ExecID != m.ExecID {
		t.Errorf("ExecID: got %q, want %q", decoded.ExecID, m.ExecID)
	}
	if decoded.Role != m.Role {
		t.Errorf("Role: got %q, want %q", decoded.Role, m.Role)
	}
	if decoded.Source.Type != m.Source.Type {
		t.Errorf("Source.Type: got %q, want %q", decoded.Source.Type, m.Source.Type)
	}
	if len(decoded.Files) != 2 {
		t.Errorf("Files: got %d, want 2", len(decoded.Files))
	}
}

func TestShardSourceRoundTrip(t *testing.T) {
	s := ShardSource{
		Type:       "github-actions",
		Repository: "sourceplane/orun",
		RunID:      "98765",
		Workflow:   "CI",
		SHA:        "abc123def456",
		Ref:        "refs/heads/main",
		EventName:  "pull_request",
	}

	data, err := json.Marshal(s)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ShardSource
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Type != s.Type {
		t.Errorf("Type: got %q, want %q", decoded.Type, s.Type)
	}
	if decoded.Repository != s.Repository {
		t.Errorf("Repository: got %q, want %q", decoded.Repository, s.Repository)
	}
	if decoded.EventName != s.EventName {
		t.Errorf("EventName: got %q, want %q", decoded.EventName, s.EventName)
	}
}

func TestChecksumsRoundTrip(t *testing.T) {
	c := Checksums{
		Algorithm: "sha256",
		Files: map[string]string{
			"plan.json":   "abc123",
			"manifest.json": "def456",
		},
	}

	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded Checksums
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Algorithm != c.Algorithm {
		t.Errorf("Algorithm: got %q, want %q", decoded.Algorithm, c.Algorithm)
	}
	if decoded.Files["plan.json"] != "abc123" {
		t.Errorf("Files[plan.json]: got %q, want %q", decoded.Files["plan.json"], "abc123")
	}
}

func TestSynthesizedExecutionRoundTrip(t *testing.T) {
	exec := &SynthesizedExecution{
		ExecID: "gh-12345-1-a1b2c3",
		PlanID: "a1b2c3d4e5f6",
		Status: "partial",
		Partial: true,
		PartialReason: "missing_job_shards",
		Counts: JobCounts{
			Total:     18,
			Completed: 12,
			Failed:    1,
			Cancelled: 0,
			Skipped:   3,
			Pending:   2,
		},
		Jobs: map[string]JobShardRef{
			"job1": {
				JobUid:    "uid1",
				JobID:     "network@development.validate-terraform",
				Status:    "completed",
				ShardName: "orun.v1.gh-12345-1-a1b2c3.job.uid1.completed",
			},
		},
		PlanShard: ShardRef{
			Name: "orun.v1.gh-12345-1-a1b2c3.plan.a1b2c3d4.created",
			Role: "plan",
		},
		CreatedAt: "2026-05-23T12:00:00Z",
	}

	data, err := json.Marshal(exec)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded SynthesizedExecution
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.ExecID != exec.ExecID {
		t.Errorf("ExecID: got %q, want %q", decoded.ExecID, exec.ExecID)
	}
	if decoded.Status != exec.Status {
		t.Errorf("Status: got %q, want %q", decoded.Status, exec.Status)
	}
	if decoded.Counts.Total != 18 {
		t.Errorf("Counts.Total: got %d, want 18", decoded.Counts.Total)
	}
	if !decoded.Partial {
		t.Error("expected Partial=true")
	}
	if len(decoded.Jobs) != 1 {
		t.Errorf("Jobs: got %d, want 1", len(decoded.Jobs))
	}
}

func TestJobCountsJSONTags(t *testing.T) {
	// Verify all fields have json tags by marshalling zero value
	c := JobCounts{Total: 5, Completed: 3, Failed: 1, Cancelled: 0, Skipped: 1, Pending: 0}
	data, err := json.Marshal(c)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded JobCounts
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if decoded.Total != 5 {
		t.Errorf("Total: got %d, want 5", decoded.Total)
	}
}

func TestShardRefRoundTrip(t *testing.T) {
	r := ShardRef{
		Name:   "test-artifact",
		Role:   "plan",
		PlanID: "abc123",
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded ShardRef
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.Name != r.Name {
		t.Errorf("Name: got %q, want %q", decoded.Name, r.Name)
	}
	if decoded.Role != r.Role {
		t.Errorf("Role: got %q, want %q", decoded.Role, r.Role)
	}
}

func TestJobShardRefRoundTrip(t *testing.T) {
	r := JobShardRef{
		JobUid:     "uid-123",
		JobID:      "network@development.validate",
		Status:     "completed",
		ShardName:  "orun.v1.exec.job.uid-123.completed",
		StartedAt:  "2026-05-23T12:00:00Z",
		FinishedAt: "2026-05-23T12:05:00Z",
	}

	data, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("failed to marshal: %v", err)
	}

	var decoded JobShardRef
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}

	if decoded.JobUid != r.JobUid {
		t.Errorf("JobUid: got %q, want %q", decoded.JobUid, r.JobUid)
	}
	if decoded.Status != r.Status {
		t.Errorf("Status: got %q, want %q", decoded.Status, r.Status)
	}
	if decoded.ShardName != r.ShardName {
		t.Errorf("ShardName: got %q, want %q", decoded.ShardName, r.ShardName)
	}
}

func TestShardRoleConstants(t *testing.T) {
	if ShardRolePlan != "plan" {
		t.Errorf("ShardRolePlan = %q, want %q", ShardRolePlan, "plan")
	}
	if ShardRoleJob != "job" {
		t.Errorf("ShardRoleJob = %q, want %q", ShardRoleJob, "job")
	}
}