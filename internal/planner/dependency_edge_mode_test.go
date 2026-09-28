package planner

import (
	"sort"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

// edgeModeFixture builds a prod lane where "api" depends on "migrate" and
// "peer" with the given lane mode and per-edge modes.
func edgeModeFixture(laneMode, migrateEdge, peerEdge string) (map[string]*model.JobInstance, map[string][]*model.ComponentInstance) {
	jobInstances := map[string]*model.JobInstance{
		"migrate.prod.deploy": {ID: "migrate.prod.deploy", Component: "migrate", Environment: "prod"},
		"peer.prod.deploy":    {ID: "peer.prod.deploy", Component: "peer", Environment: "prod"},
		"api.prod.deploy": {
			ID: "api.prod.deploy", Component: "api", Environment: "prod",
			DependencyMode: laneMode,
		},
	}
	compInstances := map[string][]*model.ComponentInstance{
		"prod": {
			{ComponentName: "migrate", Environment: "prod"},
			{ComponentName: "peer", Environment: "prod"},
			{ComponentName: "api", Environment: "prod", DependencyMode: laneMode, DependsOn: []model.ResolvedDependency{
				{ComponentName: "migrate", Environment: "prod", Mode: migrateEdge},
				{ComponentName: "peer", Environment: "prod", Mode: peerEdge},
			}},
		},
	}
	return jobInstances, compInstances
}

func resolveEdgeFixture(t *testing.T, laneMode, migrateEdge, peerEdge string) *model.JobInstance {
	t.Helper()
	jobs, comps := edgeModeFixture(laneMode, migrateEdge, peerEdge)
	jp := &JobPlanner{}
	if err := jp.resolveDependencies(jobs, comps); err != nil {
		t.Fatalf("resolveDependencies: %v", err)
	}
	api := jobs["api.prod.deploy"]
	sort.Strings(api.DependsOn)
	sort.Strings(api.AdvisoryDependsOn)
	return api
}

func assertEdges(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s = %v, want %v", label, got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("%s = %v, want %v", label, got, want)
		}
	}
}

// An advisory edge inside an enforced lane stops blocking, while the lane's
// other edges keep blocking.
func TestResolveDependencies_EdgeAdvisoryInEnforcedLane(t *testing.T) {
	api := resolveEdgeFixture(t, model.DependencyModeEnforced, "", model.DependencyModeAdvisory)
	assertEdges(t, "DependsOn", api.DependsOn, []string{"migrate.prod.deploy"})
	assertEdges(t, "AdvisoryDependsOn", api.AdvisoryDependsOn, []string{"peer.prod.deploy"})
}

// An enforced edge inside an advisory lane still blocks, while the lane's
// other edges stay advisory.
func TestResolveDependencies_EdgeEnforcedInAdvisoryLane(t *testing.T) {
	api := resolveEdgeFixture(t, model.DependencyModeAdvisory, model.DependencyModeEnforced, "")
	assertEdges(t, "DependsOn", api.DependsOn, []string{"migrate.prod.deploy"})
	assertEdges(t, "AdvisoryDependsOn", api.AdvisoryDependsOn, []string{"peer.prod.deploy"})
}

// A disabled edge is dropped entirely; the other edge follows the lane.
func TestResolveDependencies_EdgeDisabledInEnforcedLane(t *testing.T) {
	api := resolveEdgeFixture(t, model.DependencyModeEnforced, "", model.DependencyModeDisabled)
	assertEdges(t, "DependsOn", api.DependsOn, []string{"migrate.prod.deploy"})
	assertEdges(t, "AdvisoryDependsOn", api.AdvisoryDependsOn, nil)
}

// Without any edge mode the lane decides every edge, exactly as before.
func TestResolveDependencies_NoEdgeModeFollowsLane(t *testing.T) {
	api := resolveEdgeFixture(t, model.DependencyModeEnforced, "", "")
	assertEdges(t, "DependsOn", api.DependsOn, []string{"migrate.prod.deploy", "peer.prod.deploy"})
	assertEdges(t, "AdvisoryDependsOn", api.AdvisoryDependsOn, nil)

	api = resolveEdgeFixture(t, model.DependencyModeAdvisory, "", "")
	assertEdges(t, "DependsOn", api.DependsOn, nil)
	assertEdges(t, "AdvisoryDependsOn", api.AdvisoryDependsOn, []string{"migrate.prod.deploy", "peer.prod.deploy"})

	// Legacy path: no lane mode populated at all defaults to enforced.
	api = resolveEdgeFixture(t, "", "", "")
	assertEdges(t, "DependsOn", api.DependsOn, []string{"migrate.prod.deploy", "peer.prod.deploy"})
}
