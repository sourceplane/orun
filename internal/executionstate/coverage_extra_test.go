package executionstate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/testfx/statefs"
)

// TestResolveByRevAndKey_DecodeError covers the strictJSON branch of
// resolveByRevAndKey when execution.json is corrupt. Surface should be a
// wrapped decode error (not a sentinel — readers should hit ErrInvalid
// only when statestore disallows; here it's a bare decode error).
func TestResolveByRevAndKey_DecodeError(t *testing.T) {
	cfg, revKey, _ := newWriterFixture(t)
	// Plant a corrupt execution.json under revKey/run-bad/.
	docPath := statestore.ExecutionDocPath(revKey, "run-bad")
	if _, err := cfg.Store.Write(context.Background(), docPath, []byte("not json"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("seed corrupt exec: %v", err)
	}
	_, err := ResolveExecution(context.Background(), cfg.Store, "run-bad", revKey, ResolveOptions{})
	if err == nil {
		t.Fatal("expected error decoding corrupt execution.json")
	}
}

// TestResolveExecution_LatestRef_DecodeError exercises the latest-ref path
// when the latest-execution ref points at an exec key whose execution.json
// is missing. Should surface ErrNotFound through resolveLatestRef.
func TestResolveExecution_LatestRef_StaleTarget(t *testing.T) {
	cfg, revKey, _ := newWriterFixture(t)
	// Write a latest-execution ref pointing at a non-existent exec.
	if _, err := statestore.WriteLatestExecutionRef(context.Background(), cfg.Store,
		statestore.LatestExecutionRef{
			RevisionKey:  revKey,
			ExecutionKey: "run-missing",
			ExecutionID:  "exec_missing",
			Status:       StatusRunning,
		}); err != nil {
		t.Fatalf("seed latest ref: %v", err)
	}
	_, err := ResolveExecution(context.Background(), cfg.Store, "latest", "", ResolveOptions{})
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

// TestResolveLegacyLatest_NoDirEntries covers the resolveLegacyLatest
// branch where the executions/ dir exists but contains only files (no
// child dirs). Should surface ErrNotFound from "legacy executions dir
// empty" path.
func TestResolveLegacyLatest_NoDirEntries(t *testing.T) {
	root := statefs.NewWorkspace(t)
	legacyDir := filepath.Join(root, ".orun", "executions")
	if err := os.MkdirAll(legacyDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Plant a file (not a dir) so ReadDir succeeds but yields no
	// directory entries.
	if err := os.WriteFile(filepath.Join(legacyDir, "stray.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	store, _ := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(root, ".orun")})
	_, err := ResolveExecution(context.Background(), store, "latest", "", ResolveOptions{
		LegacyRoot: LegacyRoot(filepath.Join(root, ".orun")),
	})
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

func TestResolveLegacy_NoRoot(t *testing.T) {
	_, err := resolveLegacy("", "run-001")
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

// TestResolvePrefixScan_EmptyArg covers the early-return when
// ResolveExecution sends an empty-after-validation arg into
// resolvePrefixScan via a non-component (e.g. arg with slash). The
// resolver short-circuits the index branch via ValidateComponent and
// then prefix-scans — empty arg is the explicit guarded case.
func TestResolvePrefixScan_EmptyArg(t *testing.T) {
	cfg, _, _ := newWriterFixture(t)
	_, err := resolvePrefixScan(context.Background(), cfg.Store, "")
	if !errors.Is(err, statestore.ErrNotFound) {
		t.Fatalf("err=%v want ErrNotFound", err)
	}
}

// TestUpdateSnapshot_NilStore covers the early validation path.
func TestUpdateSnapshot_NilStore(t *testing.T) {
	err := UpdateSnapshot(context.Background(), Config{}, ExecutionRun{RevisionKey: "rev-main-abcdef0-pfeedface", ExecutionKey: "run-001"})
	if !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("err=%v want ErrInvalid", err)
	}
}

// TestMarkTerminal_DecodeError exercises the corrupt-execution.json path
// inside MarkTerminal's read-modify-CAS loop.
func TestMarkTerminal_DecodeError(t *testing.T) {
	cfg, revKey, _ := newWriterFixture(t)
	docPath := statestore.ExecutionDocPath(revKey, "run-corrupt")
	if _, err := cfg.Store.Write(context.Background(), docPath, []byte("{not json"), statestore.WriteOptions{}); err != nil {
		t.Fatalf("seed corrupt exec: %v", err)
	}
	_, err := MarkTerminal(context.Background(), cfg, revKey, "run-corrupt", StatusCompleted, ExecSummary{})
	if err == nil {
		t.Fatal("expected decode error")
	}
}

// TestListExecutionKeys_Empty exercises the empty-prefix branch of
// listExecutionKeys (no executions yet under the revision).
func TestListExecutionKeys_Empty(t *testing.T) {
	cfg, revKey, _ := newWriterFixture(t)
	keys, err := listExecutionKeys(context.Background(), cfg.Store, revKey)
	if err != nil {
		t.Fatalf("listExecutionKeys: %v", err)
	}
	if len(keys) != 0 {
		t.Fatalf("got %d keys want 0", len(keys))
	}
}

// TestExecutionKeyFromPath_Edges_Extra covers off-prefix and trailing-slash-only edges.
func TestExecutionKeyFromPath_Edges_Extra(t *testing.T) {
	if got := executionKeyFromPath("revisions/r/executions", "other/path"); got != "" {
		t.Fatalf("off-prefix: got %q want empty", got)
	}
	if got := executionKeyFromPath("revisions/r/executions", "revisions/r/executions/"); got != "" {
		t.Fatalf("trailing-slash-only: got %q want empty", got)
	}
}

// TestInputValidation_ErrorPaths covers cheap ErrInvalid branches in
// SanitizeExecID, NextExecutionKey, UpdateSnapshot, and MarkTerminal that
// otherwise go untested.
func TestInputValidation_ErrorPaths(t *testing.T) {
	ctx := context.Background()
	cfg, revKey, _ := newWriterFixture(t)

	if _, err := SanitizeExecID(""); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("SanitizeExecID empty err=%v want ErrInvalid", err)
	}
	if _, err := SanitizeExecID("///"); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("SanitizeExecID all-disallowed err=%v want ErrInvalid", err)
	}
	if _, err := NextExecutionKey(ctx, nil, revKey); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("NextExecutionKey nil store err=%v want ErrInvalid", err)
	}
	if _, err := NextExecutionKey(ctx, cfg.Store, "bogus"); err == nil {
		t.Fatal("NextExecutionKey bad revKey: expected error")
	}
	// UpdateSnapshot bad revKey + bad execKey
	if err := UpdateSnapshot(ctx, cfg, ExecutionRun{RevisionKey: "bogus", ExecutionKey: "run-001"}); err == nil {
		t.Fatal("UpdateSnapshot bad revKey: expected error")
	}
	if err := UpdateSnapshot(ctx, cfg, ExecutionRun{RevisionKey: revKey, ExecutionKey: "BAD/KEY"}); err == nil {
		t.Fatal("UpdateSnapshot bad execKey: expected error")
	}
	// MarkTerminal: not terminal status
	if _, err := MarkTerminal(ctx, cfg, revKey, "run-001", "running", ExecSummary{}); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("MarkTerminal not-terminal err=%v want ErrInvalid", err)
	}
	// MarkTerminal nil store
	if _, err := MarkTerminal(ctx, Config{}, revKey, "run-001", StatusCompleted, ExecSummary{}); !errors.Is(err, statestore.ErrInvalid) {
		t.Fatalf("MarkTerminal nil store err=%v want ErrInvalid", err)
	}
	// MarkTerminal bad revKey + bad execKey
	if _, err := MarkTerminal(ctx, cfg, "bogus", "run-001", StatusCompleted, ExecSummary{}); err == nil {
		t.Fatal("MarkTerminal bad revKey: expected error")
	}
	if _, err := MarkTerminal(ctx, cfg, revKey, "BAD/KEY", StatusCompleted, ExecSummary{}); err == nil {
		t.Fatal("MarkTerminal bad execKey: expected error")
	}
	// MarkTerminal: execution does not exist → wrapped read error
	if _, err := MarkTerminal(ctx, cfg, revKey, "run-missing", StatusCompleted, ExecSummary{}); err == nil {
		t.Fatal("MarkTerminal missing execution: expected error")
	}
}

// TestResolveLegacyRoot_FromStore covers the rooted-interface branch that
// pulls LegacyRoot from a LocalStore when opts.LegacyRoot is empty.
func TestResolveLegacyRoot_FromStore(t *testing.T) {
	root := statefs.NewWorkspace(t)
	store, err := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(root, ".orun")})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	got := resolveLegacyRoot(store, ResolveOptions{})
	if got == "" {
		t.Fatal("resolveLegacyRoot from store: got empty, want non-empty Root()")
	}
	// And an explicit override wins.
	want := LegacyRoot("/explicit")
	if g := resolveLegacyRoot(store, ResolveOptions{LegacyRoot: want}); g != want {
		t.Fatalf("resolveLegacyRoot override: got %q want %q", g, want)
	}
}

// TestSynthesizeFromLegacy_StatusOverride and friends exercise leniency
// of the legacy synthesizer.
func TestSynthesizeFromLegacy_DefaultsAndOverride(t *testing.T) {
	rec, err := synthesizeFromLegacy("legacy-1", []byte(`{"status":"failed","ignored":42}`))
	if err != nil {
		t.Fatalf("synthesizeFromLegacy: %v", err)
	}
	if rec.Status != "failed" {
		t.Fatalf("status=%q want failed", rec.Status)
	}
	if rec.OriginalKey != "legacy-1" {
		t.Fatalf("originalKey=%q", rec.OriginalKey)
	}
	if rec.Reason != ReasonMigration {
		t.Fatalf("reason=%q want %q", rec.Reason, ReasonMigration)
	}
	// Empty status falls back to default StatusCompleted.
	rec2, err := synthesizeFromLegacy("legacy-2", []byte(`{}`))
	if err != nil {
		t.Fatalf("synthesizeFromLegacy 2: %v", err)
	}
	if rec2.Status != StatusCompleted {
		t.Fatalf("default status=%q want %q", rec2.Status, StatusCompleted)
	}
}
