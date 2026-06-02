package objgc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/execseal"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objindex"
)

func revID() objectstore.ObjectID {
	return objectstore.ObjectID("sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa")
}

func rig(t *testing.T) (*objectstore.LocalStore, *refstore.LocalRefStore, *objindex.Indexer, *execseal.Sealer) {
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
	return store, refs, objindex.New(store, refs, root), execseal.New(nodewriter.New(store, refs))
}

func sealExec(t *testing.T, s *execseal.Sealer, execID string, started time.Time) objectstore.ObjectID {
	t.Helper()
	id, err := s.Seal(context.Background(), execseal.SealInput{
		RevisionID: revID(), ExecutionID: execID, ExecutionKey: execID,
		Status: nodes.StatusSucceeded, StartedAt: started,
		Jobs: []nodes.JobInput{{Record: nodes.JobRun{JobID: execID, Folder: "j-" + execID, Status: nodes.StatusSucceeded},
			Attempts: []nodes.AttemptInput{{Record: nodes.JobAttempt{Attempt: 1, Status: nodes.StatusSucceeded}}}}},
	})
	if err != nil {
		t.Fatalf("Seal %s: %v", execID, err)
	}
	return id
}

func TestCollectSweepsOrphanKeepsReachable(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, ix, sealer := rig(t)
	execID := sealExec(t, sealer, "exec_001", time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC))
	orphan, _ := store.PutBlob(ctx, []byte("unreachable orphan"))

	res, err := Collect(ctx, store, refs, ix, Options{})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if res.Swept < 1 {
		t.Fatalf("expected at least one sweep, got %+v", res)
	}
	if has, _ := store.Has(ctx, orphan); has {
		t.Fatalf("orphan not swept")
	}
	if has, _ := store.Has(ctx, execID); !has {
		t.Fatalf("reachable execution was swept")
	}
}

func TestCollectGraceWindow(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, ix, _ := rig(t)
	orphan, _ := store.PutBlob(ctx, []byte("young orphan"))

	// Young object within the grace window is skipped.
	res, err := Collect(ctx, store, refs, ix, Options{GracePeriod: time.Hour, Now: time.Now()})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if res.Skipped < 1 {
		t.Fatalf("expected grace-skipped object, got %+v", res)
	}
	if has, _ := store.Has(ctx, orphan); !has {
		t.Fatalf("young orphan was swept despite grace")
	}

	// Past the grace window it is swept.
	res2, err := Collect(ctx, store, refs, ix, Options{GracePeriod: time.Hour, Now: time.Now().Add(2 * time.Hour)})
	if err != nil {
		t.Fatalf("Collect 2: %v", err)
	}
	if res2.Swept < 1 {
		t.Fatalf("expected sweep past grace, got %+v", res2)
	}
	if has, _ := store.Has(ctx, orphan); has {
		t.Fatalf("orphan not swept past grace window")
	}
}

func TestCollectRetention(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, ix, sealer := rig(t)
	old := sealExec(t, sealer, "exec_001", time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC))
	sealExec(t, sealer, "exec_002", time.Date(2026, 6, 2, 11, 0, 0, 0, time.UTC))
	newest := sealExec(t, sealer, "exec_003", time.Date(2026, 6, 2, 12, 0, 0, 0, time.UTC))

	res, err := Collect(ctx, store, refs, ix, Options{KeepExecutions: 2})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if res.PrunedExecRefs != 1 {
		t.Fatalf("PrunedExecRefs = %d, want 1", res.PrunedExecRefs)
	}
	// The oldest execution's by-id ref is gone and its object swept.
	if _, err := refs.Read(ctx, "executions/by-id/exec_001"); !errors.Is(err, refstore.ErrNotFound) {
		t.Fatalf("oldest by-id ref still present: %v", err)
	}
	if has, _ := store.Has(ctx, old); has {
		t.Fatalf("pruned execution object not swept")
	}
	// The newest two remain reachable.
	if has, _ := store.Has(ctx, newest); !has {
		t.Fatalf("newest execution was swept")
	}
}

func TestCollectDryRun(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, ix, _ := rig(t)
	orphan, _ := store.PutBlob(ctx, []byte("orphan"))

	res, err := Collect(ctx, store, refs, ix, Options{DryRun: true})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if !res.DryRun || res.Swept < 1 {
		t.Fatalf("dry-run result = %+v", res)
	}
	if has, _ := store.Has(ctx, orphan); !has {
		t.Fatalf("dry-run deleted an object")
	}
}

func TestCollectRetentionDryRunKeepsRefs(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, ix, sealer := rig(t)
	sealExec(t, sealer, "exec_001", time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC))
	sealExec(t, sealer, "exec_002", time.Date(2026, 6, 2, 11, 0, 0, 0, time.UTC))

	res, err := Collect(ctx, store, refs, ix, Options{KeepExecutions: 1, DryRun: true})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if res.PrunedExecRefs != 1 {
		t.Fatalf("PrunedExecRefs = %d, want 1", res.PrunedExecRefs)
	}
	// Dry-run must not actually delete the ref.
	if _, err := refs.Read(ctx, "executions/by-id/exec_001"); err != nil {
		t.Fatalf("dry-run pruned the ref: %v", err)
	}
}

func TestCollectRetentionNoopWhenUnderKeep(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, ix, sealer := rig(t)
	sealExec(t, sealer, "exec_001", time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC))
	res, err := Collect(ctx, store, refs, ix, Options{KeepExecutions: 5})
	if err != nil {
		t.Fatalf("Collect: %v", err)
	}
	if res.PrunedExecRefs != 0 {
		t.Fatalf("nothing should be pruned, got %d", res.PrunedExecRefs)
	}
}
