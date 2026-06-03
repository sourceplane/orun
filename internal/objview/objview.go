// Package objview adapts the object model's presentation-neutral execution
// views (internal/objread) into the legacy execmodel value types the existing
// renderers and cockpit view-models consume. It is the single seam that lets the
// read commands, the TUI, and the cockpit read from the content-addressed graph
// without changing their rendering — the node status vocabulary is folded onto
// the runner vocabulary here (succeeded -> completed, cancelled -> failed).
package objview

import (
	"time"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/objread"
)

// NodeStatusToLegacy folds the object-model status vocabulary onto the legacy
// runner vocabulary the renderers expect.
func NodeStatusToLegacy(s string) string {
	switch s {
	case "succeeded":
		return "completed"
	case "failed", "cancelled":
		return "failed"
	case "running":
		return "running"
	default:
		return "pending"
	}
}

func fmtRFC3339(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func fmtRFC3339Ptr(t *time.Time) string {
	if t == nil {
		return ""
	}
	return fmtRFC3339(*t)
}

// ToMeta projects an execution header into the legacy ExecMetadata shape.
func ToMeta(v objread.ExecutionView) *execmodel.ExecMetadata {
	return &execmodel.ExecMetadata{
		ExecID:     v.ExecutionID,
		Status:     NodeStatusToLegacy(v.Status),
		StartedAt:  fmtRFC3339(v.StartedAt),
		FinishedAt: fmtRFC3339Ptr(v.FinishedAt),
		DryRun:     v.DryRun,
		JobTotal:   v.Summary.JobsTotal,
		JobDone:    v.Summary.JobsSucceeded,
		JobFailed:  v.Summary.JobsFailed,
	}
}

// ToState projects an execution's jobs/steps into the legacy ExecState shape
// (statuses folded to the legacy vocabulary).
func ToState(v objread.ExecutionView) *execmodel.ExecState {
	st := &execmodel.ExecState{ExecID: v.ExecutionID, Jobs: map[string]*execmodel.JobState{}}
	for _, j := range v.Jobs {
		js := &execmodel.JobState{
			Status:     NodeStatusToLegacy(j.Status),
			StartedAt:  fmtRFC3339Ptr(j.StartedAt),
			FinishedAt: fmtRFC3339Ptr(j.FinishedAt),
			LastError:  j.LastError,
			Steps:      map[string]string{},
		}
		if n := len(j.Attempts); n > 0 {
			for _, s := range j.Attempts[n-1].Steps {
				js.Steps[s.StepID] = NodeStatusToLegacy(s.Status)
			}
		}
		st.Jobs[j.JobID] = js
	}
	return st
}

// ToEntries projects execution headers into legacy listing rows.
func ToEntries(views []objread.ExecutionView) []execmodel.ExecEntry {
	out := make([]execmodel.ExecEntry, 0, len(views))
	for _, v := range views {
		out = append(out, execmodel.ExecEntry{
			ID:         v.ExecutionID,
			Status:     NodeStatusToLegacy(v.Status),
			StartedAt:  fmtRFC3339(v.StartedAt),
			FinishedAt: fmtRFC3339Ptr(v.FinishedAt),
			JobTotal:   v.Summary.JobsTotal,
			JobDone:    v.Summary.JobsSucceeded,
			JobFailed:  v.Summary.JobsFailed,
		})
	}
	return out
}
