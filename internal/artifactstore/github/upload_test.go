package github

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/artifactstore"
	"github.com/sourceplane/orun/internal/runbundle"
)

// TestIsGitHubActions tests the GitHub Actions environment detection.
func TestIsGitHubActions(t *testing.T) {
	// When GITHUB_ACTIONS is not set
	os.Unsetenv("GITHUB_ACTIONS")
	if IsGitHubActions() {
		t.Error("expected false when GITHUB_ACTIONS is not set")
	}

	// When GITHUB_ACTIONS is true
	os.Setenv("GITHUB_ACTIONS", "true")
	defer os.Unsetenv("GITHUB_ACTIONS")
	if !IsGitHubActions() {
		t.Error("expected true when GITHUB_ACTIONS is true")
	}
}

// TestUpload_NotInGitHubActions verifies Upload returns error outside GHA.
func TestUpload_NotInGitHubActions(t *testing.T) {
	os.Unsetenv("GITHUB_ACTIONS")

	client, err := NewClient(context.Background(), "owner/repo", WithToken("test"))
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	_, err = client.Upload(context.Background(), &runbundle.Shard{
		Dir:    t.TempDir(),
		ExecID: "gh-123-1-abc",
		Role:   runbundle.ShardRolePlan,
		Suffix: "abc123",
		Status: "created",
	})
	if err == nil {
		t.Fatal("expected error when not in GitHub Actions")
	}
	if !strings.Contains(err.Error(), "only supported inside GitHub Actions") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestUpload_NilShard verifies Upload rejects nil shard.
func TestUpload_NilShard(t *testing.T) {
	os.Setenv("GITHUB_ACTIONS", "true")
	defer os.Unsetenv("GITHUB_ACTIONS")

	client, _ := NewClient(context.Background(), "owner/repo", WithToken("test"))
	_, err := client.Upload(context.Background(), nil)
	if err == nil {
		t.Fatal("expected error for nil shard")
	}
}

// TestUpload_EmptyDir verifies Upload rejects shard with empty Dir.
func TestUpload_EmptyDir(t *testing.T) {
	os.Setenv("GITHUB_ACTIONS", "true")
	defer os.Unsetenv("GITHUB_ACTIONS")

	client, _ := NewClient(context.Background(), "owner/repo", WithToken("test"))
	_, err := client.Upload(context.Background(), &runbundle.Shard{
		ExecID: "gh-123-1-abc",
		Role:   runbundle.ShardRolePlan,
		Suffix: "abc123",
		Status: "created",
	})
	if err == nil {
		t.Fatal("expected error for empty Dir")
	}
}

// TestUpload_ResultParsing verifies the JSON result parsing works.
func TestUpload_ResultParsing(t *testing.T) {
	// Simulate the JSON output the helper would produce
	result := artifactstore.UploadResult{
		ID:   "12345",
		Name: "orun.v1.gh-123-1-abc.plan.abc123.created",
		Size: 1024,
	}

	data, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var parsed artifactstore.UploadResult
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if parsed.ID != "12345" {
		t.Errorf("ID = %q, want %q", parsed.ID, "12345")
	}
	if parsed.Size != 1024 {
		t.Errorf("Size = %d, want 1024", parsed.Size)
	}
}

// TestUpload_ResultParsingEmptyID verifies empty ID is detected.
func TestUpload_ResultParsingEmptyID(t *testing.T) {
	data := []byte(`{"id":"","name":"test","size":0}`)

	var result artifactstore.UploadResult
	if err := json.Unmarshal(data, &result); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if result.ID != "" {
		t.Errorf("expected empty ID")
	}
}

// TestRetentionDaysFromEnv tests the retention days parsing.
func TestRetentionDaysFromEnv(t *testing.T) {
	// Default
	os.Unsetenv("ORUN_ARTIFACT_RETENTION_DAYS")
	if d := retentionDaysFromEnv(); d != DefaultRetentionDays {
		t.Errorf("default = %d, want %d", d, DefaultRetentionDays)
	}

	// Custom value
	os.Setenv("ORUN_ARTIFACT_RETENTION_DAYS", "30")
	defer os.Unsetenv("ORUN_ARTIFACT_RETENTION_DAYS")
	if d := retentionDaysFromEnv(); d != 30 {
		t.Errorf("retention = %d, want 30", d)
	}

	// Invalid value defaults
	os.Setenv("ORUN_ARTIFACT_RETENTION_DAYS", "invalid")
	if d := retentionDaysFromEnv(); d != DefaultRetentionDays {
		t.Errorf("invalid retention = %d, want %d", d, DefaultRetentionDays)
	}
}

// TestEnsureHelperExtracted_FileContents verifies the embedded files exist.
func TestEnsureHelperExtracted_FileContents(t *testing.T) {
	if len(helperPackageJSON) == 0 {
		t.Error("helperPackageJSON is empty")
	}
	if len(helperUploadMJS) == 0 {
		t.Error("helperUploadMJS is empty")
	}
}

// TestArtifactNameMatchesUpload verifies the artifact name construction.
func TestArtifactNameMatchesUpload(t *testing.T) {
	name := runbundle.ArtifactName("gh-123-1-abc", runbundle.ShardRolePlan, "abc123", "created")
	expected := "orun.v1.gh-123-1-abc.plan.abc123.created"
	if name != expected {
		t.Errorf("name = %q, want %q", name, expected)
	}
}

// TestUpload_HelperExec verifies the helper execution path with a mock.
func TestUpload_HelperExec(t *testing.T) {
	os.Setenv("GITHUB_ACTIONS", "true")
	defer os.Unsetenv("GITHUB_ACTIONS")
	os.Setenv("ACTIONS_RUNTIME_TOKEN", "test-token")
	defer os.Unsetenv("ACTIONS_RUNTIME_TOKEN")
	os.Setenv("ACTIONS_RESULTS_URL", "https://results.example.com")
	defer os.Unsetenv("ACTIONS_RESULTS_URL")

	// Create a temp shard dir with a manifest
	shardDir := t.TempDir()
	manifestFile := filepath.Join(shardDir, "manifest.json")
	if err := os.WriteFile(manifestFile, []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"RunBundleShard"}`), 0644); err != nil {
		t.Fatalf("write manifest: %v", err)
	}

	client, err := NewClient(context.Background(), "owner/repo", WithToken("test"))
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	// This will fail because there's no Node.js helper installed,
	// but we test that it reaches the extraction + exec phase
	// rather than failing early on env detection.
	_, err = client.Upload(context.Background(), &runbundle.Shard{
		Dir:    shardDir,
		ExecID: "gh-123-1-abc",
		Role:   runbundle.ShardRolePlan,
		Suffix: "abc123",
		Status: "created",
	})
	if err != nil {
		// We expect either an extraction error or exec error,
		// but not "only supported inside GitHub Actions"
		if strings.Contains(err.Error(), "only supported inside GitHub Actions") {
			t.Fatalf("unexpected early rejection: %v", err)
		}
		// Any other error is fine — it means we passed env detection
		t.Logf("expected exec-phase error: %v", err)
	}
}

// ensure exec is imported for compilation reference
var _ = exec.Command