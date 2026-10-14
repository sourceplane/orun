package lanes

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func job(id, comp, env string, deps ...string) model.PlanJob {
	return model.PlanJob{ID: id, Name: "deploy", Component: comp, Environment: env, Composition: "worker", DependsOn: deps}
}

func checkName(j model.PlanJob) string { return j.Component + " · " + j.Environment + " · Deploy" }

func plan(jobs ...model.PlanJob) *model.Plan {
	p := &model.Plan{Jobs: jobs}
	for i := range p.Jobs {
		p.Jobs[i].Index = i
	}
	return p
}

func laneIDs(p *model.Plan) []string {
	var out []string
	for _, l := range p.Lanes {
		out = append(out, l.ID)
	}
	return out
}

func laneOf(p *model.Plan, id string) string {
	for _, j := range p.Jobs {
		if j.ID == id {
			return j.Lane
		}
	}
	return ""
}

func TestAssign_NoConfigLeavesPlanUntouched(t *testing.T) {
	p := plan(job("a.stage", "a", "stage"), job("b.stage", "b", "stage", "a.stage"))
	before, _ := json.Marshal(p)
	res, err := Assign(p, Options{ComponentLanes: map[string]string{"a": "isolated"}, CheckName: checkName})
	if err != nil {
		t.Fatal(err)
	}
	after, _ := json.Marshal(p)
	if string(before) != string(after) || res.Mode != "" {
		t.Fatalf("a plan without execution.lanes must be byte-identical:\n%s\n%s", before, after)
	}
	if strings.Contains(string(after), `"lane`) {
		t.Fatalf("no lane keys may appear: %s", after)
	}
}

func TestAssign_JobModeIsOneLanePerJobNamedByCheck(t *testing.T) {
	p := plan(job("a.stage", "a", "stage"), job("b.stage", "b", "stage", "a.stage"))
	if _, err := Assign(p, Options{Config: &model.IntentLanes{}, ComponentLanes: map[string]string{"a": "isolated"}, CheckName: checkName}); err != nil {
		t.Fatal(err)
	}
	if got := laneIDs(p); !reflect.DeepEqual(got, []string{"a.stage", "b.stage"}) {
		t.Fatalf("job mode lanes = %v", got)
	}
	if p.Lanes[0].Name != "a · stage · Deploy" || p.Lanes[0].Source != "job" || p.Lanes[0].Concurrency != 1 {
		t.Fatalf("job lane = %+v", p.Lanes[0])
	}
	if laneOf(p, "b.stage") != "b.stage" {
		t.Fatalf("job's lane must be its own id")
	}
}

func TestAssign_RulesSelectModeByTrigger(t *testing.T) {
	cfg := &model.IntentLanes{Mode: "env", Rules: []model.LaneRule{{Mode: "job", When: model.ProfileRuleWhen{TriggerRef: "pr"}}}}
	if EffectiveMode(cfg, []string{"pr"}) != "job" || EffectiveMode(cfg, []string{"main"}) != "env" {
		t.Fatalf("rules did not select the mode")
	}
	if EffectiveMode(&model.IntentLanes{}, nil) != "job" {
		t.Fatalf("default mode must be job")
	}
}

func TestAssign_EnvModeWithGroupsIsolatedAndRunsOn(t *testing.T) {
	big := job("console.stage", "console", "stage")
	gpu := job("ml.stage", "ml", "stage")
	gpu.RunsOn = "gpu-large"
	p := plan(
		job("migrate.stage", "migrate", "stage"),
		job("billing.stage", "billing", "stage", "migrate.stage"),
		job("config.stage", "config", "stage"),
		big, gpu,
		job("billing.prod", "billing", "prod"),
	)
	cfg := &model.IntentLanes{Mode: "env", Concurrency: 5, Groups: map[string]model.LaneGroup{"fleet": {Concurrency: 8}}}
	opts := Options{Config: cfg, CheckName: checkName, ComponentLanes: map[string]string{"billing": "fleet", "config": "fleet", "console": "isolated"}}
	if _, err := Assign(p, opts); err != nil {
		t.Fatal(err)
	}
	got := map[string]model.PlanLane{}
	for _, l := range p.Lanes {
		got[l.ID] = l
	}
	if l := got["stage/fleet"]; l.Source != "group" || l.Concurrency != 8 || !reflect.DeepEqual(l.Jobs, []string{"billing.stage", "config.stage"}) || l.Name != "stage · fleet · 2 jobs" {
		t.Fatalf("group lane = %+v", l)
	}
	if l := got["stage/console"]; l.Source != "isolated" || l.Name != "stage · console · isolated" {
		t.Fatalf("isolated lane = %+v", l)
	}
	// Two env lanes on stage (default runner and gpu) must not share an id.
	if _, ok := got["stage-default"]; !ok {
		t.Fatalf("env lanes split by runsOn must be disambiguated, got %v", laneIDs(p))
	}
	if l := got["stage-gpu-large"]; l.RunsOn != "gpu-large" || !reflect.DeepEqual(l.Jobs, []string{"ml.stage"}) {
		t.Fatalf("runsOn lane = %+v", l)
	}
	if l := got["prod/fleet"]; !reflect.DeepEqual(l.Jobs, []string{"billing.prod"}) || l.Name != "prod · fleet · 1 job" {
		t.Fatalf("a group never crosses environments: %+v", l)
	}
	// Environments in order of first appearance; prod after stage.
	if p.Lanes[len(p.Lanes)-1].Environment != "prod" {
		t.Fatalf("lane order = %v", laneIDs(p))
	}
	for i, l := range p.Lanes {
		if l.Index != i {
			t.Fatalf("lane index %d at position %d", l.Index, i)
		}
	}
}

