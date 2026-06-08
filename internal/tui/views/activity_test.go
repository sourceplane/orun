package views

import (
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/tui/services"
)

func TestActivity_SetRunsPinsLiveOnTop(t *testing.T) {
	m := NewActivityModel()
	live := &ActivityRun{ExecID: "live-1", Status: "running", Live: true}
	hist := []services.RunSummary{{ExecID: "old-1", Status: "completed"}}
	m = m.SetRuns(live, hist)
	if len(m.Runs) != 2 || m.Runs[0].ExecID != "live-1" {
		t.Fatalf("expected live first, got: %+v", m.Runs)
	}
}

func TestActivity_DrillDownAndPop(t *testing.T) {
	m := NewActivityModel()
	m = m.SetSize(160, 40)
	plan := &model.Plan{Jobs: []model.PlanJob{
		{ID: "j1", Component: "api", Steps: []model.PlanStep{
			{ID: "s1", Run: "echo hi"},
			{ID: "s2", Run: "echo bye"},
		}},
	}}
	m = m.SetRuns(&ActivityRun{
		ExecID: "exec-1", Live: true, PlanName: "demo",
		Plan: plan, Statuses: map[string]string{"j1": "running"},
	}, nil)

	if m.Level != LevelIndex {
		t.Fatalf("default level should be index, got %v", m.Level)
	}
	// index → run
	m, _ = m.Update(keyMsg("enter"))
	if m.Level != LevelRun {
		t.Fatalf("after enter at index want LevelRun got %v", m.Level)
	}
	// run → job
	m, _ = m.Update(keyMsg("enter"))
	if m.Level != LevelJob {
		t.Fatalf("after enter at run want LevelJob got %v", m.Level)
	}
	if id := m.SelectedJobID(); id != "j1" {
		t.Fatalf("expected job j1 got %q", id)
	}
	// job → step
	_, cmd := m.Update(keyMsg("enter"))
	if cmd == nil {
		t.Fatalf("expected tail-logs cmd when drilling into a step")
	}
	m, _ = m.Update(keyMsg("enter"))
	if m.Level != LevelStep {
		t.Fatalf("after enter at job want LevelStep got %v", m.Level)
	}
	// esc pops one level at a time
	for _, want := range []ActivityLevel{LevelJob, LevelRun, LevelIndex} {
		m, _ = m.Update(keyMsg("esc"))
		if m.Level != want {
			t.Fatalf("after esc want %v got %v", want, m.Level)
		}
	}
	// At root: further esc is reported via AtRoot() so the root model
	// can route the event up to global navBack.
	if !m.AtRoot() {
		t.Fatal("expected AtRoot at LevelIndex")
	}
}

// TestActivity_HistoricalRunRequestsDetailOnDrillIn verifies that opening a
// plan-less historical run emits a load request (so the root model can hydrate
// it) and that, until hydrated, the job page reports the steps as unavailable.
func TestActivity_HistoricalRunRequestsDetailOnDrillIn(t *testing.T) {
	m := NewActivityModel()
	m = m.SetSize(160, 40)
	m = m.SetRuns(nil, []services.RunSummary{
		{ExecID: "exec-h", Status: "completed", Components: []string{"cli"}},
	})

	// index → run should ask for the run's detail.
	m, cmd := m.Update(keyMsg("enter"))
	if m.Level != LevelRun {
		t.Fatalf("after enter want LevelRun got %v", m.Level)
	}
	if cmd == nil {
		t.Fatal("drilling into a historical run should emit a detail-load cmd")
	}
	msg, ok := cmd().(ActivityLoadRunDetailMsg)
	if !ok {
		t.Fatalf("expected ActivityLoadRunDetailMsg, got %T", cmd())
	}
	if msg.ExecID != "exec-h" {
		t.Fatalf("load request exec = %q, want exec-h", msg.ExecID)
	}

	// run → job: with no plan yet, the job page must report unavailability.
	m, _ = m.Update(keyMsg("enter"))
	if got := m.renderJobPage(); !strings.Contains(got, "step list unavailable") {
		t.Fatalf("plan-less job page should show unavailable hint, got:\n%s", got)
	}
}

