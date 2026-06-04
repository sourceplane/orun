package executionstate

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/statestore"
)

var errFailingStore = errors.New("failing store: injected")

// failingStore wraps a StateStore and injects failures on Write /
// CreateIfAbsent for paths matching a predicate, leaving every other operation
// (and all other methods, via embedding) intact.
type failingStore struct {
	statestore.StateStore
	failWrite          func(string) bool
	failCreateIfAbsent func(string) bool
	failCAS            func(string) bool
}

func (f failingStore) Write(ctx context.Context, p string, data []byte, o statestore.WriteOptions) (statestore.ObjectMeta, error) {
	if f.failWrite != nil && f.failWrite(p) {
		return statestore.ObjectMeta{}, errFailingStore
	}
	return f.StateStore.Write(ctx, p, data, o)
}

func (f failingStore) CreateIfAbsent(ctx context.Context, p string, data []byte) (statestore.ObjectMeta, error) {
	if f.failCreateIfAbsent != nil && f.failCreateIfAbsent(p) {
		return statestore.ObjectMeta{}, errFailingStore
	}
	return f.StateStore.CreateIfAbsent(ctx, p, data)
}

func (f failingStore) CompareAndSwap(ctx context.Context, p, oldRev string, data []byte) (statestore.ObjectMeta, error) {
	if f.failCAS != nil && f.failCAS(p) {
		return statestore.ObjectMeta{}, errFailingStore
	}
	return f.StateStore.CompareAndSwap(ctx, p, oldRev, data)
}

func createInput(rec ExecutionRun, originalKey string) CreateExecutionInput {
	return CreateExecutionInput{
		RevisionKey: rec.RevisionKey,
		RevisionID:  rec.RevisionID,
		TriggerID:   rec.TriggerID,
		TriggerKey:  rec.TriggerKey,
		OriginalKey: originalKey,
		Reason:      ReasonDirectRun,
		Status:      StatusPending,
		Runner:      RunnerProfile{Mode: "local", Backend: "local", Platform: "linux"},
		Summary:     ExecSummary{Total: 1, Pending: 1},
	}
}

// TestFinalizeExecution_IndexWriteError covers finalizeExecution's
// execution-index write-failure branch.
func TestFinalizeExecution_IndexWriteError(t *testing.T) {
	cfg, _, rec, _ := resolverFixture(t)
	cfg.Store = failingStore{
		StateStore:         cfg.Store,
		failCreateIfAbsent: func(p string) bool { return strings.Contains(p, "indexes/executions") },
	}
	_, err := CreateExecution(context.Background(), cfg, createInput(rec, "exec-fail-idx"))
	if err == nil || !strings.Contains(err.Error(), "execution index") {
		t.Fatalf("CreateExecution err = %v; want execution-index write error", err)
	}
}

// TestFinalizeExecution_LatestRefWriteError covers finalizeExecution's
// latest-execution ref write-failure branch.
func TestFinalizeExecution_LatestRefWriteError(t *testing.T) {
	cfg, _, rec, _ := resolverFixture(t)
	cfg.Store = failingStore{
		StateStore: cfg.Store,
		failWrite:  func(p string) bool { return strings.Contains(p, "latest-execution") },
	}
	_, err := CreateExecution(context.Background(), cfg, createInput(rec, "exec-fail-ref"))
	if err == nil || !strings.Contains(err.Error(), "latest-execution ref") {
		t.Fatalf("CreateExecution err = %v; want latest-execution ref write error", err)
	}
}

// TestFinalizeExecution_EventWriteError covers the execution-created event
// write-failure branch (index + latest ref succeed first).
func TestFinalizeExecution_EventWriteError(t *testing.T) {
	cfg, _, rec, _ := resolverFixture(t)
	cfg.Store = failingStore{
		StateStore:         cfg.Store,
		failCreateIfAbsent: func(p string) bool { return strings.Contains(p, "/events/") },
	}
	_, err := CreateExecution(context.Background(), cfg, createInput(rec, "exec-fail-evt"))
	if err == nil || !strings.Contains(err.Error(), "execution-created event") {
		t.Fatalf("CreateExecution err = %v; want execution-created event write error", err)
	}
}

// TestCreateExecution_DocClaimError covers CreateExecution's own failure branch
// when claiming the execution document fails with a non-ErrExists error.
func TestCreateExecution_DocClaimError(t *testing.T) {
	cfg, _, rec, _ := resolverFixture(t)
	cfg.Store = failingStore{
		StateStore:         cfg.Store,
		failCreateIfAbsent: func(p string) bool { return strings.HasSuffix(p, "/execution.json") },
	}
	_, err := CreateExecution(context.Background(), cfg, createInput(rec, "exec-fail-doc"))
	if err == nil {
		t.Fatal("CreateExecution err = nil; want execution-doc claim error")
	}
}

// TestSanitizeExecID_TruncatesLongInput covers SanitizeExecID's max-length
// truncation branch.
func TestSanitizeExecID_TruncatesLongInput(t *testing.T) {
	long := strings.Repeat("a", sanitizeExecIDMaxLen+50)
	out, err := SanitizeExecID(long)
	if err != nil {
		t.Fatalf("SanitizeExecID: %v", err)
	}
	if len(out) > sanitizeExecIDMaxLen {
		t.Errorf("len(out) = %d; want <= %d", len(out), sanitizeExecIDMaxLen)
	}
}

// TestMarkTerminal_CASError covers MarkTerminal's execution.json CAS-failure
// branch: a non-conflict CAS error is surfaced.
func TestMarkTerminal_CASError(t *testing.T) {
	cfg, revKey, rec, _ := resolverFixture(t)
	cfg.Store = failingStore{
		StateStore: cfg.Store,
		failCAS:    func(p string) bool { return strings.HasSuffix(p, "/execution.json") },
	}
	_, err := MarkTerminal(context.Background(), cfg, revKey, rec.ExecutionKey,
		StatusCompleted, ExecSummary{Total: 1, Completed: 1})
	if err == nil || !strings.Contains(err.Error(), "cas execution.json") {
		t.Fatalf("MarkTerminal err = %v; want cas execution.json error", err)
	}
}
