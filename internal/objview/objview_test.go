package objview

import (
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objread"
)

func summaryStub(total, ok, failed, steps int) nodes.ExecSummary {
	return nodes.ExecSummary{JobsTotal: total, JobsSucceeded: ok, JobsFailed: failed, StepsTotal: steps}
}

func TestNodeStatusToLegacy(t *testing.T) {
	cases := map[string]string{
		"succeeded": "completed",
		"failed":    "failed",
		"cancelled": "failed",
		"running":   "running",
		"pending":   "pending",
		"weird":     "pending",
	}
	for in, want := range cases {
		if got := NodeStatusToLegacy(in); got != want {
			t.Fatalf("NodeStatusToLegacy(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestToMetaAndState(t *testing.T) {
	start := time.Date(2026, 6, 3, 10, 0, 0, 0, time.UTC)
	fin := start.Add(time.Minute)
	v := objread.ExecutionView{
		ExecutionID: "exec_1",
		Status:      "succeeded",
		StartedAt:   start,
		FinishedAt:  &fin,
		Summary:     summaryStub(2, 1, 1, 3),
		Jobs: []objread.JobView{
			{JobID: "a@deploy", Status: "succeeded", Attempts: []objread.AttemptView{
				{Attempt: 1, Steps: []objread.StepView{{StepID: "build", Status: "succeeded"}}},
			}},
			{JobID: "b@deploy", Status: "failed", LastError: "boom", Attempts: []objread.AttemptView{
				{Attempt: 1, Steps: []objread.StepView{{StepID: "deploy", Status: "failed"}}},
			}},
		},
	}
	meta := ToMeta(v)
	if meta.Status != "completed" || meta.JobTotal != 2 || meta.JobDone != 1 || meta.JobFailed != 1 {
		t.Fatalf("meta wrong: %+v", meta)
	}
	if meta.StartedAt == "" || meta.FinishedAt == "" {
		t.Fatalf("meta times not formatted: %+v", meta)
	}
	st := ToState(v)
	if st.Jobs["a@deploy"].Status != "completed" || st.Jobs["a@deploy"].Steps["build"] != "completed" {
		t.Fatalf("state job a wrong: %+v", st.Jobs["a@deploy"])
	}
	if st.Jobs["b@deploy"].Status != "failed" || st.Jobs["b@deploy"].LastError != "boom" {
		t.Fatalf("state job b wrong: %+v", st.Jobs["b@deploy"])
	}
}

func TestToEntries(t *testing.T) {
	views := []objread.ExecutionView{
		{ExecutionID: "x", Status: "running", Summary: summaryStub(3, 1, 0, 0)},
	}
	rows := ToEntries(views)
	if len(rows) != 1 || rows[0].ID != "x" || rows[0].Status != "running" || rows[0].JobTotal != 3 || rows[0].JobDone != 1 {
		t.Fatalf("entries wrong: %+v", rows)
	}
}
