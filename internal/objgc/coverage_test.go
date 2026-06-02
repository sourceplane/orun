package objgc

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

type fmtError string

func (e fmtError) Error() string { return string(e) }

const errBoom = fmtError("boom")

// fakeRefs is a programmable ref store for exercising error branches.
type fakeRefs struct {
	listResult []string
	listErr    error
	readTarget string
	readErr    error
	deleteErr  error
}

func (f fakeRefs) Read(context.Context, string) (refstore.Ref, error) {
	if f.readErr != nil {
		return refstore.Ref{}, f.readErr
	}
	return refstore.Ref{Target: f.readTarget}, nil
}
func (f fakeRefs) Update(context.Context, string, string, string) error { return nil }
func (f fakeRefs) List(context.Context, string) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.listResult, nil
}
func (f fakeRefs) Delete(context.Context, string) error { return f.deleteErr }

// deleteFailStore wraps a store and fails Delete.
type deleteFailStore struct{ objectstore.ObjectStore }

func (deleteFailStore) Delete(context.Context, objectstore.ObjectID) error { return errBoom }

func TestCollectListRefError(t *testing.T) {
	t.Parallel()
	store, _, ix, _ := rig(t)
	if _, err := Collect(context.Background(), store, fakeRefs{listErr: errBoom}, ix, Options{}); err == nil {
		t.Fatalf("expected list error")
	}
}

func TestCollectSweepDeleteError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, refs, ix, _ := rig(t)
	if _, err := store.PutBlob(ctx, []byte("orphan")); err != nil {
		t.Fatalf("PutBlob: %v", err)
	}
	if _, err := Collect(ctx, deleteFailStore{store}, refs, ix, Options{}); err == nil {
		t.Fatalf("expected sweep delete error")
	}
}

func TestPruneDeleteError(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store, _, ix, sealer := rig(t)
	sealExec(t, sealer, "exec_001", time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC))
	sealExec(t, sealer, "exec_002", time.Date(2026, 6, 2, 11, 0, 0, 0, time.UTC))
	// ix uses the real refs (BuildExecutions sees 2 execs); Collect's refs is the
	// fake that errors when prune tries to delete the non-kept ref.
	fr := fakeRefs{
		listResult: []string{"executions/by-id/exec_001"},
		readTarget: "sha256:" + strings.Repeat("9", 64),
		deleteErr:  errBoom,
	}
	if _, err := Collect(ctx, store, fr, ix, Options{KeepExecutions: 1}); err == nil {
		t.Fatalf("expected prune delete error")
	}
}
