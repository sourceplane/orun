package services

import (
	"context"
	"errors"
	"time"

	"github.com/sourceplane/orun/internal/objview"
)

// ListRuns returns recent execution summaries, ordered newest-first.
//
// For the local path it reads the content-addressed object graph (sealed
// executions + in-flight working trees) via objread. Remote-backend retrieval
// is deferred to a later phase alongside the rest of the remote-state
// implementation; callers that pass req.RemoteState today receive a
// not-implemented error so we do not fake remote behavior here. An empty
// workspace (no object graph yet) yields an empty list rather than an error.
func (s *LiveOrunService) ListRuns(ctx context.Context, req ListRunsRequest) ([]RunSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.RemoteState {
		return nil, errors.New("ListRuns: remote-state backend not yet implemented (Phase 3)")
	}
	reader, ok := s.objReader()
	if !ok {
		return nil, nil
	}

	views, err := reader.List(ctx)
	if err != nil {
		return nil, err
	}

	limit := req.Limit
	if limit <= 0 || limit > len(views) {
		limit = len(views)
	}

	runs := make([]RunSummary, 0, limit)
	for i := 0; i < limit; i++ {
		if err := ctx.Err(); err != nil {
			return runs, err
		}
		v := views[i]

		var finished *time.Time
		if v.FinishedAt != nil {
			ft := *v.FinishedAt
			finished = &ft
		}
		var duration time.Duration
		if finished != nil && !v.StartedAt.IsZero() {
			duration = finished.Sub(v.StartedAt)
		}

		// Plan name + components come from the revision's compiled plan
		// (best-effort; both empty when the plan is unreadable).
		planName, components := reader.PlanSummary(ctx, v)

		runs = append(runs, RunSummary{
			ExecID:     v.ExecutionID,
			PlanID:     shortObjectID(v.RevisionID),
			PlanName:   planName,
			Status:     objview.NodeStatusToLegacy(v.Status),
			JobTotal:   v.Summary.JobsTotal,
			JobDone:    v.Summary.JobsSucceeded,
			JobFailed:  v.Summary.JobsFailed,
			StartedAt:  v.StartedAt,
			FinishedAt: finished,
			Duration:   duration,
			Trigger:    "", // decode the trigger object later (non-fatal)
			DryRun:     v.DryRun,
			Components: components,
		})
	}

	return runs, nil
}

// shortObjectID truncates a content-hash object id for compact display in the
// History view's plan column.
func shortObjectID(id string) string {
	const n = 14
	if len(id) <= n {
		return id
	}
	return id[:n] + "…"
}
