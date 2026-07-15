package render

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func workflowPlan() *model.Plan {
	return &model.Plan{
		Metadata: model.PlanMetadata{Name: "p"},
		Jobs: []model.PlanJob{{
			ID:          "web@prod.deploy",
			Name:        "deploy",
			Component:   "web",
			Environment: "prod",
			Steps: []model.PlanStep{
				{Name: "notify", Workflow: "wf/notify.yaml", WorkflowDigest: "sha256:abc"},
			},
		}},
	}
}

func TestViewByComponentShowsWorkflowStep(t *testing.T) {
	out := NewPlanViewer(workflowPlan()).ViewByComponent("web")
	if !strings.Contains(out, "workflow: wf/notify.yaml") {
		t.Fatalf("component view should surface the workflow step:\n%s", out)
	}
}

func TestViewDAGLongShowsWorkflowStep(t *testing.T) {
	out := NewPlanViewer(workflowPlan()).SetLong(true).ViewDAG()
	if !strings.Contains(out, "workflow: wf/notify.yaml") {
		t.Fatalf("long DAG view should surface the workflow step:\n%s", out)
	}
}
