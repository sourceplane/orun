package planner

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

// When a dependency target isn't in the plan and the edge is the
// default order-only (if-selected) policy, the planner must silently
// skip it rather than erroring.
func TestResolveDependencies_MissingIfSelectedIsSkipped(t *testing.T) {
	jobInstances := map[string]*model.JobInstance{
		"api.pr.apply": {ID: "api.pr.apply", Component: "api", Environment: "pr"},
	}
	compInstances := map[string][]*model.ComponentInstance{
		"pr": {
			{ComponentName: "api", Environment: "pr", DependsOn: []model.ResolvedDependency{
				{ComponentName: "db", Environment: "pr", Include: model.IncludeIfSelected},
			}},
		},
	}

	jp := &JobPlanner{}
	if err := jp.resolveDependencies(jobInstances, compInstances); err != nil {
		t.Fatalf("if-selected missing dep should be a no-op, got %v", err)
	}
	if len(jobInstances["api.pr.apply"].DependsOn) != 0 {
		t.Errorf("expected no dependency edge, got %v", jobInstances["api.pr.apply"].DependsOn)
	}
}

// When the include policy is "always" and the dep is missing, the
// planner must report it as a real misconfiguration.
func TestResolveDependencies_MissingAlwaysErrors(t *testing.T) {
	jobInstances := map[string]*model.JobInstance{
		"api.pr.apply": {ID: "api.pr.apply", Component: "api", Environment: "pr"},
	}
	compInstances := map[string][]*model.ComponentInstance{
		"pr": {
			{ComponentName: "api", Environment: "pr", DependsOn: []model.ResolvedDependency{
				{ComponentName: "db", Environment: "pr", Include: model.IncludeAlways},
			}},
		},
	}

	jp := &JobPlanner{}
	err := jp.resolveDependencies(jobInstances, compInstances)
	if err == nil {
		t.Fatalf("expected error for missing include:always dependency")
	}
	if !strings.Contains(err.Error(), "include: always") {
		t.Errorf("expected error to mention include:always, got %v", err)
	}
}

// Empty Include must behave like IncludeIfSelected for legacy callers
// that constructed ResolvedDependency literals before the field existed.
func TestResolveDependencies_EmptyIncludeDefaultsToIfSelected(t *testing.T) {
	jobInstances := map[string]*model.JobInstance{
		"api.pr.apply": {ID: "api.pr.apply", Component: "api", Environment: "pr"},
	}
	compInstances := map[string][]*model.ComponentInstance{
		"pr": {
			{ComponentName: "api", Environment: "pr", DependsOn: []model.ResolvedDependency{
				{ComponentName: "db", Environment: "pr"}, // Include unset
			}},
		},
	}

	jp := &JobPlanner{}
	if err := jp.resolveDependencies(jobInstances, compInstances); err != nil {
		t.Fatalf("empty include should default to if-selected (no error), got %v", err)
	}
}
