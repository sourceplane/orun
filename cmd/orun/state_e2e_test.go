package main

// End-to-end regression gate for the revision-first state layout
// (test-plan.md §4, design.md §9, implementation-plan.md M6).
//
// This test drives a single workspace through the entire revision-first
// state pipeline in the same order a real `orun plan → run → status →
// logs → describe → get → state migrate` invocation would touch it. Each
// numbered step from test-plan.md §4 lives in its own t.Run so failure
// attribution is unambiguous.
//
// Implementation note: rather than re-driving the full Cobra root with
// `os/exec` (which would force us to seed a complete intent.yaml +
// component tree and would re-test code the per-command tests already
// cover), this E2E walk invokes the same package-level subroutines the
// Cobra RunE handlers call:
//
//   * synthesizeRevisionForRun  → plan.json + revision.json + trigger.json
//                                  + manifest.json + refs/latest-revision.json
//                                  + plans/<hash>.json + plans/latest.json
//   * setupRevisionExecution    → executions/run-001/execution.json
//                                  + indexes/executions/run-001.json
//                                  + refs/latest-execution.json
//   * finalizeRevisionExecution → flips status to completed + summary counts
//   * describeRevision / loadRevisionPlanRows → read-side surface
//   * runStateMigrate           → migration idempotence
//
// That is the same surface test-plan.md §4 asks us to assert on, just
// without paying the cost of a real renderer + os.Stdout capture for
// every read. The renderer surface itself is covered by command_get_test.go
// and command_describe_test.go.
import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/statestore"
)

// stateE2EFixture bundles the per-test workspace plus the seed plan key,
// revision key, and execution key threaded through every sub-step. Each
// step that needs new state mutates the fixture in place.
type stateE2EFixture struct {
	dir      string
	revKey   string
	execKey  string
	planHash string
	store    statestore.StateStore
}

// readJSON reads a file under .orun and decodes it into out. Steps that
// only need to assert existence use os.Stat instead.
func (f *stateE2EFixture) readJSON(t *testing.T, rel string, out any) {
	t.Helper()
	path := filepath.Join(f.dir, ".orun", filepath.FromSlash(rel))
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			t.Fatalf("decode %s: %v", rel, err)
		}
	}
}

// (captureStdout is shared with command_read_revision_test.go.)