func TestAssign_ComponentMode(t *testing.T) {
	a := job("a.stage.verify", "a", "stage")
	a2 := job("a.stage.deploy", "a", "stage", "a.stage.verify")
	p := plan(a, a2, job("b.stage", "b", "stage"))
	if _, err := Assign(p, Options{Config: &model.IntentLanes{Mode: "component"}, CheckName: checkName}); err != nil {
		t.Fatal(err)
	}
	if got := laneIDs(p); !reflect.DeepEqual(got, []string{"stage/a", "stage/b"}) {
		t.Fatalf("component lanes = %v", got)
	}
	if p.Lanes[0].Name != "stage · a" || len(p.Lanes[0].Jobs) != 2 {
		t.Fatalf("component lane = %+v", p.Lanes[0])
	}
}

// cyclePlan: data holds migrate (level 0) and export (level 2); fleet holds
// notifications (level 1). migrate → notifications → export makes the two
// groups depend on each other.
func cyclePlan() (*model.Plan, Options) {
	p := plan(
		job("migrate.stage", "migrate", "stage"),
		job("notifications.stage", "notifications", "stage", "migrate.stage"),
		job("export.stage", "export", "stage", "notifications.stage"),
	)
	cfg := &model.IntentLanes{Mode: "env", Groups: map[string]model.LaneGroup{"data": {}, "fleet": {}}}
	return p, Options{Config: cfg, CheckName: checkName, ComponentLanes: map[string]string{"migrate": "data", "export": "data", "notifications": "fleet"}}
}

func TestAssign_CycleIsSplitByLevel(t *testing.T) {
	p, opts := cyclePlan()
	res, err := Assign(p, opts)
	if err != nil {
		t.Fatal(err)
	}
	if got := laneIDs(p); !reflect.DeepEqual(got, []string{"stage/data-1", "stage/fleet", "stage/data-2"}) {
		t.Fatalf("split lanes = %v", got)
	}
	if laneOf(p, "migrate.stage") != "stage/data-1" || laneOf(p, "export.stage") != "stage/data-2" {
		t.Fatalf("jobs not in their slices")
	}
	if p.Lanes[0].SplitFrom != "stage/data" || p.Lanes[0].Reason != "cycle" || p.Lanes[0].Name != "stage · data·1 · 1 job" {
		t.Fatalf("split lane = %+v", p.Lanes[0])
	}
	if len(res.Repairs) != 1 || !strings.Contains(res.Repairs[0], "split stage/data into stage/data-1, stage/data-2") ||
		!strings.Contains(res.Repairs[0], "migrate.stage → notifications.stage") {
		t.Fatalf("repairs = %v", res.Repairs)
	}
}

func TestAssign_CycleRejectedWhenAsked(t *testing.T) {
	for _, viaStrict := range []bool{false, true} {
		p, opts := cyclePlan()
		if viaStrict {
			opts.Strict = true
		} else {
			opts.Config.OnCycle = "reject"
		}
		_, err := Assign(p, opts)
		if err == nil || !strings.Contains(err.Error(), "stage/data → stage/fleet → stage/data") {
			t.Fatalf("strict=%v: want the cycle named, got %v", viaStrict, err)
		}
	}
}

