package main

// Tests for the M5.b revision-first execution wiring (cli-surface.md §2.2).
//
// The seven-branch resolver itself is exhaustively tested under
// internal/revision/resolver_test.go (TestResolveRevision_Branch1..7); these
// tests cover the CLI-layer glue:
//
//   - synthesizeRevisionForRun materializes a fresh manual revision when
//     the resolver returns a miss;
//   - setupRevisionExecution + finalizeRevisionExecution drive a real
//     execution-state lifecycle to terminal status with summary counts
//     mirrored from the legacy ExecState;
//   - the --revision flag short-circuits the resolution chain by passing
//     the raw value to ResolveRevision (we exercise a "rev-…" key path);
//   - --exec-id is plumbed through CreateExecutionInput.OriginalKey.

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/executionstate"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/revision"
	"github.com/sourceplane/orun/internal/statestore"
)

// withTempIntentRoot points the global storeDir() helper at a fresh temp
// directory and restores the previous value on test cleanup. Mirrors the
// pattern in command_github_test.go.
func withTempIntentRoot(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := intentRoot
	intentRoot = dir
	t.Cleanup(func() { intentRoot = prev })
	return dir
}

// minimalPlan returns a 1-job plan that the renderer would have produced
// from a trivial intent. Job UID/ID values are arbitrary but stable across
// canonicalPlanJSON calls.
func minimalPlan(t *testing.T) *model.Plan {
	t.Helper()
	return &model.Plan{
		Metadata: model.PlanMetadata{
			Name: "test-plan",
		},
		Jobs: []model.PlanJob{{
			ID:          "job-1",
			UID:         "job-1-uid",
			Component:   "comp-a",
			Environment: "dev",
		}},
	}
}

// resetRunFlags unbinds the package-level run flags between tests so one
// test's --revision setting doesn't leak into the next.
func resetRunFlags(t *testing.T) {
	t.Helper()
	prevRev := runRevision
	prevExec := runExecID
	prevRunner := runRunner
	prevPlanRef := runPlanRef
	prevResolvedRev := runResolvedRevisionArg
	t.Cleanup(func() {
		runRevision = prevRev
		runExecID = prevExec
		runRunner = prevRunner
		runPlanRef = prevPlanRef
		runResolvedRevisionArg = prevResolvedRev
	})
	runRevision = ""
	runExecID = ""
	runRunner = ""
	runPlanRef = ""
	runResolvedRevisionArg = ""
}

func TestSynthesizeRevisionForRun_PersistsRevisionTriplet(t *testing.T) {
	dir := withTempIntentRoot(t)
	resetRunFlags(t)

	stateStore, err := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(dir, ".orun")})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	plan := minimalPlan(t)

	revKey, err := synthesizeRevisionForRun(context.Background(), stateStore, plan, nil)
	if err != nil {
		t.Fatalf("synthesizeRevisionForRun: %v", err)
	}
	if !strings.HasPrefix(revKey, "rev-") {
		t.Fatalf("revKey = %q; want rev-… prefix", revKey)
	}
	if plan.Metadata.Revision == nil || plan.Metadata.Revision.Key != revKey {
		t.Fatalf("plan.Metadata.Revision not stamped with %q (got %+v)", revKey, plan.Metadata.Revision)
	}

	// The triplet must be readable through the resolver — proves the
	// synthesized revision is a first-class on-disk citizen, not an
	// in-memory ghost.
	ref, err := revision.ResolveRevision(context.Background(), stateStore, revKey, revision.ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveRevision(%s): %v", revKey, err)
	}
	if ref.Source != revision.ResolveSourceRevisionKey {
		t.Fatalf("ref.Source = %q; want %q", ref.Source, revision.ResolveSourceRevisionKey)
	}
}

