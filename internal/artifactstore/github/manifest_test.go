package github

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/sourceplane/orun/internal/artifactstore"
	"github.com/sourceplane/orun/internal/runbundle"
)

func TestDownloadManifestOnly_Success(t *testing.T) {
	ctx := context.Background()

	manifest := runbundle.RunBundleShardManifest{
		APIVersion:    "orun.io/v1alpha1",
		Kind:          "RunBundleShard",
		SchemaVersion: "1.0.0",
		Role:          runbundle.ShardRoleJob,
		ExecID:        "gh-100-1-abc",
		PlanID:        "plan123",
		JobUID:        "uid1",
		JobID:         "supabase-stage",
		Component:     "supabase",
		Environment:   "stage",
		Status:        "completed",
		Files:         map[string]string{"state": "state.json"},
	}
	manifestJSON, _ := json.Marshal(manifest)

	zipBuf := zipArchive(t, map[string]string{
		"manifest.json": string(manifestJSON),
		"state.json":    `{"status":"completed"}`,
	})

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/artifacts/50/zip": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/zip")
			w.Write(zipBuf)
		},
	})
	defer server.Close()

	detail, err := client.DownloadManifestOnly(ctx, artifactstore.RemoteShard{
		Name: "orun.v1.gh-100-1-abc.job.uid1.completed",
		ID:   "50",
	})
	if err != nil {
		t.Fatalf("DownloadManifestOnly failed: %v", err)
	}

	if detail.ShardName != "orun.v1.gh-100-1-abc.job.uid1.completed" {
		t.Errorf("ShardName = %q", detail.ShardName)
	}
	if detail.Manifest.Role != runbundle.ShardRoleJob {
		t.Errorf("Role = %q, want job", detail.Manifest.Role)
	}
	if detail.Manifest.ExecID != "gh-100-1-abc" {
		t.Errorf("ExecID = %q", detail.Manifest.ExecID)
	}
	if detail.Manifest.Status != "completed" {
		t.Errorf("Status = %q, want completed", detail.Manifest.Status)
	}
	if detail.Manifest.Component != "supabase" {
		t.Errorf("Component = %q, want supabase", detail.Manifest.Component)
	}
	if detail.Manifest.JobID != "supabase-stage" {
		t.Errorf("JobID = %q, want supabase-stage", detail.Manifest.JobID)
	}
}

func TestDownloadManifestOnly_PlanShard(t *testing.T) {
	ctx := context.Background()

	manifest := runbundle.RunBundleShardManifest{
		APIVersion:    "orun.io/v1alpha1",
		Kind:          "RunBundleShard",
		SchemaVersion: "1.0.0",
		Role:          runbundle.ShardRolePlan,
		ExecID:        "gh-100-1-abc",
		PlanID:        "plan123",
		Files:         map[string]string{"plan": "plan.json"},
	}
	manifestJSON, _ := json.Marshal(manifest)

	zipBuf := zipArchive(t, map[string]string{
		"manifest.json": string(manifestJSON),
		"plan.json":     `{"apiVersion":"orun/v1","kind":"Plan"}`,
	})

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/artifacts/51/zip": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/zip")
			w.Write(zipBuf)
		},
	})
	defer server.Close()

	detail, err := client.DownloadManifestOnly(ctx, artifactstore.RemoteShard{
		Name: "orun.v1.gh-100-1-abc.plan.abc.created",
		ID:   "51",
	})
	if err != nil {
		t.Fatalf("DownloadManifestOnly failed: %v", err)
	}

	if detail.Manifest.Role != runbundle.ShardRolePlan {
		t.Errorf("Role = %q, want plan", detail.Manifest.Role)
	}
}

func TestDownloadManifestOnly_DownloadError(t *testing.T) {
	ctx := context.Background()

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/artifacts/52/zip": func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusNotFound)
		},
	})
	defer server.Close()

	_, err := client.DownloadManifestOnly(ctx, artifactstore.RemoteShard{
		Name: "orun.v1.gh-100-1-abc.job.uid1.completed",
		ID:   "52",
	})
	if err == nil {
		t.Fatal("expected error for failed download")
	}
}

func TestDownloadManifestOnly_InvalidManifest(t *testing.T) {
	ctx := context.Background()

	zipBuf := zipArchive(t, map[string]string{
		"manifest.json": `{not valid json}`,
	})

	client, server := setupTestServer(t, map[string]func(w http.ResponseWriter, r *http.Request){
		"/repos/sourceplane/orun/actions/artifacts/53/zip": func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/zip")
			w.Write(zipBuf)
		},
	})
	defer server.Close()

	_, err := client.DownloadManifestOnly(ctx, artifactstore.RemoteShard{
		Name: "orun.v1.gh-100-1-abc.job.uid1.completed",
		ID:   "53",
	})
	// Should still succeed if ds.Shard is nil but ReadShardManifest also fails
	if err == nil {
		t.Fatal("expected error for invalid manifest JSON")
	}
}
