package main

// Tests for the M5.c read-side rewire of the four CLI consumers
// (status, logs, describe, get plans) against the revision-first state
// layout and the legacy fallback. Coverage:
//
//   - get plans: revision-first table happy path (--json + text),
//     legacy-only fallback (no revisions/), mixed workspace prefers new layout.
//   - status: --revision flag plumbed; legacy-only fallback resolves
//     via state.Store.ResolveExecID.
//   - logs: --exec-id flag plumbed; revision-first index path.
//   - describe revision/trigger: aliases route through revision.ResolveRevision.
//   - bridgeMirrorWarn: surfaces a stderr warning when an event exists
//     under revisions/<rev>/executions/<exec>/events/.
//
// Tests favor direct calls into the helper functions (loadRevisionPlanRows,
// renderRevisionPlanTable, describeRevision, warnBridgeMirrorFailures) plus
// stdout/stderr capture for the rendered output. Heavy end-to-end
// happy-path lifecycle is already exercised by command_run_revision_test.go.

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/runworktree"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// seedObjectModelRevision writes one 5-job revision + trigger to the object
// graph under dir/.orun via the production plan writer, returning the plan
// checksum (the resolvable describe/get ref) and the revision human key (the
// rendered key).
func seedObjectModelRevision(t *testing.T, dir string) (checksum, humanKey string) {
	t.Helper()
	checksum = "feedface00112233445566778899aabb"
	humanKey = "rev-test-feedfac-pdeadbeef"
	trig := triggerctx.TriggerOccurrence{
		TriggerName: "system.manual",
		TriggerKey:  "trg-manual-deadbeef",
		PlanScope:   triggerctx.PlanScope{Mode: "full"},
		CreatedAt:   time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC),
	}
	plan := &model.Plan{}
	plan.Metadata.Name = "test-plan"
	plan.Jobs = make([]model.PlanJob, 5)
	planBytes := []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"Plan","jobs":[]}`)
	writeObjectModelPlan(filepath.Join(dir, ".orun"), plan, planBytes, checksum, humanKey, trig, planCatalogResolution{})
	return checksum, humanKey
}

// seedObjectModelExecution seals one succeeded execution against the revision
// resolved from checksum, so `get plans` can surface a latest-execution column.
func seedObjectModelExecution(t *testing.T, checksum, execKey string) {
	t.Helper()
	store, refs, root, ok := openObjectStores()
	if !ok {
		t.Fatal("seedObjectModelExecution: no object graph (seed a revision first)")
	}
	revID, rok := objResolveRevisionRef(store, refs, checksum)
	if !rok {
		t.Fatalf("seedObjectModelExecution: resolve revision %q", checksum)
	}
	ctx := context.Background()
	mgr := runworktree.NewManager(store, refs, root)
	wt, err := mgr.Open(ctx, runworktree.OpenInput{
		ExecutionID:  "exec-" + execKey,
		ExecutionKey: execKey,
		RevisionID:   revID,
	})
	if err != nil {
		t.Fatalf("open worktree: %v", err)
	}
	if err := wt.Project([]runworktree.ProjectedJob{
		{JobID: "svc@deploy", Status: nodes.StatusSucceeded},
	}); err != nil {
		t.Fatalf("project jobs: %v", err)
	}
	if _, err := wt.Seal(ctx, nodes.StatusSucceeded, time.Time{}); err != nil {
		t.Fatalf("seal execution: %v", err)
	}
}

// captureStdout runs fn while os.Stdout is piped into a buffer; returns
// what was written.
func captureStdout(t *testing.T, fn func() error) string {
	t.Helper()
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	os.Stdout = w
	outCh := make(chan string, 1)
	go func() {
		b, _ := io.ReadAll(r)
		outCh <- string(b)
	}()
	fnErr := fn()
	w.Close()
	out := <-outCh
	os.Stdout = old
	if fnErr != nil {
		t.Fatalf("fn returned error: %v", fnErr)
	}
	return out
}

// ------------------------------------------------------------------
// get plans
// ------------------------------------------------------------------

func TestGetPlans_RevisionFirstTable_Text(t *testing.T) {
	dir := withTempIntentRoot(t)
	checksum, humanKey := seedObjectModelRevision(t, dir)
	seedObjectModelExecution(t, checksum, "run-001")

	prevFmt := getOutputFormat
	getOutputFormat = ""
	t.Cleanup(func() { getOutputFormat = prevFmt })

	out := captureStdout(t, getPlans)
	if !strings.Contains(out, "REVISION") || !strings.Contains(out, "TRIGGER") || !strings.Contains(out, "LATEST EXEC") {
		t.Fatalf("revision-first header missing in output:\n%s", out)
	}
	if !strings.Contains(out, humanKey) {
		t.Fatalf("revKey %q missing in table:\n%s", humanKey, out)
	}
	if !strings.Contains(out, "run-001") || !strings.Contains(out, "succeeded") {
		t.Fatalf("latest exec summary not rendered:\n%s", out)
	}
}

func TestGetPlans_RevisionFirstTable_JSON(t *testing.T) {
	dir := withTempIntentRoot(t)
	checksum, humanKey := seedObjectModelRevision(t, dir)
	seedObjectModelExecution(t, checksum, "run-001")

	prevFmt := getOutputFormat
	getOutputFormat = "json"
	t.Cleanup(func() { getOutputFormat = prevFmt })

	out := captureStdout(t, getPlans)
	var doc struct {
		Revisions []struct {
			RevisionKey  string `json:"revisionKey"`
			Trigger      string `json:"trigger"`
			Plan         string `json:"plan"`
			Jobs         int    `json:"jobs"`
			LatestExec   string `json:"latestExec"`
			LatestStatus string `json:"status"`
		} `json:"revisions"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &doc); err != nil {
		t.Fatalf("json unmarshal: %v\n%s", err, out)
	}
	if len(doc.Revisions) != 1 {
		t.Fatalf("want 1 revision row, got %d", len(doc.Revisions))
	}
	if doc.Revisions[0].RevisionKey != humanKey {
		t.Fatalf("revisionKey = %q want %q", doc.Revisions[0].RevisionKey, humanKey)
	}
	if doc.Revisions[0].Jobs != 5 {
		t.Errorf("jobs = %d want 5", doc.Revisions[0].Jobs)
	}
	if doc.Revisions[0].Trigger != "system.manual" {
		t.Errorf("trigger = %q want system.manual", doc.Revisions[0].Trigger)
	}
	if doc.Revisions[0].LatestExec != "run-001" || doc.Revisions[0].LatestStatus != "succeeded" {
		t.Errorf("latestExec/status = %q/%q", doc.Revisions[0].LatestExec, doc.Revisions[0].LatestStatus)
	}
	if !strings.HasSuffix(out, "\n") {
		t.Errorf("expected trailing newline")
	}
}

