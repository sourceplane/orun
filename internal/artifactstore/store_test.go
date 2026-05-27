package artifactstore_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/sourceplane/orun/internal/artifactstore"
	"github.com/sourceplane/orun/internal/artifactstore/memory"
	"github.com/sourceplane/orun/internal/runbundle"
)

func TestInMemoryStore_UploadAndList(t *testing.T) {
	ctx := context.Background()
	store := memory.New()

	shard := &runbundle.Shard{
		ExecID: "gh-1-1-abc",
		Role:   runbundle.ShardRolePlan,
		Suffix: "abc123",
		Status: "created",
		Manifest: &runbundle.RunBundleShardManifest{
			APIVersion:    "orun.io/v1alpha1",
			Kind:          "RunBundleShard",
			SchemaVersion: "1.0.0",
			Role:          runbundle.ShardRolePlan,
			ExecID:        "gh-1-1-abc",
			PlanID:        "abc123",
		},
	}

	result, err := store.Upload(ctx, shard)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}
	if result.Name == "" {
		t.Error("expected non-empty result name")
	}
	if result.ID == "" {
		t.Error("expected non-empty result ID")
	}

	// List all
	shards, err := store.List(ctx, artifactstore.ListOptions{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(shards) != 1 {
		t.Errorf("List returned %d shards, want 1", len(shards))
	}

	// List with prefix
	shards, err = store.List(ctx, artifactstore.ListOptions{Prefix: "orun.v1.gh-1-1-abc"})
	if err != nil {
		t.Fatalf("List with prefix failed: %v", err)
	}
	if len(shards) != 1 {
		t.Errorf("List with prefix returned %d shards, want 1", len(shards))
	}

	// List with non-matching prefix
	shards, err = store.List(ctx, artifactstore.ListOptions{Prefix: "nonexistent"})
	if err != nil {
		t.Fatalf("List with non-matching prefix failed: %v", err)
	}
	if len(shards) != 0 {
		t.Errorf("List with non-matching prefix returned %d shards, want 0", len(shards))
	}
}

func TestInMemoryStore_UploadAndDownload(t *testing.T) {
	ctx := context.Background()
	store := memory.New()

	shard := &runbundle.Shard{
		ExecID: "gh-1-1-abc",
		Role:   runbundle.ShardRoleJob,
		Suffix: "uid-1",
		Status: "completed",
		Manifest: &runbundle.RunBundleShardManifest{
			APIVersion:    "orun.io/v1alpha1",
			Kind:          "RunBundleShard",
			SchemaVersion: "1.0.0",
			Role:          runbundle.ShardRoleJob,
			ExecID:        "gh-1-1-abc",
			PlanID:        "abc123",
			JobUID:        "uid-1",
			Status:        "completed",
			Files:         map[string]string{"manifest": "manifest.json"},
		},
	}

	_, err := store.Upload(ctx, shard)
	if err != nil {
		t.Fatalf("Upload failed: %v", err)
	}

	// List and download
	shards, err := store.List(ctx, artifactstore.ListOptions{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(shards) == 0 {
		t.Fatal("no shards found")
	}

	ds, err := store.Download(ctx, shards[0], t.TempDir())
	if err != nil {
		t.Fatalf("Download failed: %v", err)
	}
	if ds.Name != shards[0].Name {
		t.Errorf("Name = %q, want %q", ds.Name, shards[0].Name)
	}
	if ds.Shard == nil {
		t.Error("expected non-nil Shard")
	}
}

func TestInMemoryStore_UploadNilShard(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	_, err := store.Upload(ctx, nil)
	if err == nil {
		t.Fatal("expected error for nil shard")
	}
}

func TestInMemoryStore_DownloadNonExistent(t *testing.T) {
	ctx := context.Background()
	store := memory.New()
	_, err := store.Download(ctx, artifactstore.RemoteShard{Name: "nonexistent"}, t.TempDir())
	if err == nil {
		t.Fatal("expected error for nonexistent shard")
	}
}

func TestInMemoryStore_ListWithMultipleShards(t *testing.T) {
	ctx := context.Background()
	store := memory.New()

	for i := 0; i < 5; i++ {
		suffix := fmt.Sprintf("suffix-%d", i)
		store.Upload(ctx, &runbundle.Shard{
			ExecID: "gh-1-1-test",
			Role:   runbundle.ShardRoleJob,
			Suffix: suffix,
			Status: "completed",
			Manifest: &runbundle.RunBundleShardManifest{
				ExecID: "gh-1-1-test",
				Role:   runbundle.ShardRoleJob,
			},
		})
	}

	shards, err := store.List(ctx, artifactstore.ListOptions{})
	if err != nil {
		t.Fatalf("List failed: %v", err)
	}
	if len(shards) != 5 {
		t.Errorf("List returned %d shards, want 5", len(shards))
	}
}

func TestInMemoryStore_Interface(t *testing.T) {
	// Verify memory store satisfies the Store interface
	var store artifactstore.Store
	store = memory.New()
	_ = store
}