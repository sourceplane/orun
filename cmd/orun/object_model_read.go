package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/objread"
	"github.com/sourceplane/orun/internal/ui"
)

// object_model_read.go repoints the read commands at the content-addressed
// object graph when the object model is active (M12 T3, rows 16-18). It adapts
// objread's presentation-neutral views into the execmodel value types the
// existing renderers already consume, so the data *source* swaps behind the flag
// while the rendering is unchanged. When the object model is absent or has no
// matching execution, callers fall back to the legacy store — so flag-off
// behavior is byte-identical and coexistence is preserved until the T5 cutover.

// objectModelActive reports whether either object-model flag is set.
func objectModelActive() bool { return objectRunnerEnabled() || objectModelEnabled() }

// openObjectReader returns an objread.Reader over .orun/objectmodel when the
// object model is active AND already present on disk. ok=false means the caller
// should use the legacy store (object model off, or this workspace has no object
// graph yet).
func openObjectReader() (*objread.Reader, bool) {
	if !objectModelActive() {
		return nil, false
	}
	abs, err := filepath.Abs(filepath.Join(storeDir(), ".orun"))
	if err != nil {
		return nil, false
	}
	root := objectModelRoot(abs)
	// Only adopt the object model if it actually has content; openObjectModel
	// would otherwise create an empty store and hide legacy runs.
	if _, err := os.Stat(filepath.Join(root, "objects")); err != nil {
		return nil, false
	}
	store, refs, r, err := openObjectModel()
	if err != nil {
		return nil, false
	}
	return objread.New(store, refs, r), true
}

