package objremote

import (
	"context"
	"fmt"
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

func endpoint(t *testing.T) Endpoint {
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
	return Endpoint{Objects: store, Refs: refs}
}

func seal(t *testing.T, ep Endpoint, execID string) {
	t.Helper()
	_, err := execseal.New(nodewriter.New(ep.Objects, ep.Refs)).Seal(context.Background(), execseal.SealInput{
		RevisionID: revID(), ExecutionID: execID, ExecutionKey: execID,
		Status: nodes.StatusSucceeded, StartedAt: time.Date(2026, 6, 2, 10, 0, 0, 0, time.UTC),
		Jobs: []nodes.JobInput{{Record: nodes.JobRun{JobID: execID, Folder: "j-" + execID, Status: nodes.StatusSucceeded},
			Attempts: []nodes.AttemptInput{{Record: nodes.JobAttempt{Attempt: 1, Status: nodes.StatusSucceeded}}}}},
	})
	if err != nil {
		t.Fatalf("Seal %s: %v", execID, err)
	}
}

func reachable(t *testing.T, ep Endpoint, refName string) {
	t.Helper()
	ctx := context.Background()
	r, err := ep.Refs.Read(ctx, refName)
	if err != nil {
		t.Fatalf("read ref %q: %v", refName, err)
	}
	if has, _ := ep.Objects.Has(ctx, objectstore.ObjectID(r.Target)); !has {
		t.Fatalf("ref target absent")
	}
	if err := ep.Objects.Walk(ctx, objectstore.ObjectID(r.Target), func(objectstore.ObjectID, objectstore.Kind) error { return nil }); err != nil {
		t.Fatalf("closure incomplete: %v", err)
	}
}

func TestPushCopiesClosureAndRef(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	local, remote := endpoint(t), endpoint(t)
	seal(t, local, "exec_001")

	res, err := Push(ctx, local, remote, "executions/latest")
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if res.Closure == 0 || res.Copied != res.Closure || !res.RefMoved {
		t.Fatalf("push result = %+v", res)
	}
	// The remote now has the full closure reachable from the moved ref.
	reachable(t, remote, "executions/latest")
}

func TestPushIdempotentDelta(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	local, remote := endpoint(t), endpoint(t)
	seal(t, local, "exec_001")

	if _, err := Push(ctx, local, remote, "executions/latest"); err != nil {
		t.Fatalf("Push 1: %v", err)
	}
	// Second push of the same ref copies nothing and does not move the ref.
	res, err := Push(ctx, local, remote, "executions/latest")
	if err != nil {
		t.Fatalf("Push 2: %v", err)
	}
	if res.Copied != 0 || res.Skipped != res.Closure || res.RefMoved {
		t.Fatalf("second push should be a near-no-op: %+v", res)
	}
}

func TestPullRoundTrip(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	local, remote := endpoint(t), endpoint(t)
	seal(t, local, "exec_001")
	if _, err := Push(ctx, local, remote, "executions/latest"); err != nil {
		t.Fatalf("Push: %v", err)
	}
	// A fresh endpoint pulls the closure from the remote and can read it.
	fresh := endpoint(t)
	res, err := Pull(ctx, fresh, remote, "executions/latest")
	if err != nil {
		t.Fatalf("Pull: %v", err)
	}
	if res.Copied == 0 || !res.RefMoved {
		t.Fatalf("pull result = %+v", res)
	}
	reachable(t, fresh, "executions/latest")
}

func TestPushSharedObjectsDeduped(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	local, remote := endpoint(t), endpoint(t)
	seal(t, local, "exec_001")
	seal(t, local, "exec_002")

	if _, err := Push(ctx, local, remote, "executions/by-id/exec_001"); err != nil {
		t.Fatalf("Push 1: %v", err)
	}
	// Pushing the second execution skips objects shared with the first (e.g. the
	// empty events/artifacts subtrees).
	res, err := Push(ctx, local, remote, "executions/by-id/exec_002")
	if err != nil {
		t.Fatalf("Push 2: %v", err)
	}
	if res.Skipped == 0 {
		t.Fatalf("expected some shared objects to be skipped: %+v", res)
	}
}

func TestSyncSourceRefMissing(t *testing.T) {
	t.Parallel()
	local, remote := endpoint(t), endpoint(t)
	if _, err := Push(context.Background(), local, remote, "executions/latest"); err == nil {
		t.Fatalf("expected error for missing source ref")
	}
}

// countingDest wraps a real ObjectStore and implements objectstore.MissingFilter,
// counting batch MissingObjects calls and per-object Has calls so a test can
// assert Sync negotiates a closure's presence in one batch rather than one
// Has per object. Its MissingObjects delegates to the embedded store's Has, so
// the wrapper's own Has counter stays at zero on the batch path.
type countingDest struct {
	objectstore.ObjectStore
	hasCalls     int
	missingCalls int
}

func (c *countingDest) Has(ctx context.Context, id objectstore.ObjectID) (bool, error) {
	c.hasCalls++
	return c.ObjectStore.Has(ctx, id)
}

func (c *countingDest) MissingObjects(ctx context.Context, ids []objectstore.ObjectID) ([]objectstore.ObjectID, error) {
	c.missingCalls++
	var missing []objectstore.ObjectID
	for _, id := range ids {
		has, err := c.ObjectStore.Has(ctx, id)
		if err != nil {
			return nil, err
		}
		if !has {
			missing = append(missing, id)
		}
	}
	return missing, nil
}

func countingEndpoint(t *testing.T) (Endpoint, *countingDest) {
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
	cd := &countingDest{ObjectStore: store}
	return Endpoint{Objects: cd, Refs: refs}, cd
}

func TestSyncUsesBatchMissingFilter(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	local := endpoint(t)
	remote, cd := countingEndpoint(t)
	seal(t, local, "exec_001")

	res, err := Push(ctx, local, remote, "executions/latest")
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if res.Closure == 0 || res.Copied != res.Closure || !res.RefMoved {
		t.Fatalf("push result = %+v", res)
	}
	// The whole closure's presence is negotiated in a single batch call, and the
	// per-object Has path is never taken.
	if cd.missingCalls != 1 {
		t.Fatalf("missingCalls = %d, want 1 (whole closure in one batch)", cd.missingCalls)
	}
	if cd.hasCalls != 0 {
		t.Fatalf("hasCalls = %d, want 0 (batch path must not probe per-object)", cd.hasCalls)
	}
}

func TestSyncShortCircuitsWhenRefMatches(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	local := endpoint(t)
	remote, cd := countingEndpoint(t)
	seal(t, local, "exec_001")

	if _, err := Push(ctx, local, remote, "executions/latest"); err != nil {
		t.Fatalf("Push 1: %v", err)
	}
	before := cd.missingCalls
	res, err := Push(ctx, local, remote, "executions/latest")
	if err != nil {
		t.Fatalf("Push 2: %v", err)
	}
	if res.Copied != 0 || res.Skipped != res.Closure || res.RefMoved {
		t.Fatalf("second push should short-circuit: %+v", res)
	}
	// A matching destination ref returns before any presence negotiation.
	if cd.missingCalls != before {
		t.Fatalf("missingCalls = %d, want %d (short-circuit skips the scan)", cd.missingCalls, before)
	}
}

func TestPushParallelUploadsWholeClosure(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	local, remote := endpoint(t), endpoint(t)

	// A tree whose leaf count exceeds uploadConcurrency, so the bounded parallel
	// uploader copies in several waves; every object must still land.
	const leaves = 50
	entries := make([]objectstore.TreeEntry, 0, leaves)
	for i := 0; i < leaves; i++ {
		leaf, err := local.Objects.PutBlob(ctx, []byte(fmt.Sprintf("leaf-%d", i)))
		if err != nil {
			t.Fatalf("PutBlob %d: %v", i, err)
		}
		entries = append(entries, objectstore.TreeEntry{Name: fmt.Sprintf("f-%02d", i), Kind: objectstore.KindBlob, ID: leaf})
	}
	root, err := local.Objects.PutTree(ctx, entries)
	if err != nil {
		t.Fatalf("PutTree: %v", err)
	}
	if err := local.Refs.Update(ctx, "trees/big", "", string(root)); err != nil {
		t.Fatalf("ref update: %v", err)
	}

	res, err := Push(ctx, local, remote, "trees/big")
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if res.Closure != leaves+1 || res.Copied != leaves+1 || !res.RefMoved {
		t.Fatalf("push result = %+v, want %d closure/copied", res, leaves+1)
	}
	// The remote holds the full closure reachable from the moved ref.
	reachable(t, remote, "trees/big")
}

func TestVerifyCopied(t *testing.T) {
	t.Parallel()
	if err := verifyCopied("sha256:a", "sha256:a"); err != nil {
		t.Fatalf("equal ids: %v", err)
	}
	if err := verifyCopied("sha256:a", "sha256:b"); err == nil {
		t.Fatalf("mismatched ids should error")
	}
}
