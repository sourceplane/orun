package objindex

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/execseal"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

func revID() objectstore.ObjectID {
	return objectstore.ObjectID("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
}

func rig(t *testing.T) (*Indexer, *execseal.Sealer, string) {
	t.Helper()
	root := t.TempDir()
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Clock: clock.Fixed{}})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	w := nodewriter.New(store, refs)
	return New(store, refs, root), execseal.New(w), root
}

func sealExec(t *testing.T, s *execseal.Sealer, execID, status string, started time.Time) {
	t.Helper()
	_, err := s.Seal(context.Background(), execseal.SealInput{
		RevisionID:   revID(),
		ExecutionID:  execID,
		ExecutionKey: execID,
		Status:       status,
		StartedAt:    started,
		Jobs: []nodes.JobInput{{
			Record:   nodes.JobRun{JobID: "a@deploy", Folder: "j-1", Status: status},
			Attempts: []nodes.AttemptInput{{Record: nodes.JobAttempt{Attempt: 1, Status: status}}},
		}},
	})
	if err != nil {
		t.Fatalf("Seal %s: %v", execID, err)
	}
}

func TestBuildExecutionsNewestFirst(t *testing.T) {
	t.Parallel()
	ix, sealer, _ := rig(t)
	ctx := context.Background()
	sealExec(t, sealer, "exec_001", nodes.StatusSucceeded, time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC))
	sealExec(t, sealer, "exec_002", nodes.StatusFailed, time.Date(2026, 6, 2, 11, 0, 0, 0, time.UTC))

	entries, err := ix.BuildExecutions(ctx)
	if err != nil {
		t.Fatalf("BuildExecutions: %v", err)
	}
	if len(entries) != 2 {
		t.Fatalf("entries = %d, want 2", len(entries))
	}
	// Newest (11:00, exec_002) first.
	if entries[0].ExecutionID != "exec_002" || entries[0].Status != nodes.StatusFailed {
		t.Fatalf("entry[0] = %+v", entries[0])
	}
	if entries[1].ExecutionID != "exec_001" {
		t.Fatalf("entry[1] = %+v", entries[1])
	}
	if entries[0].RevisionID != string(revID()) || entries[0].StartedAt != "2026-06-02T11:00:00Z" {
		t.Fatalf("entry[0] fields = %+v", entries[0])
	}
	if entries[0].Summary.JobsTotal != 1 {
		t.Fatalf("summary = %+v", entries[0].Summary)
	}
}

func TestReindexAndListFromCache(t *testing.T) {
	t.Parallel()
	ix, sealer, _ := rig(t)
	ctx := context.Background()
	sealExec(t, sealer, "exec_001", nodes.StatusSucceeded, time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC))

	// Before reindex, ListExecutions falls back to the walk.
	walk, err := ix.ListExecutions(ctx)
	if err != nil || len(walk) != 1 {
		t.Fatalf("walk list = %v, %v", walk, err)
	}

	if err := ix.Reindex(ctx); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	// The cache file now exists and ListExecutions reads it.
	if _, err := os.Stat(ix.allExecutionsPath()); err != nil {
		t.Fatalf("index file missing: %v", err)
	}
	cached, err := ix.ListExecutions(ctx)
	if err != nil || len(cached) != 1 || cached[0].ExecutionID != "exec_001" {
		t.Fatalf("cached list = %v, %v", cached, err)
	}

	// Reindex is deterministic (byte-identical output).
	first, _ := os.ReadFile(ix.allExecutionsPath())
	if err := ix.Reindex(ctx); err != nil {
		t.Fatalf("Reindex 2: %v", err)
	}
	second, _ := os.ReadFile(ix.allExecutionsPath())
	if string(first) != string(second) {
		t.Fatalf("reindex not deterministic")
	}
}

func TestListExecutionsCorruptCacheFallsBack(t *testing.T) {
	t.Parallel()
	ix, sealer, _ := rig(t)
	ctx := context.Background()
	sealExec(t, sealer, "exec_001", nodes.StatusSucceeded, time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC))
	if err := ix.Reindex(ctx); err != nil {
		t.Fatalf("Reindex: %v", err)
	}
	// Corrupt the cache; ListExecutions must fall back to the walk.
	if err := os.WriteFile(ix.allExecutionsPath(), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	got, err := ix.ListExecutions(ctx)
	if err != nil || len(got) != 1 {
		t.Fatalf("fallback list = %v, %v", got, err)
	}
}

func TestBuildExecutionsEmpty(t *testing.T) {
	t.Parallel()
	ix, _, _ := rig(t)
	entries, err := ix.BuildExecutions(context.Background())
	if err != nil {
		t.Fatalf("BuildExecutions: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("empty store entries = %d", len(entries))
	}
}

func TestReadExecutionErrors(t *testing.T) {
	t.Parallel()
	ix, _, _ := rig(t)
	ctx := context.Background()
	// A ref pointing at a non-execution tree (no execution.json) → error.
	blob, _ := ix.store.PutBlob(ctx, []byte("x"))
	tree, _ := ix.store.PutTree(ctx, []objectstore.TreeEntry{{Name: "other.json", Kind: objectstore.KindBlob, ID: blob}})
	if err := ix.refs.Update(ctx, "executions/by-id/bad", "", string(tree)); err != nil {
		t.Fatalf("ref: %v", err)
	}
	if _, err := ix.BuildExecutions(ctx); err == nil {
		t.Fatalf("expected error for ref pointing at non-execution tree")
	}
}

func TestFormatTimeHelpers(t *testing.T) {
	t.Parallel()
	if formatTime(time.Time{}) != "" {
		t.Fatalf("zero time should format empty")
	}
	if formatTimePtr(nil) != "" {
		t.Fatalf("nil ptr should format empty")
	}
	now := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	if formatTime(now) != "2026-06-02T10:00:00Z" || formatTimePtr(&now) != "2026-06-02T10:00:00Z" {
		t.Fatalf("format wrong")
	}
}