func TestSetupAndFinalizeRevisionExecution_HappyPath(t *testing.T) {
	dir := withTempIntentRoot(t)
	resetRunFlags(t)
	runExecID = "exec-test-001"

	plan := minimalPlan(t)

	// Pre-stamp plan.Metadata.Revision via synthesize so setup hits the
	// "resolver miss → fall through to synthesize" path deterministically.
	rx, err := setupRevisionExecution(context.Background(), plan, nil, "exec-test-001")
	if err != nil {
		t.Fatalf("setupRevisionExecution: %v", err)
	}
	if rx == nil {
		t.Fatal("setupRevisionExecution returned nil rx with no error")
	}
	if rx.revKey == "" || rx.execKey == "" {
		t.Fatalf("rx missing keys: rev=%q exec=%q", rx.revKey, rx.execKey)
	}
	if rx.execKey != "exec-test-001" {
		t.Fatalf("execKey = %q; want preserved runner exec id", rx.execKey)
	}
	if rx.exec.Status != executionstate.StatusPending {
		t.Fatalf("exec.Status = %q; want pending", rx.exec.Status)
	}
	if rx.exec.Reason != executionstate.ReasonDirectRun {
		t.Fatalf("exec.Reason = %q; want direct-run", rx.exec.Reason)
	}
	if rx.exec.OriginalKey != "exec-test-001" {
		t.Fatalf("exec.OriginalKey = %q; want exec-test-001 (--exec-id plumbed via OriginalKey)", rx.exec.OriginalKey)
	}

	// finalize projects the ExecutionCounts it is handed into the execution's
	// ExecSummary; the per-job outcome is passed directly, not read back from
	// any store.
	if _, err := finalizeRevisionExecution(context.Background(), rx, execmodel.ExecutionCounts{Total: 1, Completed: 1}, nil); err != nil {
		t.Fatalf("finalizeRevisionExecution: %v", err)
	}

	// Re-read the execution from disk and assert it landed terminal.
	stateStore, _ := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(dir, ".orun")})
	execPath := statestore.ExecutionDocPath(rx.revKey, rx.execKey)
	raw, _, err := stateStore.Read(context.Background(), execPath)
	if err != nil {
		t.Fatalf("read execution.json: %v", err)
	}
	got := string(raw)
	if !strings.Contains(got, `"status": "completed"`) {
		t.Fatalf("execution.json status not flipped to completed: %s", got)
	}
	if !strings.Contains(got, `"completed": 1`) {
		t.Fatalf("execution.json summary.completed not 1: %s", got)
	}
}

func TestSetupRevisionExecution_PreservesGitHubActionsExecID(t *testing.T) {
	_ = withTempIntentRoot(t)
	resetRunFlags(t)

	plan := minimalPlan(t)
	ghaExecID := "gh-123456789-2-abcdef0"

	rx, err := setupRevisionExecution(context.Background(), plan, nil, ghaExecID)
	if err != nil {
		t.Fatalf("setupRevisionExecution: %v", err)
	}
	if rx.execKey != ghaExecID {
		t.Fatalf("execKey = %q; want %q", rx.execKey, ghaExecID)
	}
	if rx.exec.OriginalKey != ghaExecID {
		t.Fatalf("OriginalKey = %q; want %q", rx.exec.OriginalKey, ghaExecID)
	}
}

func TestSetupRevisionExecution_FailedRunMarksFailed(t *testing.T) {
	dir := withTempIntentRoot(t)
	resetRunFlags(t)
	runExecID = "exec-fail-001"

	plan := minimalPlan(t)

	rx, err := setupRevisionExecution(context.Background(), plan, nil, "exec-fail-001")
	if err != nil {
		t.Fatalf("setupRevisionExecution: %v", err)
	}

	// Pass a non-nil runErr to finalize → status MUST be failed even
	// if the legacy ExecutionCounts say zero failures (the runner can
	// fail to start before persisting any per-job status).
	if _, err := finalizeRevisionExecution(context.Background(), rx, execmodel.ExecutionCounts{}, errFakeRunner); err != nil {
		t.Fatalf("finalizeRevisionExecution: %v", err)
	}

	stateStore, _ := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(dir, ".orun")})
	raw, _, err := stateStore.Read(context.Background(), statestore.ExecutionDocPath(rx.revKey, rx.execKey))
	if err != nil {
		t.Fatalf("read execution.json: %v", err)
	}
	if !strings.Contains(string(raw), `"status": "failed"`) {
		t.Fatalf("expected failed status; got %s", raw)
	}
}

// errFakeRunner is the canned runner error used for the failed-path test.
// We use a package-level var (rather than an inline errors.New) so it can
// be referenced by reference and remain comparable across calls.
var errFakeRunner = &fakeRunnerError{}

type fakeRunnerError struct{}

func (*fakeRunnerError) Error() string { return "fake runner failure" }

