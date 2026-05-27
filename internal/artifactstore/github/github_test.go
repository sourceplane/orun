package github

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/artifactstore"
)

// setupTestServer creates a test HTTP server that responds to GitHub API endpoints.
func setupTestServer(t *testing.T, handlers map[string]func(w http.ResponseWriter, r *http.Request)) (*Client, *httptest.Server) {
	t.Helper()

	mux := http.NewServeMux()
	for pattern, handler := range handlers {
		mux.HandleFunc(pattern, handler)
	}

	server := httptest.NewServer(mux)

	client, err := NewClient(context.Background(), "sourceplane/orun",
		WithBaseURL(server.URL),
		WithToken("test-token"),
	)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	return client, server
}

func TestNewClient_InvalidRepo(t *testing.T) {
	_, err := NewClient(context.Background(), "")
	if err == nil {
		t.Fatal("expected error for empty repo")
	}
	_, err = NewClient(context.Background(), "no-slash")
	if err == nil {
		t.Fatal("expected error for invalid repo format")
	}
}

func TestNewClient_WithOptions(t *testing.T) {
	c, err := NewClient(context.Background(), "owner/repo",
		WithToken("custom-token"),
		WithBaseURL("https://github.enterprise.local/api/v3"),
	)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}
	if c.token != "custom-token" {
		t.Errorf("token = %q, want %q", c.token, "custom-token")
	}
	if c.baseURL != "https://github.enterprise.local/api/v3" {
		t.Errorf("baseURL = %q, want %q", c.baseURL, "https://github.enterprise.local/api/v3")
	}
}

func TestListWorkflowRuns(t *testing.T) {
	ctx := context.Background()

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/runs": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("per_page") != "10" {
				t.Errorf("expected per_page=10, got %s", r.URL.Query().Get("per_page"))
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"workflow_runs": []map[string]interface{}{
					{
						"id": 12345, "name": "CI", "head_sha": "abc123",
						"head_branch": "main", "status": "completed",
						"conclusion": "success", "event": "pull_request",
					},
					{
						"id": 12346, "name": "CI", "head_sha": "def456",
						"head_branch": "main", "status": "completed",
						"conclusion": "failure", "event": "push",
					},
				},
			})
		},
	})
	defer server.Close()

	runs, err := client.ListWorkflowRuns(ctx, ListRunOptions{PerPage: 10})
	if err != nil {
		t.Fatalf("ListWorkflowRuns failed: %v", err)
	}
	if len(runs) != 2 {
		t.Errorf("got %d runs, want 2", len(runs))
	}
	if runs[0].ID != 12345 {
		t.Errorf("run[0].ID = %d, want 12345", runs[0].ID)
	}
	if runs[1].Conclusion != "failure" {
		t.Errorf("run[1].Conclusion = %q, want %q", runs[1].Conclusion, "failure")
	}
}

func TestListWorkflowRuns_WithBranchFilter(t *testing.T) {
	ctx := context.Background()

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/runs": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("branch") != "feature" {
				t.Errorf("expected branch=feature, got %s", r.URL.Query().Get("branch"))
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"workflow_runs": []map[string]interface{}{},
			})
		},
	})
	defer server.Close()

	runs, err := client.ListWorkflowRuns(ctx, ListRunOptions{Branch: "feature", PerPage: 5})
	if err != nil {
		t.Fatalf("ListWorkflowRuns failed: %v", err)
	}
	if len(runs) != 0 {
		t.Errorf("got %d runs, want 0", len(runs))
	}
}

func TestListArtifacts(t *testing.T) {
	ctx := context.Background()

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/runs/123/artifacts": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"artifacts": []map[string]interface{}{
					{
						"id": 1, "name": "orun.v1.gh-123-1-abc.plan.abc.created",
						"size_in_bytes": 1024, "created_at": "2026-05-23T12:00:00Z",
						"expires_at": "2026-06-23T12:00:00Z",
					},
					{
						"id": 2, "name": "orun.v1.gh-123-1-abc.job.uid1.completed",
						"size_in_bytes": 512, "created_at": "2026-05-23T12:05:00Z",
						"expires_at": "2026-06-23T12:05:00Z",
					},
					{
						"id": 3, "name": "other-artifact",
						"size_in_bytes": 256, "created_at": "2026-05-23T12:10:00Z",
						"expires_at": "2026-06-23T12:10:00Z",
					},
				},
			})
		},
	})
	defer server.Close()

	shards, err := client.ListArtifacts(ctx, 123)
	if err != nil {
		t.Fatalf("ListArtifacts failed: %v", err)
	}
	if len(shards) != 3 {
		t.Errorf("got %d shards, want 3", len(shards))
	}
	if shards[0].Name != "orun.v1.gh-123-1-abc.plan.abc.created" {
		t.Errorf("shard[0].Name = %q", shards[0].Name)
	}
}

