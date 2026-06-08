package services

import (
	"context"
	"errors"
	"time"

	"github.com/sourceplane/orun/internal/objview"
)

// GetRunDetail loads the compiled plan and per-job statuses for a single
// execution from the content-addressed object graph.
//
// ListRuns deliberately stays cheap (header-only), so historical runs reach the
// Activity pane without a plan — which is why drilling into a job used to dead
// end at "step list unavailable". This fills that gap on demand: it reads the
// sealed execution view for real per-job statuses and decodes the revision's
// plan.json for the job/step definitions the drilldown enumerates. The step
// logs themselves are streamed separately by TailLogs, keyed by exec/job/step
// id, so once the plan is attached the existing log path just works.
//
// Best-effort by design: a missing object root, an unreadable execution, or a
// pruned plan blob degrade to a RunDetail with whatever was resolvable (often a
// nil Plan) rather than surfacing a blocking error.
func (s *LiveOrunService) GetRunDetail(ctx context.Context, req RunDetailRequest) (RunDetail, error) {
	if err := ctx.Err(); err != nil {
		return RunDetail{}, err
	}
	if req.RemoteState {
		return RunDetail{}, errors.New("GetRunDetail: remote-state backend not yet implemented")
	}
	if req.ExecID == "" {
		return RunDetail{}, errors.New("GetRunDetail: ExecID is required")
	}
	reader, ok := s.objReader()
	if !ok {
		return RunDetail{ExecID: req.ExecID}, nil
	}

	view, err := reader.Get(ctx, req.ExecID)
	if err != nil {
		// Best-effort: the run isn't readable from the object graph (yet).
		return RunDetail{ExecID: req.ExecID}, nil
	}

	statuses := make(map[string]string, len(view.Jobs))
	steps := map[string]StepInfo{}
	for _, j := range view.Jobs {
		statuses[j.JobID] = objview.NodeStatusToLegacy(j.Status)
		// Per-step records come from the job's most recent attempt — the same
		// lineage the step list and log tail read.
		if n := len(j.Attempts); n > 0 {
			for _, s := range j.Attempts[n-1].Steps {
				var dur time.Duration
				if s.StartedAt != nil && s.FinishedAt != nil {
					dur = s.FinishedAt.Sub(*s.StartedAt)
				}
				steps[StepDetailKey(j.JobID, s.StepID)] = StepInfo{
					Status:   objview.NodeStatusToLegacy(s.Status),
					Duration: dur,
					ExitCode: s.ExitCode,
				}
			}
		}
	}

	// Plan is best-effort: a pruned/legacy run keeps its statuses but loses
	// step enumeration, which the drilldown reports as unavailable.
	plan, _ := reader.Plan(ctx, view)

	return RunDetail{ExecID: req.ExecID, Plan: plan, Statuses: statuses, Steps: steps}, nil
}
