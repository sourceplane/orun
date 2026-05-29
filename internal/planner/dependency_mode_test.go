package planner

import (
	"sort"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

// TestResolveDependencies_AdvisoryModeMovesEdges verifies that when a job's
// DependencyMode is advisory, the planner records dependencies on
// AdvisoryDependsOn rather than the blocking DependsOn slice.
func TestResolveDependencies_AdvisoryModeMovesEdges(t *testing.T) {
	jobInstances := map[string]*model.JobInstance{
		"db.pr.apply": {
			ID: "db.pr.apply", Component: "db", Environment: "pr",
		},
		"api.pr.apply": {
			ID: "api.pr.apply", Component: "api", Environment: "pr",
			DependencyMode:   model.DependencyModeAdvisory,
			DependencySource: "subscription-rule",
		},
	}
	compInstances := map[string][]*model.ComponentInstance{
		"pr": {
			{ComponentName: "db", Environment: "pr"},
			{ComponentName: "api", Environment: "pr", DependsOn: []model.ResolvedDependency{
				{ComponentName: "db", Environment: "pr"},
			}},
		},
	}

	jp := &JobPlanner{}
	if err := jp.resolveDependencies(jobInstances, compInstances); err != nil {
		t.Fatalf("resolveDependencies: %v", err)
	}

	api := jobInstances["api.pr.apply"]
	if len(api.DependsOn) != 0 {
		t.Errorf("advisory mode should leave DependsOn empty, got %v", api.DependsOn)
	}
	if len(api.AdvisoryDependsOn) != 1 || api.AdvisoryDependsOn[0] != "db.pr.apply" {
		t.Errorf("expected AdvisoryDependsOn=[db.pr.apply], got %v", api.AdvisoryDependsOn)
	}
}

// TestResolveDependencies_DisabledModeDrops verifies disabled mode produces
// no edges at all (neither blocking nor advisory).
func TestResolveDependencies_DisabledModeDrops(t *testing.T) {
	jobInstances := map[string]*model.JobInstance{
		"db.pr.apply": {ID: "db.pr.apply", Component: "db", Environment: "pr"},
		"api.pr.apply": {
			ID: "api.pr.apply", Component: "api", Environment: "pr",
			DependencyMode: model.DependencyModeDisabled,
		},
	}
	compInstances := map[string][]*model.ComponentInstance{
		"pr": {
			{ComponentName: "db", Environment: "pr"},
			{ComponentName: "api", Environment: "pr", DependsOn: []model.ResolvedDependency{
				{ComponentName: "db", Environment: "pr"},
			}},
		},
	}

	jp := &JobPlanner{}
	if err := jp.resolveDependencies(jobInstances, compInstances); err != nil {
		t.Fatalf("resolveDependencies: %v", err)
	}
	api := jobInstances["api.pr.apply"]
	if len(api.DependsOn) != 0 || len(api.AdvisoryDependsOn) != 0 {
		t.Errorf("disabled mode should drop all edges; got DependsOn=%v Advisory=%v",
			api.DependsOn, api.AdvisoryDependsOn)
	}
}

// TestResolveDependencies_EnforcedDefault verifies missing/enforced mode still
// emits blocking DependsOn edges.
func TestResolveDependencies_EnforcedDefault(t *testing.T) {
	jobInstances := map[string]*model.JobInstance{
		"db.prod.apply":  {ID: "db.prod.apply", Component: "db", Environment: "prod"},
		"api.prod.apply": {ID: "api.prod.apply", Component: "api", Environment: "prod"}, // unset mode
	}
	compInstances := map[string][]*model.ComponentInstance{
		"prod": {
			{ComponentName: "db", Environment: "prod"},
			{ComponentName: "api", Environment: "prod", DependsOn: []model.ResolvedDependency{
				{ComponentName: "db", Environment: "prod"},
			}},
		},
	}
	jp := &JobPlanner{}
	if err := jp.resolveDependencies(jobInstances, compInstances); err != nil {
		t.Fatalf("resolveDependencies: %v", err)
	}
	api := jobInstances["api.prod.apply"]
	sort.Strings(api.DependsOn)
	if len(api.DependsOn) != 1 || api.DependsOn[0] != "db.prod.apply" {
		t.Errorf("expected blocking edge, got %v", api.DependsOn)
	}
}