// nodeStatusToLegacy folds the object-model status vocabulary onto the legacy
// runner vocabulary the renderers expect (succeeded -> completed, etc.).
func nodeStatusToLegacy(s string) string {
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

// objViewToMeta projects an execution header into the legacy ExecMetadata shape.
func objViewToMeta(v objread.ExecutionView) *execmodel.ExecMetadata {
	return &execmodel.ExecMetadata{
		ExecID:     v.ExecutionID,
		Status:     nodeStatusToLegacy(v.Status),
		StartedAt:  fmtRFC3339(v.StartedAt),
		FinishedAt: fmtRFC3339Ptr(v.FinishedAt),
		DryRun:     v.DryRun,
		JobTotal:   v.Summary.JobsTotal,
		JobDone:    v.Summary.JobsSucceeded,
		JobFailed:  v.Summary.JobsFailed,
	}
}

// objViewToState projects an execution's jobs/steps into the legacy ExecState
// shape (statuses folded to the legacy vocabulary).
func objViewToState(v objread.ExecutionView) *execmodel.ExecState {
	st := &execmodel.ExecState{ExecID: v.ExecutionID, Jobs: map[string]*execmodel.JobState{}}
	for _, j := range v.Jobs {
		js := &execmodel.JobState{
			Status:     nodeStatusToLegacy(j.Status),
			StartedAt:  fmtRFC3339Ptr(j.StartedAt),
			FinishedAt: fmtRFC3339Ptr(j.FinishedAt),
			LastError:  j.LastError,
			Steps:      map[string]string{},
		}
		// Flatten the latest attempt's steps (the legacy model has no attempts).
		if n := len(j.Attempts); n > 0 {
			for _, s := range j.Attempts[n-1].Steps {
				js.Steps[s.StepID] = nodeStatusToLegacy(s.Status)
			}
		}
		st.Jobs[j.JobID] = js
	}
	return st
}

// objViewsToEntries projects execution headers into legacy listing rows.
func objViewsToEntries(views []objread.ExecutionView) []execmodel.ExecEntry {
	out := make([]execmodel.ExecEntry, 0, len(views))
	for _, v := range views {
		out = append(out, execmodel.ExecEntry{
			ID:         v.ExecutionID,
			Status:     nodeStatusToLegacy(v.Status),
			StartedAt:  fmtRFC3339(v.StartedAt),
			FinishedAt: fmtRFC3339Ptr(v.FinishedAt),
			JobTotal:   v.Summary.JobsTotal,
			JobDone:    v.Summary.JobsSucceeded,
			JobFailed:  v.Summary.JobsFailed,
		})
	}
	return out
}

// objStatusList renders the execution list from the object graph. handled=false
// (with nil error) means there is nothing in the object model — fall back to the
// legacy store.
func objStatusList(reader *objread.Reader) (handled bool, err error) {
	views, err := reader.List(context.Background())
	if err != nil || len(views) == 0 {
		return false, nil
	}
	return true, cockpitRenderRunList(objViewsToEntries(views))
}

// objLogEntries builds the legacy logEntry rows for an execution from the object
// graph, honoring the --job/--step/--failed filters and reading log content from
// the sealed log blobs (or the live working tree).
func objLogEntries(reader *objread.Reader, v objread.ExecutionView) []logEntry {
	ctx := context.Background()
	var out []logEntry
	for _, j := range v.Jobs {
		if logsJob != "" && !strings.Contains(j.JobID, logsJob) {
			continue
		}
		n := len(j.Attempts)
		if n == 0 {
			continue
		}
		for _, s := range j.Attempts[n-1].Steps {
			if logsStep != "" && !strings.Contains(s.StepID, logsStep) {
				continue
			}
			if !s.HasLog {
				continue
			}
			status := nodeStatusToLegacy(s.Status)
			if logsFailed && status != "failed" {
				continue
			}
			content, err := reader.StepLog(ctx, v, j.JobID, s.StepID)
			if err != nil {
				continue
			}
			trimmed := strings.TrimSpace(string(content))
			if trimmed == "" {
				continue
			}
			out = append(out, logEntry{jobID: j.JobID, stepID: s.StepID, status: status, content: trimmed})
		}
	}
	return out
}

// objShowLogs renders an execution's logs from the object graph. handled=false
// (nil error) means the ref was not found — fall back to the legacy store.
func objShowLogs(reader *objread.Reader, ref string, color bool) (handled bool, err error) {
	v, gerr := reader.Get(context.Background(), ref)
	if gerr != nil {
		return false, nil
	}
	meta := objViewToMeta(v)
	st := objViewToState(v)
	counts := executionCountsFromState(meta, st)
	entries := objLogEntries(reader, v)
	if len(entries) == 0 {
		fmt.Println(ui.Dim(color, "No logs for this run yet."))
		return true, nil
	}
	sortLogEntries(entries)
	entries = selectRelevantLogEntries(entries)
	renderLogEntries(v.ExecutionID, meta, counts, entries, color)
	return true, nil
}

// sortLogEntries orders log rows by component/env, then status, then job/step —
// the same ordering the legacy logs path uses.
func sortLogEntries(entries []logEntry) {
	sort.Slice(entries, func(i, j int) bool {
		ci, ei, _ := splitJobID(entries[i].jobID)
		cj, ej, _ := splitJobID(entries[j].jobID)
		if ci != cj {
			return ci < cj
		}
		if ei != ej {
			return ei < ej
		}
		oi := statusSortKey(entries[i].status)
		oj := statusSortKey(entries[j].status)
		if oi != oj {
			return oi < oj
		}
		if entries[i].jobID != entries[j].jobID {
			return entries[i].jobID < entries[j].jobID
		}
		return entries[i].stepID < entries[j].stepID
	})
}

// objStatusSingle renders one execution from the object graph. handled=false
// (nil error) means the ref was not found in the object model.
func objStatusSingle(reader *objread.Reader, ref string, color bool) (handled bool, err error) {
	v, gerr := reader.Get(context.Background(), ref)
	if gerr != nil {
		return false, nil
	}
	meta := objViewToMeta(v)
	st := objViewToState(v)
	if statusJSON {
		return true, renderExecutionJSON(v.ExecutionID, meta, st)
	}
	return true, renderExecution(v.ExecutionID, meta, st, color)
}
