package lanes

import (
	"github.com/sourceplane/orun/internal/model"
)

// autoDrafts lays out the jobs auto mode is left with (§6). Until LN3 lands
// the packing, auto lays them out as job mode does: one lane per job.
func autoDrafts(plan *model.Plan, cfg *model.IntentLanes, pending []int, fixed int, g *jobGraph) []*draft {
	out := make([]*draft, 0, len(pending))
	for _, i := range pending {
		job := plan.Jobs[i]
		out = append(out, &draft{baseID: job.ID, env: job.Environment, runsOn: job.RunsOn, source: "job", component: job.Component, concurrency: 1, jobs: []int{i}})
	}
	return out
}

// renumberAuto refreshes k/n on auto lanes after a merge.
func renumberAuto(ds []*draft) {}