func TestAssign_AdvisoryEdgesMakeNoCycle(t *testing.T) {
	p, opts := cyclePlan()
	// notifications → export becomes advisory: no lane edge back to data.
	p.Jobs[2].DependsOn = nil
	p.Jobs[2].AdvisoryDependsOn = []string{"notifications.stage"}
	res, err := Assign(p, opts)
	if err != nil || len(res.Repairs) != 0 {
		t.Fatalf("advisory edges must not force a split: %v %v", err, res.Repairs)
	}
	if got := laneIDs(p); !reflect.DeepEqual(got, []string{"stage/data", "stage/fleet"}) {
		t.Fatalf("lanes = %v", got)
	}
}

func TestAssign_Deterministic(t *testing.T) {
	var first string
	for i := 0; i < 20; i++ {
		p, opts := cyclePlan()
		if _, err := Assign(p, opts); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(p)
		if i == 0 {
			first = string(b)
		} else if string(b) != first {
			t.Fatalf("run %d differs:\n%s\n%s", i, first, b)
		}
	}
}

func TestListComponentsDropsCommonSuffix(t *testing.T) {
	p := plan(job("a", "billing-worker", "s"), job("b", "config-worker", "s"), job("c", "events-worker", "s"), job("d", "identity-worker", "s"))
	d := &draft{jobs: []int{0, 1, 2, 3}}
	if got := listComponents(p, d); got != "billing, config, events +1" {
		t.Fatalf("listComponents = %q", got)
	}
}

// cirrusLike is shaped like the cirrus plan: 12 workers on dev, stage and
// prod (stage waits on dev, prod on stage), 12 tests, and db-migrate on stage
// and prod that notifications waits on.
func cirrusLike() *model.Plan {
	workers := []string{"billing", "config", "events", "identity", "notifications", "policy", "search", "storage", "audit", "export", "webhooks", "metering"}
	var jobs []model.PlanJob
	for _, w := range workers {
		t := job(w+"-worker.test", w+"-worker", "test")
		t.Composition = "test"
		jobs = append(jobs, t)
	}
	for _, env := range []string{"dev", "stage", "prod"} {
		if env != "dev" {
			jobs = append(jobs, job("db-migrate."+env, "db-migrate", env))
		}
		for _, w := range workers {
			var deps []string
			switch env {
			case "stage":
				deps = append(deps, w+"-worker.dev")
			case "prod":
				deps = append(deps, w+"-worker.stage")
			}
			if w == "notifications" {
				if env != "dev" {
					deps = append(deps, "db-migrate."+env)
				}
				deps = append(deps, "events-worker."+env)
			}
			jobs = append(jobs, job(w+"-worker."+env, w+"-worker", env, deps...))
		}
	}
	return plan(jobs...)
}

func TestAuto_PacksIntoMaxLanesKeepingChainsTogether(t *testing.T) {
	p := cirrusLike()
	if len(p.Jobs) != 50 {
		t.Fatalf("fixture has %d jobs", len(p.Jobs))
	}
	res, err := Assign(p, Options{Config: &model.IntentLanes{Mode: "auto", MaxLanes: 16}, CheckName: checkName})
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Lanes) > 16 || len(p.Lanes) < 4 {
		t.Fatalf("auto made %d lanes: %v", len(p.Lanes), laneIDs(p))
	}
	if len(res.Repairs) != 0 {
		t.Fatalf("auto must not need splits here: %v", res.Repairs)
	}
	for _, env := range []string{"stage", "prod"} {
		m, e, n := laneOf(p, "db-migrate."+env), laneOf(p, "events-worker."+env), laneOf(p, "notifications-worker."+env)
		if m != n || e != n {
			t.Fatalf("%s: migrate %s, events %s, notifications %s must share a lane", env, m, e, n)
		}
	}
	seen := map[string]bool{}
	for _, l := range p.Lanes {
		if seen[l.ID] {
			t.Fatalf("duplicate lane id %s", l.ID)
		}
		seen[l.ID] = true
		if l.Source != "auto" || l.Concurrency != model.DefaultLaneConcurrency {
			t.Fatalf("auto lane = %+v", l)
		}
		for _, id := range l.Jobs {
			if laneOf(p, id) != l.ID {
				t.Fatalf("job %s stamped %s, listed in %s", id, laneOf(p, id), l.ID)
			}
		}
		if !strings.HasPrefix(l.ID, l.Environment+"/auto-") {
			t.Fatalf("auto lane id %s", l.ID)
		}
	}
	if !acyclicLanes(p) {
		t.Fatalf("lane graph has a cycle")
	}
}

