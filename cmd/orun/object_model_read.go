package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/objread"
	"github.com/sourceplane/orun/internal/objview"
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

// objStatusList renders the execution list from the object graph. handled=false
// (with nil error) means there is nothing in the object model — fall back to the
// legacy store.
func objStatusList(reader *objread.Reader) (handled bool, err error) {
	views, err := reader.List(context.Background())
	if err != nil || len(views) == 0 {
		return false, nil
	}
	return true, cockpitRenderRunList(objview.ToEntries(views))
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
			status := objview.NodeStatusToLegacy(s.Status)
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
	meta := objview.ToMeta(v)
	st := objview.ToState(v)
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
	meta := objview.ToMeta(v)
	st := objview.ToState(v)
	if statusJSON {
		return true, renderExecutionJSON(v.ExecutionID, meta, st)
	}
	return true, renderExecution(v.ExecutionID, meta, st, color)
}