func TestSetupRevisionExecution_RevisionFlagShortCircuit(t *testing.T) {
	dir := withTempIntentRoot(t)
	resetRunFlags(t)

	// Pre-persist a revision the resolver will find on branch 3 when
	// --revision is set to its key.
	stateStore, _ := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(dir, ".orun")})
	plan := minimalPlan(t)
	revKey, err := synthesizeRevisionForRun(context.Background(), stateStore, plan, nil)
	if err != nil {
		t.Fatalf("seed synthesizeRevisionForRun: %v", err)
	}

	// Reset plan.Metadata.Revision so we can prove --revision pulled
	// the real bytes from disk rather than reusing the in-memory stamp.
	plan2 := minimalPlan(t)
	runRevision = revKey
	runExecID = "exec-rev-flag"

	rx, err := setupRevisionExecution(context.Background(), plan2, nil, "exec-rev-flag")
	if err != nil {
		t.Fatalf("setupRevisionExecution(--revision %s): %v", revKey, err)
	}
	if rx.revKey != revKey {
		t.Fatalf("rx.revKey = %q; want %q (--revision should bind exactly)", rx.revKey, revKey)
	}
	if rx.source != revision.ResolveSourceRevisionKey {
		t.Fatalf("rx.source = %q; want %q", rx.source, revision.ResolveSourceRevisionKey)
	}
}

func TestResolveAndLoadPlan_RevisionPrefixUsesGlobalIndex(t *testing.T) {
	dir := withTempIntentRoot(t)
	resetRunFlags(t)

	stateStore, _ := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(dir, ".orun")})
	plan := minimalPlan(t)
	revKey, err := synthesizeRevisionForRun(context.Background(), stateStore, plan, nil)
	if err != nil {
		t.Fatalf("synthesizeRevisionForRun: %v", err)
	}

	runPlanRef = revKey[:len("rev-manual-")+4]
	got, err := resolveAndLoadPlan()
	if err != nil {
		t.Fatalf("resolveAndLoadPlan(%q): %v", runPlanRef, err)
	}
	if got.Metadata.Name != "test-plan" {
		t.Fatalf("plan name = %q; want test-plan", got.Metadata.Name)
	}
	if runResolvedRevisionArg != revKey {
		t.Fatalf("runResolvedRevisionArg = %q; want %q", runResolvedRevisionArg, revKey)
	}
}

func TestSetupRevisionExecution_NilPlanRejected(t *testing.T) {
	withTempIntentRoot(t)
	resetRunFlags(t)
	if _, err := setupRevisionExecution(context.Background(), nil, nil, "x"); err == nil {
		t.Fatal("expected error for nil plan")
	}
}

func TestSetupRevisionExecution_EmptyExecIDRejected(t *testing.T) {
	withTempIntentRoot(t)
	resetRunFlags(t)
	if _, err := setupRevisionExecution(context.Background(), minimalPlan(t), nil, ""); err == nil {
		t.Fatal("expected error for empty legacyExecID")
	}
}

// TestFinalizeRevisionExecution_NilRxIsNoOp guards the early-return — the
// caller installs setupRevisionExecution best-effort and may have a nil
// rx if the local store was unwritable. Finalize must not crash.
func TestFinalizeRevisionExecution_NilRxIsNoOp(t *testing.T) {
	if _, err := finalizeRevisionExecution(context.Background(), nil, execmodel.ExecutionCounts{}, nil); err != nil {
		t.Fatalf("finalizeRevisionExecution(nil) = %v; want nil", err)
	}
}

// TestSetupRevisionExecution_StoreRootIsAbsolute confirms the store path
// is an absolute path (not relative to cwd) so the Bridge can resolve
// LegacyRoot independently of the runner's working directory.
func TestSetupRevisionExecution_StoreRootIsAbsolute(t *testing.T) {
	_ = withTempIntentRoot(t)
	resetRunFlags(t)
	runExecID = "exec-abs-001"

	plan := minimalPlan(t)
	rx, err := setupRevisionExecution(context.Background(), plan, nil, "exec-abs-001")
	if err != nil {
		t.Fatalf("setupRevisionExecution: %v", err)
	}
	if !filepath.IsAbs(rx.planFile) {
		t.Fatalf("rx.planFile = %q; want absolute path", rx.planFile)
	}
	// Smoke-check: the canonical plan.json file actually exists.
	if _, err := os.Stat(rx.planFile); err != nil {
		t.Fatalf("plan.json missing at %s: %v", rx.planFile, err)
	}
}