func acyclicLanes(p *model.Plan) bool {
	g := newJobGraph(p.Jobs)
	idx := map[string]int{}
	var ds []*draft
	for i, l := range p.Lanes {
		idx[l.ID] = i
		ds = append(ds, &draft{})
	}
	for i, j := range p.Jobs {
		ds[idx[j.Lane]].jobs = append(ds[idx[j.Lane]].jobs, i)
	}
	return acyclic(g, ds)
}

func TestAuto_SmallPlanIsJobLayout(t *testing.T) {
	p := plan(job("a.stage", "a", "stage"), job("b.stage", "b", "stage", "a.stage"))
	if _, err := Assign(p, Options{Config: &model.IntentLanes{Mode: "auto"}, CheckName: checkName}); err != nil {
		t.Fatal(err)
	}
	if got := laneIDs(p); !reflect.DeepEqual(got, []string{"a.stage", "b.stage"}) || p.Lanes[0].Source != "job" {
		t.Fatalf("small auto plan lanes = %v", got)
	}
}

func TestAuto_IsolatedCountsAgainstBudget(t *testing.T) {
	p := cirrusLike()
	opts := Options{Config: &model.IntentLanes{Mode: "auto", MaxLanes: 8}, CheckName: checkName, ComponentLanes: map[string]string{"billing-worker": "isolated"}}
	if _, err := Assign(p, opts); err != nil {
		t.Fatal(err)
	}
	iso := 0
	for _, l := range p.Lanes {
		if l.Source == "isolated" {
			iso++
		}
	}
	if iso != 4 || len(p.Lanes) > 8+4 {
		t.Fatalf("lanes = %v", laneIDs(p))
	}
	if !acyclicLanes(p) {
		t.Fatalf("lane graph has a cycle")
	}
}

func TestAuto_Deterministic(t *testing.T) {
	var first string
	for i := 0; i < 20; i++ {
		p := cirrusLike()
		if _, err := Assign(p, Options{Config: &model.IntentLanes{Mode: "auto", MaxLanes: 10}, CheckName: checkName}); err != nil {
			t.Fatal(err)
		}
		b, _ := json.Marshal(p)
		if i == 0 {
			first = string(b)
		} else if string(b) != first {
			t.Fatalf("run %d differs", i)
		}
	}
}

func TestAllocateLanesIsProportional(t *testing.T) {
	sizes := []int{12, 12, 13, 13}
	got := make([]int, 4)
	allocateLanes(50, 16, 4, func(k int) int { return sizes[k] }, func(k, n int) { got[k] = n })
	sum := 0
	for _, n := range got {
		sum += n
		if n < 3 || n > 5 {
			t.Fatalf("alloc = %v", got)
		}
	}
	if sum != 16 {
		t.Fatalf("alloc = %v sums to %d", got, sum)
	}
}

// Each lane records the lanes and jobs it waits on and its level in the lane
// graph (specs/orun-runner-efficiency §5.1): 0 with no upstream lane, else
// one more than the deepest upstream lane.
func TestAssign_LanesCarryLevelAndUpstreams(t *testing.T) {
	p := plan(
		job("db.stage", "db", "stage"),
		job("api.stage", "api", "stage", "db.stage"),
		job("db.prod", "db", "prod", "db.stage"),
		job("api.prod", "api", "prod", "db.prod", "api.stage"),
	)
	if _, err := Assign(p, Options{Config: &model.IntentLanes{Mode: "component"}, CheckName: checkName}); err != nil {
		t.Fatal(err)
	}
	got := map[string]model.PlanLane{}
	for _, l := range p.Lanes {
		got[l.ID] = l
	}
	check := func(id string, level int, lanes, jobs []string) {
		t.Helper()
		l, ok := got[id]
		if !ok {
			t.Fatalf("no lane %s in %v", id, laneIDs(p))
		}
		if l.Level != level || !reflect.DeepEqual(l.UpstreamLanes, lanes) || !reflect.DeepEqual(l.UpstreamJobs, jobs) {
			t.Fatalf("%s: level %d after %v via %v; want level %d after %v via %v", id, l.Level, l.UpstreamLanes, l.UpstreamJobs, level, lanes, jobs)
		}
	}
	check("stage/db", 0, nil, nil)
	check("stage/api", 1, []string{"stage/db"}, []string{"db.stage"})
	check("prod/db", 1, []string{"stage/db"}, []string{"db.stage"})
	check("prod/api", 2, []string{"prod/db", "stage/api"}, []string{"api.stage", "db.prod"})

	// Everything in one lane waits on nothing outside it.
	p = plan(job("a.stage", "a", "stage"), job("b.stage", "b", "stage", "a.stage"))
	if _, err := Assign(p, Options{Config: &model.IntentLanes{Mode: "env"}, CheckName: checkName}); err != nil {
		t.Fatal(err)
	}
	if l := p.Lanes[0]; l.Level != 0 || l.UpstreamLanes != nil || l.UpstreamJobs != nil {
		t.Fatalf("env lane = %+v", l)
	}
}

