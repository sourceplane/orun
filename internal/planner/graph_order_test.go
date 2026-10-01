package planner

import (
	"fmt"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

// TopologicalSort must be a function of the graph alone (OMR1): a job's
// position becomes PlanJob.Index, which CI turns into the runner label that
// pins a runner to one lane, so two plans of the same intent must agree.
func TestTopologicalSort_IsDeterministic(t *testing.T) {
	build := func() *JobGraph {
		jobs := make(map[string]*model.JobInstance, 40)
		for i := 0; i < 20; i++ {
			id := fmt.Sprintf("root-%02d", i)
			jobs[id] = &model.JobInstance{ID: id}
		}
		for i := 0; i < 20; i++ {
			id := fmt.Sprintf("leaf-%02d", i)
			jobs[id] = &model.JobInstance{
				ID:        id,
				DependsOn: []string{fmt.Sprintf("root-%02d", (i*7)%20), fmt.Sprintf("root-%02d", (i*3)%20)},
			}
		}
		return NewJobGraph(jobs)
	}
	first, err := build().TopologicalSort()
	if err != nil {
		t.Fatal(err)
	}
	for n := 0; n < 25; n++ {
		again, err := build().TopologicalSort()
		if err != nil {
			t.Fatal(err)
		}
		if fmt.Sprint(again) != fmt.Sprint(first) {
			t.Fatalf("order changed between plans:\n%v\n%v", first, again)
		}
	}
	// Roots come first, sorted by id, so the first entry is stable too.
	if first[0] != "root-00" {
		t.Fatalf("first = %q, want root-00", first[0])
	}
}