// TestActivity_SetRunDetailEnablesStepDrilldown verifies that once a historical
// run's plan + statuses are merged in, its steps enumerate and the user can
// drill all the way into a step (which kicks off log tailing).
func TestActivity_SetRunDetailEnablesStepDrilldown(t *testing.T) {
	m := NewActivityModel()
	m = m.SetSize(160, 40)
	m = m.SetRuns(nil, []services.RunSummary{
		{ExecID: "exec-h", Status: "completed", Components: []string{"cli"}},
	})

	plan := &model.Plan{Jobs: []model.PlanJob{
		{ID: "j1", Component: "cli", Steps: []model.PlanStep{
			{ID: "s1", Run: "echo hi"},
			{ID: "s2", Run: "echo bye"},
		}},
	}}
	stepRecs := map[string]services.StepInfo{
		services.StepDetailKey("j1", "s1"): {Status: "completed", Duration: 1200 * time.Millisecond},
		services.StepDetailKey("j1", "s2"): {Status: "failed", Duration: 800 * time.Millisecond},
	}
	m = m.SetRunDetail("exec-h", plan, map[string]string{"j1": "completed"}, stepRecs)

	// index → run → job
	m, _ = m.Update(keyMsg("enter"))
	m, _ = m.Update(keyMsg("enter"))
	if m.Level != LevelJob {
		t.Fatalf("want LevelJob got %v", m.Level)
	}
	if id := m.SelectedJobID(); id != "j1" {
		t.Fatalf("selected job = %q, want j1", id)
	}
	if n := len(m.selectedSteps()); n != 2 {
		t.Fatalf("hydrated run should enumerate 2 steps, got %d", n)
	}
	// The step page now renders per-step status + duration.
	page := m.renderJobPage()
	for _, want := range []string{"done", "failed", "1.2s"} {
		if !strings.Contains(page, want) {
			t.Fatalf("step page missing %q:\n%s", want, page)
		}
	}
	// job → step emits a tail-logs request.
	_, cmd := m.Update(keyMsg("enter"))
	if cmd == nil {
		t.Fatal("drilling into a step should emit a tail-logs cmd")
	}
	if _, ok := cmd().(ActivityTailLogsMsg); !ok {
		t.Fatalf("expected ActivityTailLogsMsg, got %T", cmd())
	}
	m, _ = m.Update(keyMsg("enter"))
	if m.Level != LevelStep {
		t.Fatalf("after enter at job want LevelStep got %v", m.Level)
	}
}

// TestActivity_LoadDetailCmdSkipsLiveAndHydrated ensures we don't issue
// redundant detail loads for live runs (already carry a plan) or runs that have
// already been hydrated.
func TestActivity_LoadDetailCmdSkipsLiveAndHydrated(t *testing.T) {
	plan := &model.Plan{Jobs: []model.PlanJob{{ID: "j1", Component: "cli"}}}

	live := NewActivityModel().SetRuns(&ActivityRun{ExecID: "live", Live: true, Plan: plan}, nil)
	if cmd := live.LoadDetailCmd(); cmd != nil {
		t.Fatal("live run should not request detail")
	}

	hydrated := NewActivityModel().SetRuns(nil, []services.RunSummary{{ExecID: "exec-h"}})
	hydrated = hydrated.SetRunDetail("exec-h", plan, nil, nil)
	if cmd := hydrated.LoadDetailCmd(); cmd != nil {
		t.Fatal("already-hydrated run should not re-request detail")
	}
}

func TestActivity_RunPageGraphToggle(t *testing.T) {
	m := NewActivityModel()
	m = m.SetSize(160, 40)
	m = m.SetRuns(&ActivityRun{
		ExecID: "x", Live: true,
		Plan:     &model.Plan{Jobs: []model.PlanJob{{ID: "j1"}}},
		Statuses: map[string]string{},
	}, nil)
	m, _ = m.Update(keyMsg("enter"))
	if m.runMode != RunPageList {
		t.Fatalf("default run mode should be list")
	}
	m, _ = m.Update(keyMsg("v"))
	if m.runMode != RunPageGraph {
		t.Fatalf("v should toggle to graph")
	}
	m, _ = m.Update(keyMsg("v"))
	if m.runMode != RunPageList {
		t.Fatalf("v should toggle back to list")
	}
}

func TestActivity_ViewRendersLevelTitles(t *testing.T) {
	m := NewActivityModel()
	m = m.SetSize(160, 40)
	m = m.SetRuns(&ActivityRun{
		ExecID: "abc", Status: "running", Live: true, PlanName: "demo",
		Plan: &model.Plan{Jobs: []model.PlanJob{
			{ID: "j1", Component: "api", Steps: []model.PlanStep{{ID: "s1", Run: "echo hi"}}},
		}},
		Statuses: map[string]string{"j1": "running"},
	}, nil)

	if !strings.Contains(m.View(), "Activity") {
		t.Error("index view missing Activity title")
	}
	m, _ = m.Update(keyMsg("enter"))
	if !strings.Contains(m.View(), "Run") {
		t.Error("run view missing Run title")
	}
	m, _ = m.Update(keyMsg("enter"))
	out := m.View()
	if !strings.Contains(out, "Job") {
		t.Errorf("job view missing Job title: %s", out)
	}
}

