package remotestate

import (
	"encoding/json"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

// The plan blob carries each job's index, runs-on and labels (OMR1): the
// platform maps a GitHub lane's ghr-orun-job:<run>-<index> label back to its
// plan job through the index, and knows what the lane asked to run on.
func TestConvertPlan_CarriesIndexRunsOnAndLabels(t *testing.T) {
	plan := &model.Plan{
		Metadata: model.PlanMetadata{Name: "t"},
		Jobs: []model.PlanJob{
			{ID: "a.dev.terraform", Component: "a", Index: 0, RunsOn: "ubuntu-22.04", Labels: map[string]string{"scope": "infra"}},
			{ID: "b.dev.terraform", Component: "b", Index: 1, DependsOn: []string{"a.dev.terraform"}},
		},
	}
	bp := ConvertPlan(plan)
	if len(bp.Jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(bp.Jobs))
	}
	if bp.Jobs[0].Index != 0 || bp.Jobs[1].Index != 1 {
		t.Fatalf("indices = %d,%d, want 0,1", bp.Jobs[0].Index, bp.Jobs[1].Index)
	}
	if bp.Jobs[0].RunsOn != "ubuntu-22.04" {
		t.Fatalf("runsOn = %q", bp.Jobs[0].RunsOn)
	}
	if bp.Jobs[0].Labels["scope"] != "infra" {
		t.Fatalf("labels = %v", bp.Jobs[0].Labels)
	}

	// Index is never omitted: 0 is a real position, and a consumer must be
	// able to tell "first job" from "no index".
	raw, err := json.Marshal(bp.Jobs[0])
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	if v, ok := m["index"]; !ok || v != float64(0) {
		t.Fatalf("index missing or wrong in %s", raw)
	}
	if _, ok := m["labels"]; !ok {
		t.Fatalf("labels missing in %s", raw)
	}
}