func TestListOrunArtifacts(t *testing.T) {
	ctx := context.Background()

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/runs/123/artifacts": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"artifacts": []map[string]interface{}{
					{"id": 1, "name": "orun.v1.gh-123-1-abc.plan.abc.created", "size_in_bytes": 100},
					{"id": 2, "name": "orun.v1.gh-123-1-abc.job.uid.completed", "size_in_bytes": 200},
					{"id": 3, "name": "other-artifact", "size_in_bytes": 300},
					{"id": 4, "name": "random-data", "size_in_bytes": 400},
				},
			})
		},
	})
	defer server.Close()

	shards, err := client.ListOrunArtifacts(ctx, 123)
	if err != nil {
		t.Fatalf("ListOrunArtifacts failed: %v", err)
	}
	if len(shards) != 2 {
		t.Errorf("got %d orun shards, want 2", len(shards))
	}
	for _, s := range shards {
		if !strings.HasPrefix(s.Name, "orun.v1.") {
			t.Errorf("unexpected non-orun artifact: %s", s.Name)
		}
	}
}

func TestDownload(t *testing.T) {
	ctx := context.Background()
	destDir := t.TempDir()

	// Create a test zip file
	zipBuf := zipArchive(t, map[string]string{
		"manifest.json": `{"apiVersion":"orun.io/v1alpha1","kind":"RunBundleShard","schemaVersion":"1.0.0","role":"plan","execId":"gh-123-1-abc","planId":"abc123","files":{}}`,
		"plan.json":     `{"apiVersion":"orun/v1","kind":"Plan"}`,
	})

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/artifacts/99/zip": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/zip")
			w.Write(zipBuf)
		},
	})
	defer server.Close()

	ds, err := client.Download(ctx, artifactstore.RemoteShard{
		Name: "orun.v1.gh-123-1-abc.plan.abc.created",
		ID:   "99",
	}, destDir)
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}

	if ds.Name != "orun.v1.gh-123-1-abc.plan.abc.created" {
		t.Errorf("Name = %q", ds.Name)
	}

	// Verify files were extracted
	if _, err := os.Stat(filepath.Join(destDir, "manifest.json")); os.IsNotExist(err) {
		t.Error("manifest.json not extracted")
	}
	if _, err := os.Stat(filepath.Join(destDir, "plan.json")); os.IsNotExist(err) {
		t.Error("plan.json not extracted")
	}
}

func TestDownloadByName(t *testing.T) {
	ctx := context.Background()
	destDir := t.TempDir()

	zipBuf := zipArchive(t, map[string]string{
		"manifest.json": `{"apiVersion":"orun.io/v1alpha1","kind":"RunBundleShard"}`,
	})

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/runs/123/artifacts": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"artifacts": []map[string]interface{}{
					{"id": 10, "name": "target-artifact", "size_in_bytes": 100},
				},
			})
		},
		"/repos/sourceplane/orun/actions/artifacts/10/zip": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/zip")
			w.Write(zipBuf)
		},
	})
	defer server.Close()

	ds, err := client.DownloadByName(ctx, 123, "target-artifact", destDir)
	if err != nil {
		t.Fatalf("DownloadByName failed: %v", err)
	}
	if ds.Name != "target-artifact" {
		t.Errorf("Name = %q, want %q", ds.Name, "target-artifact")
	}
}

func TestDownloadByName_NotFound(t *testing.T) {
	ctx := context.Background()

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/runs/123/artifacts": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"artifacts": []map[string]interface{}{},
			})
		},
	})
	defer server.Close()

	_, err := client.DownloadByName(ctx, 123, "nonexistent", t.TempDir())
	if err == nil {
		t.Fatal("expected error for nonexistent artifact")
	}
}

func TestDownload_PathTraversalRejected(t *testing.T) {
	ctx := context.Background()

	// Zip with path traversal
	zipBuf := zipArchive(t, map[string]string{
		"../../etc/passwd": "root:x:0:0:root",
	})

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/artifacts/1/zip": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/zip")
			w.Write(zipBuf)
		},
	})
	defer server.Close()

	_, err := client.Download(ctx, artifactstore.RemoteShard{Name: "bad", ID: "1"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for path traversal in zip")
	}
}

