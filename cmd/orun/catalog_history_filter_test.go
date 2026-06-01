package main

// catalog_history_filter_test.go verifies the `orun catalog history` filter
// flags (--trigger, --profile, --environment) preserved through C8. It seeds a
// catalog-local ComponentExecutionIndex with three rows that vary by trigger,
// profile, and environment, then drives runCatalogHistory with each filter and
// asserts the row set narrows correctly. (The C5 history test only covered the
// empty-history path; this closes the filter-coverage gap.)

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/statestore"
)

// seedExecutionIndex writes a three-row ComponentExecutionIndex for svc-a into
// the refreshed catalog so history has something to filter.
func seedExecutionIndex(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	st, _, err := openLocalStateStore()
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	store := catalogstore.New(st)
	cat, err := store.ResolveCatalog(ctx, catalogstore.RefSelector{Kind: "current"})
	if err != nil {
		t.Fatalf("resolve catalog: %v", err)
	}

	idx := catalogmodel.ComponentExecutionIndex{
		APIVersion:         catalogmodel.APIVersionV1Alpha1,
		Kind:               catalogmodel.KindComponentExecIndex,
		ComponentKey:       "default/orun/svc-a",
		SourceSnapshotKey:  cat.SourceSnapshotKey,
		CatalogSnapshotKey: cat.CatalogSnapshotKey,
		Executions: []catalogmodel.ComponentExecutionRow{
			{
				RevisionKey: "rev-1", ExecutionKey: "exec-1",
				TriggerName: "github-push-main", Profile: "worker.release", Environment: "production",
				Status: "succeeded", CreatedAt: "2026-06-01T00:00:03Z",
			},
			{
				RevisionKey: "rev-2", ExecutionKey: "exec-2",
				TriggerName: "github-push-main", Profile: "worker.verify", Environment: "staging",
				Status: "succeeded", CreatedAt: "2026-06-01T00:00:02Z",
			},
			{
				RevisionKey: "rev-3", ExecutionKey: "exec-3",
				TriggerName: "system.manual", Profile: "worker.verify", Environment: "production",
				Status: "failed", CreatedAt: "2026-06-01T00:00:01Z",
			},
		},
	}
	p, err := catalogstore.ComponentLocalIndexPath(cat.SourceSnapshotKey, cat.CatalogSnapshotKey, "svc-a")
	if err != nil {
		t.Fatalf("index path: %v", err)
	}
	body, err := catalogmodel.PrettyEncode(idx)
	if err != nil {
		t.Fatalf("encode index: %v", err)
	}
	if _, err := st.Write(ctx, p, body, statestore.WriteOptions{}); err != nil {
		t.Fatalf("write index: %v", err)
	}
}

// historyRows runs runCatalogHistory in JSON mode and returns the decoded rows.
func historyRows(t *testing.T) []catalogmodel.ComponentExecutionRow {
	t.Helper()
	catalogJSONFlag = true
	out := captureStdout(t, func() error { return runCatalogHistory(nil, "svc-a") })
	var env catalogEnvelope
	var rows []catalogmodel.ComponentExecutionRow
	env.Data = &rows
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("history envelope: %v\n%s", err, out)
	}
	return rows
}

func TestCatalogHistory_Filters(t *testing.T) {
	refreshSeededCatalog(t)
	seedExecutionIndex(t)
	t.Cleanup(func() {
		catalogJSONFlag = false
		catalogHistoryTriggerFlag = ""
		catalogHistoryProfileFlag = ""
		catalogHistoryEnvFlag = ""
	})

	// No filter: all three rows, newest-first.
	if rows := historyRows(t); len(rows) != 3 {
		t.Fatalf("unfiltered rows = %d, want 3", len(rows))
	}

	// --trigger narrows to the two github-push-main rows.
	catalogHistoryTriggerFlag = "github-push-main"
	rows := historyRows(t)
	if len(rows) != 2 {
		t.Errorf("--trigger rows = %d, want 2 (%+v)", len(rows), rows)
	}
	for _, r := range rows {
		if r.TriggerName != "github-push-main" {
			t.Errorf("trigger filter leaked row %+v", r)
		}
	}
	catalogHistoryTriggerFlag = ""

	// --profile narrows to the two worker.verify rows.
	catalogHistoryProfileFlag = "worker.verify"
	rows = historyRows(t)
	if len(rows) != 2 {
		t.Errorf("--profile rows = %d, want 2 (%+v)", len(rows), rows)
	}
	catalogHistoryProfileFlag = ""

	// --environment narrows to the two production rows.
	catalogHistoryEnvFlag = "production"
	rows = historyRows(t)
	if len(rows) != 2 {
		t.Errorf("--environment rows = %d, want 2 (%+v)", len(rows), rows)
	}
	catalogHistoryEnvFlag = ""

	// Combined filters AND together: github-push-main + production → 1 row.
	catalogHistoryTriggerFlag = "github-push-main"
	catalogHistoryEnvFlag = "production"
	rows = historyRows(t)
	if len(rows) != 1 || rows[0].ExecutionKey != "exec-1" {
		t.Errorf("combined filter rows = %+v, want [exec-1]", rows)
	}
}
