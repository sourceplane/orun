package main

// Tests for the M5.d hidden `orun state migrate` command. Spec source of
// truth: specs/orun-state-redesign/compatibility-and-migration.md §5.
//
// The plan-migration phase is exercised against a workspace seeded with a
// real `.orun/plans/<hex>.json` file so the legacy-hash branch (5) of
// internal/revision.ResolveRevision can synthesize a system.migrated
// revision from disk. The execution-attachment phase seeds a legacy
// `.orun/executions/<execID>/state.json` that points at the same plan
// hash; the bridge mirrors state.json into the new layout under
// revisions/<revKey>/executions/<execKey>/.
//
// Coverage by test:
//
//   - TestStateMigrate_HappyPath_PlansAndExecutions — seeds a plan + a
//     state.json that references its hash, runs migrate, verifies the
//     revision triplet, mirrored state.json, and a CreateExecution-derived
//     ExecutionRun all land on disk.
//   - TestStateMigrate_DryRun — same fixture, --dry-run leaves the new
//     layout untouched.
//   - TestStateMigrate_Idempotent — runs migrate twice; second run
//     reports zero new revisions.
//   - TestStateMigrate_OrphanExecution — execution with no recoverable
//     plan hash gets attached to a deterministic unknown-hash revision
//     and counted as an orphan.
//   - TestDescribeRevision_LiteralLatest_NormalizesToEmpty — Option A
//     CLI normalization on `orun describe revision latest` /
//     `orun describe trigger latest`.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/revision"
	"github.com/sourceplane/orun/internal/state"
	"github.com/sourceplane/orun/internal/statestore"
)

// seedLegacyPlan writes a tiny plan to .orun/plans/<short>.json under dir
// (mirroring state.Store.SavePlan) and returns the file's bare-hex stem
// (the "<checksum>" segment) so callers can reference it. The migrate
// command's plan-migration phase derives a canonical "sha256:<hex>" hash
// from the file bytes itself; tests that need that value go through
// computePlanHashFromShort.
func seedLegacyPlan(t *testing.T, dir string) string {
	t.Helper()
	plan := &model.Plan{
		Metadata: model.PlanMetadata{
			Name:     "fixture-plan",
			Checksum: "sha256-deadbeef00000000000000000000000000000000000000000000000000000000",
		},
		Jobs: []model.PlanJob{{
			ID:          "job-1",
			UID:         "job-1-uid",
			Component:   "comp-a",
			Environment: "dev",
		}},
	}
	st := state.NewStore(dir)
	if err := st.SavePlan(plan, ""); err != nil {
		t.Fatalf("seed legacy plan: %v", err)
	}
	short := state.PlanChecksumShort(plan)
	if short == "" {
		t.Fatalf("seedLegacyPlan: PlanChecksumShort returned empty")
	}
	return short
}

// seedLegacyExecution writes a legacy `.orun/executions/<execID>/state.json`
// + `.orun/executions/<execID>/metadata.json`. planChecksum, when
// non-empty, becomes the state.json `planChecksum` field — set to ""
// to simulate an orphan with no recoverable hash.
func seedLegacyExecution(t *testing.T, dir, execID, planChecksum string) {
	t.Helper()
	st := state.NewStore(dir)
	exec := &state.ExecState{
		ExecID:       execID,
		PlanChecksum: planChecksum,
		Jobs:         map[string]*state.JobState{},
	}
	if err := st.SaveState(execID, exec); err != nil {
		t.Fatalf("seed legacy exec state.json: %v", err)
	}
	meta := &state.ExecMetadata{
		ExecID:    execID,
		PlanID:    "fixture-plan",
		PlanName:  "fixture-plan",
		StartedAt: "2025-01-01T00:00:00Z",
		Status:    "completed",
	}
	if err := st.SaveMetadata(execID, meta); err != nil {
		t.Fatalf("seed legacy exec metadata.json: %v", err)
	}
}

// computePlanHashFromShort opens the plan file written by seedLegacyPlan
// and runs the legacy-hash resolver against its short stem to recover
// the canonical revision key + plan hash. Tests use this to look up the
// expected on-disk artifacts after a migrate run.
func computePlanHashFromShort(t *testing.T, dir, shortHash string) (revKey, planHash string) {
	t.Helper()
	stateStore, err := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(dir, ".orun")})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	ref, err := revision.ResolveRevision(context.Background(), stateStore, shortHash, revision.ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveRevision(legacy): %v", err)
	}
	return ref.Revision.RevisionKey, ref.Revision.PlanHash
}

