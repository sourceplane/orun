package services

import (
	"context"
	"errors"
	"time"
)

// ListRuns returns recent execution summaries, ordered newest-first.
//
// For the local backend, it reads the on-disk .orun/executions/ tree via
// state.Store. Remote-backend retrieval is deferred to a later phase
// alongside the rest of the remote-state implementation; callers that
// pass req.RemoteState today receive a not-implemented error so we do not
// fake remote behavior in the read-only Phase 1 surface.
func (s *LiveOrunService) ListRuns(ctx context.Context, req ListRunsRequest) ([]RunSummary, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if req.RemoteState {
		return nil, errors.New("ListRuns: remote-state backend not yet implemented (Phase 3)")
	}
	if s.cfg.Store == nil {
		return nil, errors.New("ListRuns: no state store configured")
	}

	entries, err := s.cfg.Store.ListExecutions()
	if err != nil {
		return nil, err
	}

	limit := req.Limit
	if limit <= 0 || limit > len(entries) {
		limit = len(entries)
	}

	runs := make([]RunSummary, 0, limit)
	for i := 0; i < limit; i++ {
		if err := ctx.Err(); err != nil {
			return runs, err
		}
		e := entries[i]
		started := parseTimestamp(e.StartedAt)
		finished := parseTimestampPtr(e.FinishedAt)
		var duration time.Duration
		if finished != nil && !started.IsZero() {
			duration = finished.Sub(started)
		}

		// Pull metadata for fields not on ExecEntry (planID, trigger,
		// dry-run flag).
		meta, _ := s.cfg.Store.LoadMetadata(e.ID)
		var (
			planID  string
			trigger string
			dryRun  bool
		)
		if meta != nil {
			planID = meta.PlanID
			trigger = meta.Trigger
			dryRun = meta.DryRun
		}

		runs = append(runs, RunSummary{
			ExecID:     e.ID,
			PlanID:     planID,
			PlanName:   e.PlanName,
			Status:     e.Status,
			JobTotal:   e.JobTotal,
			JobDone:    e.JobDone,
			JobFailed:  e.JobFailed,
			StartedAt:  started,
			FinishedAt: finished,
			Duration:   duration,
			Trigger:    trigger,
			DryRun:     dryRun,
		})
	}

	return runs, nil
}

func parseTimestamp(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339} {
		if t, err := time.Parse(layout, s); err == nil {
			return t
		}
	}
	return time.Time{}
}

func parseTimestampPtr(s string) *time.Time {
	t := parseTimestamp(s)
	if t.IsZero() {
		return nil
	}
	return &t
}
