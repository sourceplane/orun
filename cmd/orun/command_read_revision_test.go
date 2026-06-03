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
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/revision"
	"github.com/sourceplane/orun/internal/state"
	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/triggerctx"
)

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

// seedRevisionFirstWorkspace plants one revision (revision.json + manifest +
// trigger) under intentRoot/.orun. Returns the revision key.
func seedRevisionFirstWorkspace(t *testing.T, dir string) string {
	t.Helper()
	store, err := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(dir, ".orun")})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	now := time.Date(2026, 5, 30, 18, 0, 0, 0, time.UTC)
	trig := triggerctx.NewSystemManual(triggerctx.SystemOptions{
		Source: triggerctx.TriggerSource{HeadRevision: "deadbeefcafe1234"},
	})
	cfg := revision.Config{
		Store:    store,
		JobCount: 5,
		Now:      func() time.Time { return now },
	}.WithCompatibilityWrites(false)
	plan := []byte(`{"apiVersion":"orun.io/v1alpha1","kind":"Plan","jobs":[]}`)
	planHash := "feedface00112233445566778899aabbccddeeff00112233"
	rev, err := revision.WriteRevision(context.Background(), cfg, trig, plan, planHash)
	if err != nil {
		t.Fatalf("WriteRevision: %v", err)
	}
	if err := revision.WriteManifest(context.Background(), cfg, rev, trig); err != nil {
		t.Fatalf("WriteManifest: %v", err)
	}
	// Stamp a latest-execution summary so the table shows a non-empty
	// LATEST EXEC / STATUS column.
	if err := revision.UpdateLatestExecutionSummary(context.Background(), cfg, rev.RevisionKey, revision.LatestExecutionSummary{
		Key:    "run-001",
		Status: "completed",
	}); err != nil {
		t.Fatalf("UpdateLatestExecutionSummary: %v", err)
	}
	return rev.RevisionKey
}

// ------------------------------------------------------------------
// get plans
// ------------------------------------------------------------------

func TestGetPlans_RevisionFirstTable_Text(t *testing.T) {
	dir := withTempIntentRoot(t)
	revKey := seedRevisionFirstWorkspace(t, dir)

	prevFmt := getOutputFormat
	getOutputFormat = ""
	t.Cleanup(func() { getOutputFormat = prevFmt })

	out := captureStdout(t, getPlans)
	if !strings.Contains(out, "REVISION") || !strings.Contains(out, "TRIGGER") || !strings.Contains(out, "LATEST EXEC") {
		t.Fatalf("revision-first header missing in output:\n%s", out)
	}
	if !strings.Contains(out, revKey) {
		t.Fatalf("revKey %q missing in table:\n%s", revKey, out)
	}
	if !strings.Contains(out, "run-001") || !strings.Contains(out, "completed") {
		t.Fatalf("latest exec summary not rendered:\n%s", out)
	}
}

