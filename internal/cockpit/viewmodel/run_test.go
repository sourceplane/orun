package viewmodel

import (
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/execmodel"
)

func TestBuildRunViewBasic(t *testing.T) {
	meta := &execmodel.ExecMetadata{
		ExecID:    "run-1",
		PlanID:    "a1b2c3",
		PlanName:  "release",
		Status:    "running",
		Trigger:   "manual",
		StartedAt: "2026-05-29T10:00:00Z",
	}
	st := &execmodel.ExecState{
		ExecID: "run-1",
		Jobs: map[string]*execmodel.JobState{
			"api.stage.deploy": {Status: "running", StartedAt: "2026-05-29T10:00:10Z"},
			"api.stage.verify": {Status: "pending"},
			"web.prod.deploy":  {Status: "completed", StartedAt: "2026-05-29T10:00:05Z", FinishedAt: "2026-05-29T10:00:15Z"},
			"db.prod.migrate":  {Status: "failed", LastError: "boom"},
		},
	}
	v := BuildRunView("run-1", meta, st)
	if v.PlanName != "release" {
		t.Fatalf("plan name: %q", v.PlanName)
	}
	if v.Counts.Total != 4 || v.Counts.Failed != 1 || v.Counts.Completed != 1 ||
		v.Counts.Running != 1 || v.Counts.Pending != 1 {
		t.Fatalf("counts: %+v", v.Counts)
	}
	if !v.MultiEnv {
		t.Fatal("expected multi-env detected")
	}
	if len(v.Groups) != 3 {
		t.Fatalf("groups: got %d, want 3", len(v.Groups))
	}
	// Failed first within a group (api: running before pending).
	apiGroup := findGroup(v.Groups, "api")
	if apiGroup == nil || apiGroup.Jobs[0].Status != "running" {
		t.Fatalf("api group order: %+v", apiGroup)
	}
	if got := v.Counts.Percent(); got != 50 {
		t.Fatalf("percent: %d", got)
	}
}

func TestBuildRunViewEmptyState(t *testing.T) {
	meta := &execmodel.ExecMetadata{Status: "pending", JobTotal: 3, JobDone: 0, JobFailed: 0}
	v := BuildRunView("x", meta, nil)
	if v.Counts.Total != 3 {
		t.Fatalf("expected fallback to metadata totals, got %+v", v.Counts)
	}
	if v.Counts.Pending != 3 {
		t.Fatalf("expected 3 pending, got %d", v.Counts.Pending)
	}
}

func TestBuildRunListViewSortsRunningFirst(t *testing.T) {
	entries := []execmodel.ExecEntry{
		{ID: "a", Status: "completed", StartedAt: "2026-05-29T09:00:00Z"},
		{ID: "b", Status: "running", StartedAt: "2026-05-29T08:00:00Z"},
		{ID: "c", Status: "failed", StartedAt: "2026-05-29T10:00:00Z"},
	}
	v := BuildRunListView(entries)
	if v.Runs[0].ExecID != "b" {
		t.Fatalf("expected running first, got %s", v.Runs[0].ExecID)
	}
	// non-running sorted by start time desc → c, then a
	if v.Runs[1].ExecID != "c" || v.Runs[2].ExecID != "a" {
		t.Fatalf("sort order: %v", []string{v.Runs[0].ExecID, v.Runs[1].ExecID, v.Runs[2].ExecID})
	}
}

func TestJobDuration(t *testing.T) {
	j := Job{
		StartedAt:  parseTime("2026-05-29T10:00:00Z"),
		FinishedAt: parseTime("2026-05-29T10:00:30Z"),
	}
	if got := j.Duration(time.Now()); got != 30*time.Second {
		t.Fatalf("duration: %v", got)
	}
	running := Job{StartedAt: parseTime("2026-05-29T10:00:00Z")}
	now := parseTime("2026-05-29T10:00:45Z")
	if got := running.Duration(now); got != 45*time.Second {
		t.Fatalf("running duration: %v", got)
	}
}

func findGroup(groups []ComponentGroup, name string) *ComponentGroup {
	for i := range groups {
		if groups[i].Component == name {
			return &groups[i]
		}
	}
	return nil
}