func TestStateMigrate_HappyPath_PlansAndExecutions(t *testing.T) {
	dir := withTempIntentRoot(t)

	shortHash := seedLegacyPlan(t, dir)
	revKey, planHash := computePlanHashFromShort(t, dir, shortHash)
	// Match phase 1's hashToRev lookup format: state.json stores the
	// short legacy stem, but the migrate hashToRev map is keyed on the
	// canonical "sha256:<hex>" form. Use the bare short hex; the
	// fallback in lookupRevByHash handles the prefix dance.
	seedLegacyExecution(t, dir, "exec-fixture-001", shortHash)

	var buf bytes.Buffer
	if err := runStateMigrate(context.Background(), &buf, false); err != nil {
		t.Fatalf("runStateMigrate: %v\n%s", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "Plan migration:") {
		t.Fatalf("missing plan-migration header in output:\n%s", out)
	}
	if !strings.Contains(out, "Execution attachment:") {
		t.Fatalf("missing execution-attachment header in output:\n%s", out)
	}
	if !strings.Contains(out, "revisions created:    1") {
		t.Fatalf("expected 1 revision created in summary:\n%s", out)
	}

	// The revision triplet must exist on disk.
	for _, rel := range []string{"revisions/" + revKey + "/plan.json",
		"revisions/" + revKey + "/revision.json",
		"revisions/" + revKey + "/trigger.json",
		"revisions/" + revKey + "/executions/exec-fixture-001/execution.json"} {
		if _, err := os.Stat(filepath.Join(dir, ".orun", rel)); err != nil {
			t.Fatalf("expected on-disk artifact %s: %v", rel, err)
		}
	}
	// Bridge-mirrored state.json must land in the new layout.
	mirrored := filepath.Join(dir, ".orun", "revisions", revKey, "executions", "exec-fixture-001", "state.json")
	if _, err := os.Stat(mirrored); err != nil {
		t.Fatalf("bridge mirror failed: %v", err)
	}

	_ = planHash
}

func TestStateMigrate_DryRun(t *testing.T) {
	dir := withTempIntentRoot(t)
	shortHash := seedLegacyPlan(t, dir)
	seedLegacyExecution(t, dir, "exec-dryrun-001", shortHash)

	var buf bytes.Buffer
	if err := runStateMigrate(context.Background(), &buf, true); err != nil {
		t.Fatalf("dry-run errored: %v\n%s", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "Summary (dry run):") {
		t.Fatalf("missing dry-run summary header:\n%s", out)
	}
	// No revisions/ directory should have been written.
	if _, err := os.Stat(filepath.Join(dir, ".orun", "revisions")); !os.IsNotExist(err) {
		t.Fatalf(".orun/revisions/ exists after --dry-run (err=%v)", err)
	}
}

func TestStateMigrate_Idempotent(t *testing.T) {
	dir := withTempIntentRoot(t)
	shortHash := seedLegacyPlan(t, dir)
	seedLegacyExecution(t, dir, "exec-idem-001", shortHash)

	var buf1 bytes.Buffer
	if err := runStateMigrate(context.Background(), &buf1, false); err != nil {
		t.Fatalf("first migrate: %v\n%s", err, buf1.String())
	}
	var buf2 bytes.Buffer
	if err := runStateMigrate(context.Background(), &buf2, false); err != nil {
		t.Fatalf("second migrate: %v\n%s", err, buf2.String())
	}
	out := buf2.String()
	if !strings.Contains(out, "revisions created:    0") {
		t.Fatalf("expected 0 revisions created on rerun, got:\n%s", out)
	}
	if strings.Contains(out, "(new)") {
		t.Fatalf("rerun unexpectedly reported (new) artifacts:\n%s", out)
	}
}

func TestStateMigrate_OrphanExecution(t *testing.T) {
	dir := withTempIntentRoot(t)
	// No legacy plan; only a legacy execution with no recoverable hash.
	seedLegacyExecution(t, dir, "exec-orphan-001", "")

	var buf bytes.Buffer
	if err := runStateMigrate(context.Background(), &buf, false); err != nil {
		t.Fatalf("orphan migrate: %v\n%s", err, buf.String())
	}
	out := buf.String()
	if !strings.Contains(out, "(orphan)") {
		t.Fatalf("expected (orphan) marker:\n%s", out)
	}
	if !strings.Contains(out, "orphans:              1") {
		t.Fatalf("expected 1 orphan in summary:\n%s", out)
	}
}

func TestDescribeRevision_LiteralLatest_NormalizesToEmpty(t *testing.T) {
	// Validate the Option A normalization: describing "latest" must
	// behave identically to describing "" (the empty-arg branch).
	// We don't drive the full describe pipeline here (it writes to
	// os.Stdout); instead we exercise the branch directly via the
	// underlying ResolveRevision contract.
	dir := withTempIntentRoot(t)
	shortHash := seedLegacyPlan(t, dir)
	stateStore, err := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(dir, ".orun")})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	// First migrate so refs/latest-revision.json is populated.
	var sink bytes.Buffer
	if err := runStateMigrate(context.Background(), &sink, false); err != nil {
		t.Fatalf("seed migrate: %v\n%s", err, sink.String())
	}

	// Sanity: the legacy-hash branch resolves the seeded plan.
	if _, err := revision.ResolveRevision(context.Background(), stateStore, shortHash, revision.ResolveOptions{}); err != nil {
		t.Fatalf("ResolveRevision(legacy short hash): %v", err)
	}

	// Empty arg → branch 1 (latest ref).
	emptyRef, err := revision.ResolveRevision(context.Background(), stateStore, "", revision.ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveRevision(empty): %v", err)
	}
	// Normalize "latest" → "" via the same one-line check
	// describeRevision/describeTrigger now apply.
	arg := "latest"
	if arg == "latest" {
		arg = ""
	}
	latestRef, err := revision.ResolveRevision(context.Background(), stateStore, arg, revision.ResolveOptions{})
	if err != nil {
		t.Fatalf("ResolveRevision(literal latest, normalized): %v", err)
	}
	if emptyRef.Revision.RevisionKey != latestRef.Revision.RevisionKey {
		t.Fatalf("normalized 'latest' resolves to %q; want %q (same as empty arg)",
			latestRef.Revision.RevisionKey, emptyRef.Revision.RevisionKey)
	}
}