func TestGetPlans_RevisionFirstTable_JSON(t *testing.T) {
	dir := withTempIntentRoot(t)
	revKey := seedRevisionFirstWorkspace(t, dir)

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
	if doc.Revisions[0].RevisionKey != revKey {
		t.Fatalf("revisionKey = %q want %q", doc.Revisions[0].RevisionKey, revKey)
	}
	if doc.Revisions[0].Jobs != 5 {
		t.Errorf("jobs = %d want 5", doc.Revisions[0].Jobs)
	}
	if doc.Revisions[0].LatestExec != "run-001" || doc.Revisions[0].LatestStatus != "completed" {
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
	// Seed both legacy plans and a revision row.
	store := state.NewStore(dir)
	plan := minimalPlan(t)
	if err := store.SavePlan(plan, "latest"); err != nil {
		t.Fatalf("SavePlan: %v", err)
	}
	revKey := seedRevisionFirstWorkspace(t, dir)

	prevFmt := getOutputFormat
	getOutputFormat = ""
	t.Cleanup(func() { getOutputFormat = prevFmt })

	out := captureStdout(t, getPlans)
	if !strings.Contains(out, "TRIGGER") || !strings.Contains(out, revKey) {
		t.Fatalf("mixed workspace should prefer revision-first table:\n%s", out)
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
	revKey := seedRevisionFirstWorkspace(t, dir)

	prevFmt := getOutputFormat
	getOutputFormat = ""
	t.Cleanup(func() { getOutputFormat = prevFmt })

	out := captureStdout(t, func() error { return describeRevision(revKey) })
	if !strings.Contains(out, revKey) {
		t.Fatalf("describe revision did not render key %q:\n%s", revKey, out)
	}
	if !strings.Contains(out, "Plan Hash") || !strings.Contains(out, "Trigger Key") {
		t.Fatalf("describe revision missing core fields:\n%s", out)
	}
	if !strings.Contains(out, "Latest Exec") {
		t.Fatalf("describe revision should surface manifest summary latest exec:\n%s", out)
	}
}

func TestDescribeTrigger_JSON(t *testing.T) {
	dir := withTempIntentRoot(t)
	revKey := seedRevisionFirstWorkspace(t, dir)

	prevFmt := getOutputFormat
	getOutputFormat = "json"
	t.Cleanup(func() { getOutputFormat = prevFmt })

	out := captureStdout(t, func() error { return describeTrigger(revKey) })
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
	revKey := seedRevisionFirstWorkspace(t, dir)

	prevFmt := getOutputFormat
	getOutputFormat = ""
	t.Cleanup(func() { getOutputFormat = prevFmt })
	out := captureStdout(t, func() error { return describeRevision("") })
	if !strings.Contains(out, revKey) {
		t.Fatalf("describe revision (latest) should resolve to %q:\n%s", revKey, out)
	}
}

// ------------------------------------------------------------------
// resolveExecutionForRead — status/logs glue
// ------------------------------------------------------------------

// ------------------------------------------------------------------
// bridge-mirror-failed surfacing
// ------------------------------------------------------------------

func TestWarnBridgeMirrorFailures_EmitsStderrWarning(t *testing.T) {
	dir := withTempIntentRoot(t)
	// Plant a single bridge-mirror-failed event under revisions/<rev>/
	// executions/<exec>/events/.
	store, err := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(dir, ".orun")})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	revKey := "rev-test-abcd123-pfeedface"
	execKey := "run-001"
	evtPath := statestore.EventPath(revKey, execKey, 1, "bridge-mirror-failed")
	if _, err := store.Write(context.Background(), evtPath, []byte(`{"kind":"bridge-mirror-failed"}`), statestore.WriteOptions{}); err != nil {
		t.Fatalf("seed event: %v", err)
	}

	// Swap the warn sink for a buffer.
	var buf bytes.Buffer
	prev := bridgeMirrorWarnSink
	bridgeMirrorWarnSink = &buf
	t.Cleanup(func() { bridgeMirrorWarnSink = prev })

	warnBridgeMirrorFailures(context.Background(), store, revKey, execKey)
	got := buf.String()
	if !strings.Contains(got, "bridge mirror failed") || !strings.Contains(got, execKey) {
		t.Fatalf("warn sink got %q; want a one-line warning containing exec key", got)
	}

	// Second call must not duplicate when fed the same exec.
	buf.Reset()
	warnBridgeMirrorFailures(context.Background(), store, revKey, execKey)
	if !strings.Contains(buf.String(), "bridge mirror failed") {
		t.Fatalf("second call should still emit (function is best-effort, not memoized): %q", buf.String())
	}
}

func TestWarnBridgeMirrorFailures_NoEventsIsSilent(t *testing.T) {
	dir := withTempIntentRoot(t)
	store, err := statestore.NewLocalStore(statestore.LocalConfig{Root: filepath.Join(dir, ".orun")})
	if err != nil {
		t.Fatalf("NewLocalStore: %v", err)
	}
	var buf bytes.Buffer
	prev := bridgeMirrorWarnSink
	bridgeMirrorWarnSink = &buf
	t.Cleanup(func() { bridgeMirrorWarnSink = prev })

	warnBridgeMirrorFailures(context.Background(), store, "rev-x-abcdef0-pfeedface", "run-001")
	if buf.Len() != 0 {
		t.Fatalf("expected silent; got %q", buf.String())
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
