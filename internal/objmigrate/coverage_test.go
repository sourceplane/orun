package objmigrate

import (
	"context"
	"os"
	"testing"

	"github.com/sourceplane/orun/internal/objectstore"
)

type fmtError string

func (e fmtError) Error() string { return string(e) }

const errBoom = fmtError("boom")

// putFailStore makes the revision write fail.
type putFailStore struct{ objectstore.ObjectStore }

func (putFailStore) PutBlob(context.Context, []byte) (objectstore.ObjectID, error) {
	return "", errBoom
}

func TestMigrateSkipsUnreadableExecution(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	legacy, _, execID := legacyWith(t, true)
	// Corrupt the execution's state.json so LoadState fails; it must be skipped.
	if err := os.WriteFile(legacy.StatePath(execID), []byte("{not json"), 0o644); err != nil {
		t.Fatalf("corrupt: %v", err)
	}
	store, refs := objstores(t)
	res, err := Migrate(ctx, legacy, store, refs, false)
	if err != nil {
		t.Fatalf("Migrate: %v", err)
	}
	if res.Plans != 1 || res.Executions != 0 {
		t.Fatalf("result = %+v (expected the unreadable execution skipped)", res)
	}
}

func TestMigrateWriteErrorPropagates(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	legacy, _, _ := legacyWith(t, false) // a plan, no executions
	store, refs := objstores(t)
	if _, err := Migrate(ctx, legacy, putFailStore{store}, refs, false); err == nil {
		t.Fatalf("expected revision write error")
	}
}
