package views

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestPlanToDAGNodes_DefaultsWaiting(t *testing.T) {
	plan := &model.Plan{Jobs: []model.PlanJob{
		{ID: "a", Component: "api"},
		{ID: "b", Component: "web", DependsOn: []string{"a"}},
	}}
	nodes := PlanToDAGNodes(plan, map[string]string{"a": "completed"})
	if len(nodes) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(nodes))
	}
	if nodes[0].Status != "completed" {
		t.Errorf("node a status: want completed, got %q", nodes[0].Status)
	}
	if nodes[1].Status != "waiting" {
		t.Errorf("node b status: want waiting (default), got %q", nodes[1].Status)
	}
	if len(nodes[1].DependsOn) != 1 || nodes[1].DependsOn[0] != "a" {
		t.Errorf("dep wiring lost: %v", nodes[1].DependsOn)
	}
}

func TestRenderDAG_TreeConnectors(t *testing.T) {
	nodes := []DAGNode{
		{ID: "root", Label: "root", Status: "completed"},
		{ID: "a", Label: "child-a", Status: "running", DependsOn: []string{"root"}},
		{ID: "b", Label: "child-b", Status: "waiting", DependsOn: []string{"root"}},
	}
	out := RenderDAG(nodes, "a", 60)
	if !strings.Contains(out, "├─") && !strings.Contains(out, "╰─") {
		t.Errorf("expected tree connectors, got:\n%s", out)
	}
	if !strings.Contains(out, "root") || !strings.Contains(out, "child-a") {
		t.Errorf("missing labels in output:\n%s", out)
	}
}

func TestRenderDAG_EmptyState(t *testing.T) {
	out := RenderDAG(nil, "", 40)
	if !strings.Contains(out, "no jobs") {
		t.Errorf("expected empty-state hint, got: %q", out)
	}
}

func TestDAGSummary_Counts(t *testing.T) {
	nodes := []DAGNode{
		{Status: "completed"}, {Status: "running"},
		{Status: "failed"}, {Status: "waiting"}, {Status: ""},
	}
	out := DAGSummary(nodes)
	for _, want := range []string{"5 jobs", "1 running", "1 done", "1 failed", "2 waiting"} {
		if !strings.Contains(out, want) {
			t.Errorf("summary missing %q in: %s", want, out)
		}
	}
}
