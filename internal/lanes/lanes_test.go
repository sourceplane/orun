package lanes

import (
	"encoding/json"
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
