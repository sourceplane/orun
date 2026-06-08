package main

// catalog_history_filter_test.go unit-tests the `orun catalog history` filter
// logic (--trigger, --profile, --environment) directly against filterHistoryRows.
//
// History was re-pointed onto the object-model execution graph
// (specs/orun-legacy-retirement Bucket 1), which does not record per-execution
// profile/environment/triggerName, so the end-to-end filter path can no longer
// be seeded via the legacy ComponentExecutionIndex. The filter logic itself is
// unchanged and still worth covering, so it is exercised directly here.

import (
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

func TestFilterHistoryRows(t *testing.T) {
	rows := []catalogmodel.ComponentExecutionRow{
		{ExecutionKey: "exec-1", TriggerName: "github-push-main", Profile: "worker.release", Environment: "production", Status: "succeeded"},
		{ExecutionKey: "exec-2", TriggerName: "github-push-main", Profile: "worker.verify", Environment: "staging", Status: "succeeded"},
		{ExecutionKey: "exec-3", TriggerName: "system.manual", Profile: "worker.verify", Environment: "production", Status: "failed"},
	}
	t.Cleanup(func() {
		catalogHistoryTriggerFlag = ""
		catalogHistoryProfileFlag = ""
		catalogHistoryEnvFlag = ""
	})

	keys := func(rs []catalogmodel.ComponentExecutionRow) []string {
		out := make([]string, 0, len(rs))
		for _, r := range rs {
			out = append(out, r.ExecutionKey)
		}
		return out
	}

	// No filter: all rows pass through unchanged.
	if got := filterHistoryRows(rows); len(got) != 3 {
		t.Fatalf("unfiltered = %v, want 3 rows", keys(got))
	}

	// --trigger narrows to the two github-push-main rows.
	catalogHistoryTriggerFlag = "github-push-main"
	if got := filterHistoryRows(rows); len(got) != 2 || got[0].ExecutionKey != "exec-1" || got[1].ExecutionKey != "exec-2" {
		t.Errorf("--trigger = %v, want [exec-1 exec-2]", keys(got))
	}
	catalogHistoryTriggerFlag = ""

	// --profile narrows to the two worker.verify rows.
	catalogHistoryProfileFlag = "worker.verify"
	if got := filterHistoryRows(rows); len(got) != 2 {
		t.Errorf("--profile = %v, want 2 rows", keys(got))
	}
	catalogHistoryProfileFlag = ""

	// --environment narrows to the two production rows.
	catalogHistoryEnvFlag = "production"
	if got := filterHistoryRows(rows); len(got) != 2 {
		t.Errorf("--environment = %v, want 2 rows", keys(got))
	}
	catalogHistoryEnvFlag = ""

	// Combined filters AND together: github-push-main + production → exec-1.
	catalogHistoryTriggerFlag = "github-push-main"
	catalogHistoryEnvFlag = "production"
	if got := filterHistoryRows(rows); len(got) != 1 || got[0].ExecutionKey != "exec-1" {
		t.Errorf("combined = %v, want [exec-1]", keys(got))
	}
}