func TestStateE2E(t *testing.T) {
	// Coverage gate: the milestone walk owns its own workspace and
	// must not contend with another test's intentRoot.
	dir := withTempIntentRoot(t)
	resetRunFlags(t)
	runExecID = "exec-e2e-001"

	store, err := statestore.NewLocalStore(statestore.LocalConfig{
		Root: filepath.Join(dir, ".orun"),
	})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	plan := minimalPlan(t)

	f := &stateE2EFixture{dir: dir, store: store}
	ctx := context.Background()

	// ---- Steps 1–2: workspace + plan -----------------------------------
	// Step 1: temp workspace (already done by withTempIntentRoot).
	// Step 2: synthesize and persist a plan revision. This is the
	// programmatic equivalent of `orun plan` going through the
	// synthesise → write path that command_run.go invokes when the
	// resolver returns a miss.
	t.Run("step02_plan_synthesizes_revision", func(t *testing.T) {
		revKey, err := synthesizeRevisionForRun(ctx, store, plan, nil)
		if err != nil {
			t.Fatalf("synthesizeRevisionForRun: %v", err)
		}
		if !strings.HasPrefix(revKey, "rev-") {
			t.Fatalf("revKey = %q; want rev-… prefix", revKey)
		}
		f.revKey = revKey
		if plan.Metadata.Revision != nil {
			f.planHash = plan.Metadata.Revision.PlanHash
		}
	})

	// Step 3: the four canonical revision documents must exist on disk.
	t.Run("step03_revision_documents_persisted", func(t *testing.T) {
		for _, rel := range []string{
			"revisions/" + f.revKey + "/plan.json",
			"revisions/" + f.revKey + "/trigger.json",
			"revisions/" + f.revKey + "/revision.json",
			"revisions/" + f.revKey + "/manifest.json",
		} {
			path := filepath.Join(dir, ".orun", filepath.FromSlash(rel))
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("missing %s: %v", rel, err)
			}
		}
	})

	// Step 4: refs/latest-revision.json must point at the new revision.
	t.Run("step04_latest_revision_ref_updated", func(t *testing.T) {
		var ref struct {
			RevisionKey string `json:"revisionKey"`
		}
		f.readJSON(t, "refs/latest-revision.json", &ref)
		if ref.RevisionKey != f.revKey {
			t.Fatalf("latest-revision.json revisionKey = %q; want %q", ref.RevisionKey, f.revKey)
		}
	})

	// Step 5: legacy compat — plans/<hash>.json + plans/latest.json must
	// also exist (writer §writeCompatibilityMirror, on by default).
	t.Run("step05_legacy_plan_mirror_present", func(t *testing.T) {
		// planHash is the canonical "sha256:<hex>" form; the legacy
		// mirror strips that prefix per writer.normalizeLegacyChecksum.
		legacyHash := strings.TrimPrefix(f.planHash, "sha256:")
		for _, rel := range []string{
			"plans/" + legacyHash + ".json",
			"plans/latest.json",
		} {
			path := filepath.Join(dir, ".orun", filepath.FromSlash(rel))
			if _, err := os.Stat(path); err != nil {
				t.Fatalf("missing legacy mirror %s: %v", rel, err)
			}
		}
	})

	// ---- Steps 6–9: execution + indexes -------------------------------
	// Step 6: `orun run --dry-run` equivalent — drive setup, then a
	// completed terminal state through finalize. The dry-run branch in
	// command_run.go skips the runner but still mints the execution
	// documents; this directly exercises the same writer path.
	var rx *revisionExecution
	t.Run("step06_run_dry_run_setup", func(t *testing.T) {
		var err error
		rx, err = setupRevisionExecution(ctx, plan, nil, "exec-e2e-001")
		if err != nil {
			t.Fatalf("setupRevisionExecution: %v", err)
		}
		if rx.execKey == "" {
			t.Fatal("setupRevisionExecution returned empty execKey")
		}
		f.execKey = rx.execKey
		// finalize projects the ExecutionCounts it is handed into the
		// execution's ExecSummary (passed directly, not read from a store).
		if _, err := finalizeRevisionExecution(ctx, rx, execmodel.ExecutionCounts{Total: 1, Completed: 1}, nil); err != nil {
			t.Fatalf("finalizeRevisionExecution: %v", err)
		}
	})

	// Step 7: revisions/<key>/executions/<execKey>/execution.json must exist
	// and report status=completed.
	t.Run("step07_execution_json_terminal_status", func(t *testing.T) {
		execPath := statestore.ExecutionDocPath(f.revKey, f.execKey)
		raw, _, err := store.Read(ctx, execPath)
		if err != nil {
			t.Fatalf("read %s: %v", execPath, err)
		}
		body := string(raw)
		if !strings.Contains(body, `"status": "completed"`) {
			t.Fatalf("execution.json status not completed:\n%s", body)
		}
	})

	// Step 8: refs/latest-execution.json must point at the new execution.
	t.Run("step08_latest_execution_ref_updated", func(t *testing.T) {
		var ref struct {
			ExecutionKey string `json:"executionKey"`
			RevisionKey  string `json:"revisionKey"`
		}
		f.readJSON(t, "refs/latest-execution.json", &ref)
		if ref.ExecutionKey != f.execKey {
			t.Fatalf("latest-execution executionKey = %q; want %q", ref.ExecutionKey, f.execKey)
		}
		if ref.RevisionKey != f.revKey {
			t.Fatalf("latest-execution revisionKey = %q; want %q", ref.RevisionKey, f.revKey)
		}
	})

	// Step 9: indexes/executions/<execKey>.json must exist and match.
	t.Run("step09_execution_index_written", func(t *testing.T) {
		var entry statestore.ExecutionIndexEntry
		f.readJSON(t, "indexes/executions/"+f.execKey+".json", &entry)
		if entry.ExecutionKey != f.execKey {
			t.Fatalf("index executionKey = %q; want %q", entry.ExecutionKey, f.execKey)
		}
		if entry.RevisionKey != f.revKey {
			t.Fatalf("index revisionKey = %q; want %q", entry.RevisionKey, f.revKey)
		}
	})

	// ---- Steps 12–13: read-side CLI surface ----------------------------
	// (Steps 10–11, the legacy execution-read resolver, were removed at the
	// M12 cutover: execution reads now go through the object graph.)
	// Step 12: `orun describe revision latest` — the literal "latest"
	// must normalize to "" and surface the revision key + trigger field.
	t.Run("step12_describe_revision_latest", func(t *testing.T) {
		out := captureStdout(t, func() error { return describeRevision("latest") })
		if !strings.Contains(out, f.revKey) {
			t.Fatalf("describe output missing revision key %q:\n%s", f.revKey, out)
		}
		if !strings.Contains(out, "Trigger Key:") {
			t.Fatalf("describe output missing 'Trigger Key:' field:\n%s", out)
		}
	})

	// Step 13: `orun get plans` — the revision row must appear in the
	// table projected from manifest.json.
	t.Run("step13_get_plans_lists_revision", func(t *testing.T) {
		rows, ok, err := loadRevisionPlanRows()
		if err != nil {
			t.Fatalf("loadRevisionPlanRows: %v", err)
		}
		if !ok {
			t.Fatal("loadRevisionPlanRows reported empty layout; want at least 1 row")
		}
		found := false
		for _, row := range rows {
			if row.RevisionKey == f.revKey {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("rows missing revision %q: %+v", f.revKey, rows)
		}
	})

	// Steps 14–15 (`orun state migrate` idempotence) were removed with the
	// legacy state-migrate command at the M12 cutover: there is no legacy
	// .orun/ file store left to ingest. The revision documents this walk
	// builds (steps 1–13 above) are the migration target, and their
	// determinism is already asserted where they are written.
}
