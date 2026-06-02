package objindex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

func TestSortTiebreakSameTime(t *testing.T) {
	t.Parallel()
	ix, sealer, _ := rig(t)
	same := time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC)
	sealExec(t, sealer, "exec_a", nodes.StatusSucceeded, same)
	sealExec(t, sealer, "exec_b", nodes.StatusSucceeded, same)
	entries, err := ix.BuildExecutions(context.Background())
	if err != nil {
		t.Fatalf("BuildExecutions: %v", err)
	}
	// Equal StartedAt → tiebreak by ExecutionID descending.
	if entries[0].ExecutionID != "exec_b" || entries[1].ExecutionID != "exec_a" {
		t.Fatalf("tiebreak order = %s,%s", entries[0].ExecutionID, entries[1].ExecutionID)
	}
}

func TestReindexRenameError(t *testing.T) {
	t.Parallel()
	ix, sealer, _ := rig(t)
	ctx := context.Background()
	sealExec(t, sealer, "exec_001", nodes.StatusSucceeded, time.Now())
	// Make the index file path a non-empty directory so the atomic rename fails.
	p := ix.allExecutionsPath()
	if err := os.MkdirAll(filepath.Join(p, "child"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := ix.Reindex(ctx); err == nil {
		t.Fatalf("Reindex should fail when the index path is a non-empty dir")
	}
}

func TestBuildExecutionsAbsentTarget(t *testing.T) {
	t.Parallel()
	ix, _, _ := rig(t)
	ctx := context.Background()
	absent := "sha256:" + strings.Repeat("e", 64)
	if err := ix.refs.Update(ctx, "executions/by-id/x", "", absent); err != nil {
		t.Fatalf("ref: %v", err)
	}
	if _, err := ix.BuildExecutions(ctx); err == nil {
		t.Fatalf("expected error for ref pointing at absent object")
	}
}

func TestReadExecutionGetError(t *testing.T) {
	t.Parallel()
	ix, _, _ := rig(t)
	// GetTree on a blob id (not a tree) errors.
	blob, _ := ix.store.PutBlob(context.Background(), []byte("x"))
	if _, err := ix.readExecution(context.Background(), blob); err == nil {
		t.Fatalf("readExecution on a blob should error")
	}
	_ = objectstore.KindBlob
}

// fakeRefs is a programmable ref store for exercising List/Read error branches.
type fakeRefs struct {
	names   []string
	listErr error
	readErr error
}

func (f fakeRefs) Read(context.Context, string) (refstore.Ref, error) {
	if f.readErr != nil {
		return refstore.Ref{}, f.readErr
	}
	return refstore.Ref{Target: "sha256:" + strings.Repeat("a", 64)}, nil
}
func (f fakeRefs) Update(context.Context, string, string, string) error { return nil }
func (f fakeRefs) List(context.Context, string) ([]string, error) {
	if f.listErr != nil {
		return nil, f.listErr
	}
	return f.names, nil
}
func (f fakeRefs) Delete(context.Context, string) error { return nil }

func TestBuildExecutionsRefErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	listFail := &Indexer{store: objectstore.NewMemStore(""), refs: fakeRefs{listErr: errBoom}, dir: t.TempDir()}
	if _, err := listFail.BuildExecutions(ctx); err == nil {
		t.Fatalf("expected list error")
	}
	if err := listFail.Reindex(ctx); err == nil {
		t.Fatalf("expected Reindex to surface the list error")
	}
	readFail := &Indexer{store: objectstore.NewMemStore(""), refs: fakeRefs{names: []string{"executions/by-id/x"}, readErr: errBoom}, dir: t.TempDir()}
	if _, err := readFail.BuildExecutions(ctx); err == nil {
		t.Fatalf("expected read error")
	}
}

func TestReindexMkdirError(t *testing.T) {
	t.Parallel()
	ix, sealer, root := rig(t)
	ctx := context.Background()
	sealExec(t, sealer, "exec_001", nodes.StatusSucceeded, time.Now())
	// Block index/executions with a regular file so MkdirAll fails.
	if err := os.MkdirAll(filepath.Join(root, "index"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "index", "executions"), []byte("x"), 0o644); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := ix.Reindex(ctx); err == nil {
		t.Fatalf("Reindex should fail when index/executions is a file")
	}
}

var errBoom = fmtError("boom")

type fmtError string

func (e fmtError) Error() string { return string(e) }
