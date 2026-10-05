package planner

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

// Two environments on different profiles of the same composition, where one
// profile overrides a step's run. The template cache used to key on
// composition:step:field only, so whichever variant rendered first (map order)
// leaked into every job.
func TestPlanJobs_ProfileStepOverridesDoNotShareTemplateCache(t *testing.T) {
	job := &model.JobSpec{Name: "validate", Steps: []model.Step{
		{Name: "plan", Run: "terraform -chdir={{.parameters.dir}} plan"},
	}}
	comps := map[string]*CompositionInfo{
		"terraform": {
			Type:   "terraform",
			JobMap: map[string]*model.JobSpec{"validate": job},
			ExecutionProfiles: map[string]model.ExecutionProfile{
				"pr": {Jobs: map[string]model.ProfileJobSpec{"validate": {
					StepsEnabled:  []string{"plan"},
					StepOverrides: map[string]model.ProfileStepPatch{"plan": {Run: "terraform -chdir={{.parameters.dir}} plan -lock=false"}},
				}}},
				"release": {Jobs: map[string]model.ProfileJobSpec{"validate": {
					StepsEnabled: []string{"plan"},
				}}},
			},
		},
	}
	inst := func(env, profile string) *model.ComponentInstance {
		return &model.ComponentInstance{
			ComponentName: "net", Environment: env, Type: "terraform", Enabled: true,
			ProfileName: profile, ProfileSource: "subscription",
			Parameters: map[string]interface{}{"dir": "."},
		}
	}
	instances := map[string][]*model.ComponentInstance{
		"dev":  {inst("dev", "pr")},
		"prod": {inst("prod", "release")},
	}
	want := map[string]string{
		"net.dev.validate":  "terraform -chdir=. plan -lock=false",
		"net.prod.validate": "terraform -chdir=. plan",
	}
	for i := 0; i < 30; i++ {
		jobs, err := NewJobPlanner(comps).PlanJobs(instances)
		if err != nil {
			t.Fatal(err)
		}
		for id, run := range want {
			if got := jobs[id].Steps[0].Run; got != run {
				t.Fatalf("iteration %d: %s run = %q, want %q", i, id, got, run)
			}
		}
	}
}