func TestResolveRun_ByRunID(t *testing.T) {
	ctx := context.Background()

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/runs/42": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 42, "name": "CI", "head_sha": "abc123",
				"head_branch": "main", "status": "completed", "conclusion": "success",
			})
		},
	})
	defer server.Close()

	run, err := ResolveRun(ctx, client, ResolveOpts{RunID: 42})
	if err != nil {
		t.Fatalf("ResolveRun failed: %v", err)
	}
	if run.ID != 42 {
		t.Errorf("ID = %d, want 42", run.ID)
	}
}

func TestResolveRun_ByExecID(t *testing.T) {
	ctx := context.Background()

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/runs/99": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"id": 99, "head_sha": "xyz789", "head_branch": "feature",
			})
		},
	})
	defer server.Close()

	run, err := ResolveRun(ctx, client, ResolveOpts{ExecID: "gh-99-1-abc123"})
	if err != nil {
		t.Fatalf("ResolveRun failed: %v", err)
	}
	if run.ID != 99 {
		t.Errorf("ID = %d, want 99", run.ID)
	}
}

func TestResolveRun_BySHA(t *testing.T) {
	ctx := context.Background()

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/runs": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("head_sha") != "abc123def456" {
				t.Errorf("unexpected sha: %s", r.URL.Query().Get("head_sha"))
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"workflow_runs": []map[string]interface{}{
					{"id": 100, "head_sha": "abc123def456", "head_branch": "main"},
				},
			})
		},
	})
	defer server.Close()

	run, err := ResolveRun(ctx, client, ResolveOpts{SHA: "abc123def456"})
	if err != nil {
		t.Fatalf("ResolveRun failed: %v", err)
	}
	if run.ID != 100 {
		t.Errorf("ID = %d, want 100", run.ID)
	}
}

func TestResolveRun_Failed(t *testing.T) {
	ctx := context.Background()

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/runs": func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Query().Get("status") != "failure" {
				t.Errorf("expected status=failure, got %s", r.URL.Query().Get("status"))
			}
			json.NewEncoder(w).Encode(map[string]interface{}{
				"workflow_runs": []map[string]interface{}{
					{"id": 200, "conclusion": "failure"},
				},
			})
		},
	})
	defer server.Close()

	run, err := ResolveRun(ctx, client, ResolveOpts{Failed: true})
	if err != nil {
		t.Fatalf("ResolveRun failed: %v", err)
	}
	if run.ID != 200 {
		t.Errorf("ID = %d, want 200", run.ID)
	}
}

func TestResolveRun_InvalidExecID(t *testing.T) {
	ctx := context.Background()
	client, _ := NewClient(ctx, "owner/repo", WithToken("test"))

	_, err := ResolveRun(ctx, client, ResolveOpts{ExecID: "invalid-format"})
	if err == nil {
		t.Fatal("expected error for invalid exec-id")
	}
}

func TestResolveRun_NoRunsFound(t *testing.T) {
	ctx := context.Background()

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/runs": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"workflow_runs": []map[string]interface{}{},
			})
		},
	})
	defer server.Close()

	_, err := ResolveRun(ctx, client, ResolveOpts{})
	if err == nil {
		t.Fatal("expected error when no runs found")
	}
}

func TestResolveRun_BySHA_NoResults(t *testing.T) {
	ctx := context.Background()

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/runs": func(w http.ResponseWriter, r *http.Request) {
			json.NewEncoder(w).Encode(map[string]interface{}{
				"workflow_runs": []map[string]interface{}{},
			})
		},
	})
	defer server.Close()

	_, err := ResolveRun(ctx, client, ResolveOpts{SHA: "nonexistent"})
	if err == nil {
		t.Fatal("expected error for SHA with no runs")
	}
}

// Helpers

// zipArchive creates a zip archive in memory from a map of filename -> content.
func zipArchive(t *testing.T, files map[string]string) []byte {
	t.Helper()
	var buf strings.Builder
	zw := zip.NewWriter(&buf)
	for name, content := range files {
		w, err := zw.Create(name)
		if err != nil {
			t.Fatalf("zip create %s: %v", name, err)
		}
		if _, err := w.Write([]byte(content)); err != nil {
			t.Fatalf("zip write %s: %v", name, err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("zip close: %v", err)
	}
	return []byte(buf.String())
}

// Ensure download.go compiles with readerAt
var _ = fmt.Sprintf("compile check")