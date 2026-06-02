package objremote

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

type fmtError string

func (e fmtError) Error() string { return string(e) }

const errBoom = fmtError("boom")

// Store wrappers that fail a single operation.
type putBlobFail struct{ objectstore.ObjectStore }

func (putBlobFail) PutBlob(context.Context, []byte) (objectstore.ObjectID, error) {
	return "", errBoom
}

type putTreeFail struct{ objectstore.ObjectStore }

func (putTreeFail) PutTree(context.Context, []objectstore.TreeEntry) (objectstore.ObjectID, error) {
	return "", errBoom
}

type hasFail struct{ objectstore.ObjectStore }

func (hasFail) Has(context.Context, objectstore.ObjectID) (bool, error) { return false, errBoom }

type walkFail struct{ objectstore.ObjectStore }

func (walkFail) Walk(context.Context, objectstore.ObjectID, func(objectstore.ObjectID, objectstore.Kind) error) error {
	return errBoom
}

func TestSyncStoreErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	local, _ := endpoint(t), endpoint(t)
	seal(t, local, "exec_001")

	// Walk error on the source.
	wf := Endpoint{Objects: walkFail{local.Objects}, Refs: local.Refs}
	if _, err := Sync(ctx, wf, endpoint(t), "executions/latest"); err == nil {
		t.Fatalf("expected walk error")
	}
	// Has error on the destination.
	dstHas := Endpoint{Objects: hasFail{endpoint(t).Objects}, Refs: endpoint(t).Refs}
	if _, err := Sync(ctx, local, dstHas, "executions/latest"); err == nil {
		t.Fatalf("expected dest has error")
	}
	// PutBlob error on the destination.
	dstPB := Endpoint{Objects: putBlobFail{endpoint(t).Objects}, Refs: endpoint(t).Refs}
	if _, err := Sync(ctx, local, dstPB, "executions/latest"); err == nil {
		t.Fatalf("expected dest put-blob error")
	}
	// PutTree error on the destination.
	dstPT := Endpoint{Objects: putTreeFail{endpoint(t).Objects}, Refs: endpoint(t).Refs}
	if _, err := Sync(ctx, local, dstPT, "executions/latest"); err == nil {
		t.Fatalf("expected dest put-tree error")
	}
}

func TestCopyObjectGetError(t *testing.T) {
	t.Parallel()
	from := objectstore.NewMemStore("")
	absent := objectstore.ObjectID("sha256:" + "0000000000000000000000000000000000000000000000000000000000000000")
	if err := copyObject(context.Background(), from, objectstore.NewMemStore(""), absent); err == nil {
		t.Fatalf("expected get error for absent object")
	}
}

// fakeRefs is a programmable ref store for exercising moveRef branches.
type fakeRefs struct {
	cur      string
	found    bool
	readErr  error
	onUpdate func(name, old, new string) error
}

func (f *fakeRefs) Read(context.Context, string) (refstore.Ref, error) {
	if f.readErr != nil {
		return refstore.Ref{}, f.readErr
	}
	if !f.found {
		return refstore.Ref{}, refstore.ErrNotFound
	}
	return refstore.Ref{Target: f.cur}, nil
}
func (f *fakeRefs) Update(_ context.Context, name, old, new string) error {
	if f.onUpdate != nil {
		return f.onUpdate(name, old, new)
	}
	f.cur, f.found = new, true
	return nil
}
func (f *fakeRefs) List(context.Context, string) ([]string, error) { return nil, nil }
func (f *fakeRefs) Delete(context.Context, string) error           { return nil }

func TestMoveRefBranches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()

	// Already at target → no move.
	at := &fakeRefs{cur: "sha256:x", found: true}
	if moved, err := moveRef(ctx, at, "r", "sha256:x"); err != nil || moved {
		t.Fatalf("already-at-target = %v,%v", moved, err)
	}
	// Read error (non-NotFound).
	re := &fakeRefs{readErr: errBoom}
	if _, err := moveRef(ctx, re, "r", "sha256:x"); err == nil {
		t.Fatalf("expected read error")
	}
	// Conflict once, then succeed.
	n := 0
	ct := &fakeRefs{onUpdate: func(string, string, string) error {
		n++
		if n == 1 {
			return refstore.ErrConflict
		}
		return nil
	}}
	if moved, err := moveRef(ctx, ct, "r", "sha256:x"); err != nil || !moved {
		t.Fatalf("conflict-then-success = %v,%v", moved, err)
	}
	// Always conflict → too many.
	ac := &fakeRefs{onUpdate: func(string, string, string) error { return refstore.ErrConflict }}
	if _, err := moveRef(ctx, ac, "r", "sha256:x"); err == nil {
		t.Fatalf("expected too-many-conflicts")
	}
	// Generic update error.
	ue := &fakeRefs{onUpdate: func(string, string, string) error { return errBoom }}
	if _, err := moveRef(ctx, ue, "r", "sha256:x"); err == nil {
		t.Fatalf("expected update error")
	}
}