// concurrency: auto is carried to the plan as 0, which `orun run --lane`
// resolves to the runner's core count (specs/orun-runner-efficiency §5.2).
// Job-mode lanes stay at 1: one job needs no slots.
func TestAssign_AutoConcurrencyIsZeroInPlan(t *testing.T) {
	cfg := &model.IntentLanes{Mode: "env", Concurrency: model.LaneConcurrencyAuto,
		Groups: map[string]model.LaneGroup{"fleet": {Concurrency: model.LaneConcurrencyAuto}, "big": {Concurrency: 8}}}
	p := plan(job("a.stage", "a", "stage"), job("b.stage", "b", "stage"), job("c.stage", "c", "stage"), job("d.stage", "d", "stage"))
	if _, err := Assign(p, Options{Config: cfg, ComponentLanes: map[string]string{"b": "fleet", "c": "big"}, CheckName: checkName}); err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, l := range p.Lanes {
		got[l.ID] = l.Concurrency
	}
	if got["stage"] != 0 || got["stage/fleet"] != 0 || got["stage/big"] != 8 {
		t.Fatalf("concurrency per lane = %v; want env and fleet auto (0), big 8", got)
	}
	p = plan(job("a.stage", "a", "stage"))
	if _, err := Assign(p, Options{Config: &model.IntentLanes{Concurrency: model.LaneConcurrencyAuto}, CheckName: checkName}); err != nil {
		t.Fatal(err)
	}
	if p.Lanes[0].Concurrency != 1 {
		t.Fatalf("a job-mode lane runs one job: concurrency %d", p.Lanes[0].Concurrency)
	}
}

// --max-lanes / ORUN_MAX_LANES override the intent's budget for one plan,
// and plan.metadata.lanes records the budget and where it came from.
func TestAssign_MaxLanesOverrideIsRecorded(t *testing.T) {
	var jobs []model.PlanJob
	for i := 0; i < 20; i++ {
		jobs = append(jobs, job(fmt.Sprintf("c%02d.stage", i), fmt.Sprintf("c%02d", i), "stage"))
	}
	cases := []struct {
		name  string
		cfg   *model.IntentLanes
		opts  Options
		lanes int
		meta  model.PlanLanesMeta
	}{
		{"default", &model.IntentLanes{Mode: "auto"}, Options{}, 16, model.PlanLanesMeta{Mode: "auto", MaxLanes: 16, MaxLanesFrom: "default"}},
		{"intent", &model.IntentLanes{Mode: "auto", MaxLanes: 8}, Options{}, 8, model.PlanLanesMeta{Mode: "auto", MaxLanes: 8, MaxLanesFrom: "intent"}},
		{"flag", &model.IntentLanes{Mode: "auto", MaxLanes: 8}, Options{MaxLanes: 4, MaxLanesFrom: "flag"}, 4, model.PlanLanesMeta{Mode: "auto", MaxLanes: 4, MaxLanesFrom: "flag"}},
		{"env", &model.IntentLanes{Mode: "auto"}, Options{MaxLanes: 5, MaxLanesFrom: "env"}, 5, model.PlanLanesMeta{Mode: "auto", MaxLanes: 5, MaxLanesFrom: "env"}},
	}
	for _, c := range cases {
		p := plan(jobs...)
		c.opts.Config, c.opts.CheckName = c.cfg, checkName
		if _, err := Assign(p, c.opts); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if len(p.Lanes) != c.lanes {
			t.Fatalf("%s: %d lanes, want %d: %v", c.name, len(p.Lanes), c.lanes, laneIDs(p))
		}
		if p.Metadata.Lanes == nil || *p.Metadata.Lanes != c.meta {
			t.Fatalf("%s: metadata.lanes = %+v, want %+v", c.name, p.Metadata.Lanes, c.meta)
		}
		if c.cfg.MaxLanes == 8 && c.opts.MaxLanes > 0 && c.cfg.MaxLanes != 8 {
			t.Fatalf("%s: the intent must not be mutated", c.name)
		}
	}
}
