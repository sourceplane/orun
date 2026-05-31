package executionstate

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/statestore"
)

// This file adds buffer coverage for executionstate. The package floor sits
// at exactly 90.0% with zero margin; CI has been observed measuring 89.6%
// (an environmental delta on the same source). These tests exercise
// genuinely-reachable, deterministic branches — no fragile store error
// injection — to lift local coverage above the floor with headroom, per the
// R-008 carry-forward note. They touch only public/package surfaces already
// covered by sibling tests.

// TestScanForNextRunSeq_SkipsNonRunKeysAndTracksMax seeds the executions
// directory with one well-formed run-NNN execution plus a non-run-key
// object, then asserts NextExecutionKey increments past the max run number.
// This covers scanForNextRunSeq's match/skip/max-tracking loop body
// (writer.go:205-219) on a real listing rather than the fresh-revision path.
func TestScanForNextRunSeq_SkipsNonRunKeysAndTracksMax(t *testing.T) {
	cfg, revKey, _ := newWriterFixture(t)
	ctx := context.Background()

	// A well-formed execution at run-005 (drives max=5).
	if _, err := cfg.Store.Write(ctx,
		statestore.ExecutionDocPath(revKey, "run-005"),
		[]byte(`{"status":"completed"}`), statestore.WriteOptions{}); err != nil {
		t.Fatalf("seed run-005: %v", err)
	}
	// A non-run-key object under the same executions/ prefix — the scan
	// must skip it (runKeyPattern miss) without affecting the sequence.
	if _, err := cfg.Store.Write(ctx,
		statestore.ExecutionDocPath(revKey, "scratch"),
		[]byte(`{"status":"completed"}`), statestore.WriteOptions{}); err != nil {
		t.Fatalf("seed scratch: %v", err)
	}

	got, err := NextExecutionKey(ctx, cfg.Store, revKey)
	if err != nil {
		t.Fatalf("NextExecutionKey: %v", err)
	}
	if got != "run-006" {
		t.Fatalf("NextExecutionKey = %q want run-006", got)
	}
}

// TestListExecutionKeys_MultiKeyDedup seeds two distinct execution dirs (each
// with two objects) and asserts the sorted, de-duplicated key set. Covers the
// listExecutionKeys accumulation + sort tail (writer.go:626-631) on a
// non-empty listing.
func TestListExecutionKeys_MultiKeyDedup(t *testing.T) {
	cfg, revKey, _ := newWriterFixture(t)
	ctx := context.Background()
	for _, k := range []string{"run-001", "run-002"} {
		if _, err := cfg.Store.Write(ctx,
			statestore.ExecutionDocPath(revKey, k),
			[]byte(`{"status":"completed"}`), statestore.WriteOptions{}); err != nil {
			t.Fatalf("seed %s doc: %v", k, err)
		}
		// A second object under the same exec dir to exercise the dedup map.
		if _, err := cfg.Store.Write(ctx,
			statestore.ExecutionFilePath(revKey, k, "snapshot.json"),
			[]byte(`{}`), statestore.WriteOptions{}); err != nil {
			t.Fatalf("seed %s snapshot: %v", k, err)
		}
	}
	keys, err := listExecutionKeys(ctx, cfg.Store, revKey)
	if err != nil {
		t.Fatalf("listExecutionKeys: %v", err)
	}
	if len(keys) != 2 || keys[0] != "run-001" || keys[1] != "run-002" {
		t.Fatalf("keys = %v want [run-001 run-002]", keys)
	}
}

// TestResolveExactByIndex_StaleTarget writes an execution-index entry whose
// (revKey, execKey) target has no execution.json, then resolves by the exact
// index key. The resolver must surface a wrapped error through
// resolveExactByIndex (resolver.go:236-239) rather than a bare store error.
func TestResolveExactByIndex_StaleTarget(t *testing.T) {
	cfg, revKey, _ := newWriterFixture(t)
	ctx := context.Background()
	if _, err := statestore.WriteExecutionIndex(ctx, cfg.Store, statestore.ExecutionIndexEntry{
		ExecutionKey: "run-staleexact",
		ExecutionID:  "exec_stale",
		RevisionKey:  revKey,
		Status:       StatusCompleted,
		CreatedAt:    time.Now().UTC(),
		Path:         statestore.ExecutionDir(revKey, "run-staleexact"),
	}); err != nil {
		t.Fatalf("seed stale index: %v", err)
	}
	_, err := ResolveExecution(ctx, cfg.Store, "run-staleexact", "", ResolveOptions{})
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound through resolveExactByIndex wrap", err)
	}
}

// TestResolvePrefixScan_SingleStaleMatch writes one execution-index entry with
// a unique prefix whose target execution.json is absent, then resolves by a
// shorter prefix that matches exactly that one entry. This drives the
// single-match worker arm of resolvePrefixScan and its error wrap
// (resolver.go:271-274) — distinct from the ambiguous (≥2) and zero-match
// arms already covered.
func TestResolvePrefixScan_SingleStaleMatch(t *testing.T) {
	cfg, revKey, _ := newWriterFixture(t)
	ctx := context.Background()
	if _, err := statestore.WriteExecutionIndex(ctx, cfg.Store, statestore.ExecutionIndexEntry{
		ExecutionKey: "run-prefixstale01",
		ExecutionID:  "exec_prefixstale",
		RevisionKey:  revKey,
		Status:       StatusCompleted,
		CreatedAt:    time.Now().UTC(),
		Path:         statestore.ExecutionDir(revKey, "run-prefixstale01"),
	}); err != nil {
		t.Fatalf("seed stale prefix index: %v", err)
	}
	// "run-prefixstale" is not an exact index key, so dispatch falls to the
	// prefix scan, which finds exactly one match and forwards to the worker.
	_, err := ResolveExecution(ctx, cfg.Store, "run-prefixstale", "", ResolveOptions{})
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound through resolvePrefixScan single-match wrap", err)
	}
}