func TestActivity_FocusLabel(t *testing.T) {
	m := NewActivityModel()
	cases := map[ActivityLevel]string{
		LevelIndex: "runs",
		LevelRun:   "run",
		LevelJob:   "job",
		LevelStep:  "step",
	}
	for lvl, want := range cases {
		m.Level = lvl
		if got := m.FocusLabel(); got != want {
			t.Errorf("level %v label: want %q got %q", lvl, want, got)
		}
	}
}

func TestActivity_InspectorDescPerLevel(t *testing.T) {
	m := NewActivityModel()
	m = m.SetRuns(&ActivityRun{
		ExecID: "exec-7", Live: true, PlanName: "demo",
		Plan: &model.Plan{Jobs: []model.PlanJob{
			{ID: "j1", Component: "api", Environment: "prod",
				DependsOn: []string{"j0"},
				Steps: []model.PlanStep{{ID: "s1", Run: "echo hi"}}},
		}},
		Statuses: map[string]string{"j1": "running"},
	}, nil)

	// Index → run desc
	if d := m.InspectorDesc(); d == nil || d.Kind != "run" {
		t.Fatalf("index level: want run desc, got %+v", d)
	}
	// Drill to run → job desc
	m, _ = m.Update(keyMsg("enter"))
	d := m.InspectorDesc()
	if d == nil || d.Kind != "job" {
		t.Fatalf("run level: want job desc, got %+v", d)
	}
	var sawComp, sawDeps bool
	for _, f := range d.Fields {
		if f.Label == "component" && f.Value == "api" {
			sawComp = true
		}
		if f.Label == "depends-on" && strings.Contains(f.Value, "j0") {
			sawDeps = true
		}
	}
	if !sawComp || !sawDeps {
		t.Errorf("job desc missing fields: comp=%v deps=%v", sawComp, sawDeps)
	}
	// Drill to job → step desc
	m, _ = m.Update(keyMsg("enter"))
	if d := m.InspectorDesc(); d == nil || d.Kind != "step" {
		t.Fatalf("job level: want step desc, got %+v", d)
	}
}

func TestActivity_InspectorDescRunWhenNoPlan(t *testing.T) {
	m := NewActivityModel()
	m = m.SetRuns(&ActivityRun{ExecID: "exec-9", Status: "completed"}, nil)
	d := m.InspectorDesc()
	if d == nil || d.Kind != "run" {
		t.Fatalf("expected run description, got %+v", d)
	}
}

func TestActivity_BreadcrumbGrowsWithLevel(t *testing.T) {
	m := NewActivityModel()
	m = m.SetSize(160, 40)
	m = m.SetRuns(&ActivityRun{
		ExecID: "exec-123", Live: true,
		Plan: &model.Plan{Jobs: []model.PlanJob{
			{ID: "j1", Component: "api", Steps: []model.PlanStep{{ID: "s1"}}},
		}},
		Statuses: map[string]string{"j1": "running"},
	}, nil)
	if got := m.Breadcrumb(); len(got) != 1 || got[0] != "activity" {
		t.Fatalf("index breadcrumb: %v", got)
	}
	m, _ = m.Update(keyMsg("enter"))
	if got := m.Breadcrumb(); len(got) < 2 || !strings.HasPrefix(got[1], "run ") {
		t.Fatalf("run breadcrumb: %v", got)
	}
	m, _ = m.Update(keyMsg("enter"))
	if got := m.Breadcrumb(); len(got) < 3 || !strings.HasPrefix(got[2], "job ") {
		t.Fatalf("job breadcrumb: %v", got)
	}
}

func TestActivity_BottomPanelContentVariesByLevel(t *testing.T) {
	m := NewActivityModel()
	m = m.SetSize(160, 40)
	m = m.SetRuns(&ActivityRun{
		ExecID: "exec-1", Live: true,
		Plan: &model.Plan{Jobs: []model.PlanJob{
			{ID: "j1", Component: "api",
				Steps: []model.PlanStep{{ID: "s1"}}},
		}},
		Statuses: map[string]string{"j1": "running"},
	}, nil)
	if got := m.BottomPanelContent(80); !strings.Contains(got, "OVERVIEW") {
		t.Errorf("index bottom: want OVERVIEW, got %q", got)
	}
	m, _ = m.Update(keyMsg("enter"))
	if got := m.BottomPanelContent(80); !strings.Contains(got, "RUN PROGRESS") {
		t.Errorf("run bottom: want RUN PROGRESS, got %q", got)
	}
	m, _ = m.Update(keyMsg("enter"))
	if got := m.BottomPanelContent(80); !strings.Contains(got, "JOB") {
		t.Errorf("job bottom: want JOB, got %q", got)
	}
}