func TestGetPlans_NoRevisions_Empty(t *testing.T) {
	_ = withTempIntentRoot(t)
	prevFmt := getOutputFormat
	getOutputFormat = ""
	t.Cleanup(func() { getOutputFormat = prevFmt })

	// No revisions and no object model: `get plans` surfaces nothing. The legacy
	// .orun/plans/ store is no longer read (M12 cutover); old workspaces use
	// `orun objects migrate`.
	out := captureStdout(t, getPlans)
	if !strings.Contains(out, "No plans yet") {
		t.Fatalf("expected 'No plans yet', got:\n%s", out)
	}
}

func TestGetPlans_MixedWorkspace_PrefersRevisionFirst(t *testing.T) {
	dir := withTempIntentRoot(t)
	// A stray legacy plans file on disk must not disrupt the object-model
	// revision table: the legacy .orun/plans/ store is no longer read, so
	// `get plans` resolves entirely from the object graph.
	plansDir := filepath.Join(dir, ".orun", "plans")
	if err := os.MkdirAll(plansDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plansDir, "latest.json"), []byte(`{"metadata":{"name":"legacy"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	_, humanKey := seedObjectModelRevision(t, dir)

	prevFmt := getOutputFormat
	getOutputFormat = ""
	t.Cleanup(func() { getOutputFormat = prevFmt })

	out := captureStdout(t, getPlans)
	if !strings.Contains(out, "TRIGGER") || !strings.Contains(out, humanKey) {
		t.Fatalf("mixed workspace should render the object-model revision table:\n%s", out)
	}
}

func TestGetPlans_EmptyJSON_ReturnsArray(t *testing.T) {
	withTempIntentRoot(t)
	prevFmt := getOutputFormat
	getOutputFormat = "json"
	t.Cleanup(func() { getOutputFormat = prevFmt })
	out := captureStdout(t, getPlans)
	if strings.TrimSpace(out) != "[]" {
		t.Fatalf("empty workspace json output = %q; want []", out)
	}
}

// ------------------------------------------------------------------
// describe revision / trigger
// ------------------------------------------------------------------

func TestDescribeRevision_HappyPath(t *testing.T) {
	dir := withTempIntentRoot(t)
	checksum, humanKey := seedObjectModelRevision(t, dir)

	prevFmt := getOutputFormat
	getOutputFormat = ""
	t.Cleanup(func() { getOutputFormat = prevFmt })

	// Resolve by plan checksum (the object-model ref grammar describePlan uses).
	out := captureStdout(t, func() error { return describeRevision(checksum) })
	if !strings.Contains(out, humanKey) {
		t.Fatalf("describe revision did not render key %q:\n%s", humanKey, out)
	}
	if !strings.Contains(out, "Plan Hash") || !strings.Contains(out, "Trigger Key") {
		t.Fatalf("describe revision missing core fields:\n%s", out)
	}
	if !strings.Contains(out, "trg-manual-") {
		t.Fatalf("describe revision should surface the producing trigger:\n%s", out)
	}
}

func TestDescribeTrigger_JSON(t *testing.T) {
	dir := withTempIntentRoot(t)
	checksum, _ := seedObjectModelRevision(t, dir)

	prevFmt := getOutputFormat
	getOutputFormat = "json"
	t.Cleanup(func() { getOutputFormat = prevFmt })

	out := captureStdout(t, func() error { return describeTrigger(checksum) })
	var trig map[string]interface{}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &trig); err != nil {
		t.Fatalf("describe trigger json: %v\n%s", err, out)
	}
	if key, _ := trig["triggerKey"].(string); !strings.HasPrefix(key, "trg-manual-") {
		t.Errorf("trigger.triggerKey = %v (want prefix trg-manual-)", trig["triggerKey"])
	}
}

func TestDescribeRevision_Latest_EmptyArg(t *testing.T) {
	dir := withTempIntentRoot(t)
	_, humanKey := seedObjectModelRevision(t, dir)

	prevFmt := getOutputFormat
	getOutputFormat = ""
	t.Cleanup(func() { getOutputFormat = prevFmt })
	out := captureStdout(t, func() error { return describeRevision("") })
	if !strings.Contains(out, humanKey) {
		t.Fatalf("describe revision (latest) should resolve to %q:\n%s", humanKey, out)
	}
}

// ------------------------------------------------------------------
// Flag wiring smoke tests
// ------------------------------------------------------------------

func TestStatusFlagsRegistered(t *testing.T) {
	for _, name := range []string{"revision", "exec-id", "all"} {
		if statusCmd.Flags().Lookup(name) == nil {
			t.Errorf("status command missing flag --%s", name)
		}
	}
}

func TestLogsFlagsRegistered(t *testing.T) {
	for _, name := range []string{"revision", "exec-id"} {
		if logsCmd.Flags().Lookup(name) == nil {
			t.Errorf("logs command missing flag --%s", name)
		}
	}
}

func TestDescribeAliasesRegistered(t *testing.T) {
	want := map[string]bool{"revision": false, "trigger": false, "run": false}
	for _, sub := range describeCmd.Commands() {
		if _, ok := want[sub.Name()]; ok {
			want[sub.Name()] = true
		}
	}
	for k, v := range want {
		if !v {
			t.Errorf("describe missing subcommand %q", k)
		}
	}
}
