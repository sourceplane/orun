package runbundle

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sourceplane/orun/internal/objrun"
)

// HydrateOptions controls hydration behavior.
type HydrateOptions struct {
	ExecID string
	Source ShardSource
	// Overwrite and IncludeRaw were inputs to the legacy file-store hydration.
	// The object graph is content-addressed and append-only — re-importing the
	// same run re-seals to the same id — so they are retained for caller
	// compatibility but no longer affect behavior.
	Overwrite  bool
	IncludeRaw bool
}

// HydrateResult describes what was imported.
type HydrateResult struct {
	// ExecID is the execution id the run was sealed under (opts.ExecID).
	ExecID string
	// RevisionID is the sealed object id of the ExecutionRun.
	RevisionID string
	// Root is the object-model root the run was sealed into.
	Root     string
	JobCount int
	LogFiles int
}

// Hydrate records a run pulled from elsewhere (a plan shard + job shards, e.g.
// from `orun github pull`) into the local content-addressed object graph under
// <orunDir>/objectmodel, sealing it through the same path the live runner uses
// (objrun.Seal). The imported execution is shaped identically to a natively-run
// one, so `orun status`/`orun logs` read it directly.
//
// It supersedes the original hydration, which rebuilt the legacy .orun/state
// file store the read commands no longer consult.
func Hydrate(ctx context.Context, planShard *PlanShard, jobShards []*JobShard, opts HydrateOptions, orunDir string) (*HydrateResult, error) {
	if planShard == nil {
		return nil, fmt.Errorf("plan shard is required")
	}
	if planShard.Plan == nil {
		return nil, fmt.Errorf("plan shard has no plan")
	}
	if opts.ExecID == "" {
		return nil, fmt.Errorf("exec ID is required")
	}
	if orunDir == "" {
		orunDir = ".orun"
	}

	// Synthesize the overall execution outcome (status, counts) from the shards.
	exec, err := Synthesize(planShard, jobShards)
	if err != nil {
		return nil, fmt.Errorf("synthesize failed: %w", err)
	}

	// Build the per-job projection (jobs + steps + step logs) from the job
	// shards, in a deterministic (sorted) order.
	shards := append([]*JobShard(nil), jobShards...)
	sort.Slice(shards, func(i, j int) bool {
		return jobShardID(shards[i]) < jobShardID(shards[j])
	})

	var (
		sealJobs []objrun.SealJob
		logFiles int
		startedAt, finishedAt time.Time
	)
	for _, js := range shards {
		if js == nil || js.JobState == nil {
			continue
		}
		jobID := jobShardID(js)
		if jobID == "" {
			continue
		}
		st := js.JobState

		sj := objrun.SealJob{
			JobID:     jobID,
			Status:    st.Status,
			LastError: st.LastError,
		}
		stepIDs := make([]string, 0, len(st.Steps))
		for sid := range st.Steps {
			stepIDs = append(stepIDs, sid)
		}
		sort.Strings(stepIDs)
		for _, sid := range stepIDs {
			step := objrun.SealStep{StepID: sid, Status: st.Steps[sid]}
			if log, ok := readStepLog(js, sid); ok {
				step.Log = log
				logFiles++
			}
			sj.Steps = append(sj.Steps, step)
		}
		sealJobs = append(sealJobs, sj)

		startedAt = earliest(startedAt, parseTime(st.StartedAt))
		finishedAt = latest(finishedAt, parseTime(st.FinishedAt))
	}

	if startedAt.IsZero() {
		startedAt = parseTime(exec.CreatedAt)
	}
	if finishedAt.IsZero() {
		finishedAt = time.Now().UTC()
	}

	root := filepath.Join(orunDir, "objectmodel")
	sealedID, err := objrun.Seal(ctx, root, planShard.Plan, objrun.ImportInput{
		ExecID:     opts.ExecID,
		Status:     exec.Status,
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		Jobs:       sealJobs,
		Links:      sourceLinks(opts.Source),
	})
	if err != nil {
		return nil, fmt.Errorf("seal imported run: %w", err)
	}

	return &HydrateResult{
		ExecID:     opts.ExecID,
		RevisionID: string(sealedID),
		Root:       root,
		JobCount:   len(sealJobs),
		LogFiles:   logFiles,
	}, nil
}

// jobShardID returns the stable job identifier for a shard, preferring the
// manifest JobID and falling back to the JobUID.
func jobShardID(js *JobShard) string {
	if js == nil || js.Manifest == nil {
		return ""
	}
	if js.Manifest.JobID != "" {
		return js.Manifest.JobID
	}
	return js.Manifest.JobUID
}

// readStepLog reads a step's captured log from a job shard. Job shards store
// per-step logs at logs/<stepId>.log (logical name "log:<stepId>" in the
// manifest; see writer.copyLogs). Returns (nil, false) when no log is present.
func readStepLog(js *JobShard, stepID string) ([]byte, bool) {
	if js == nil || js.Dir == "" {
		return nil, false
	}
	// Prefer the manifest's recorded relative path, fall back to the convention.
	rel := ""
	if js.Manifest != nil {
		rel = js.Manifest.Files["log:"+stepID]
	}
	if rel == "" {
		rel = filepath.Join("logs", stepID+".log")
	}
	data, err := os.ReadFile(filepath.Join(js.Dir, rel))
	if err != nil || len(data) == 0 {
		return nil, false
	}
	return data, true
}

// sourceLinks builds the external links recorded on an imported execution from
// its shard provenance — currently the GitHub Actions run page.
func sourceLinks(src ShardSource) []objrun.SealLink {
	if src.Repository != "" && src.RunID != "" {
		label := "GitHub Actions"
		if src.Workflow != "" {
			label = src.Workflow
		}
		return []objrun.SealLink{{
			Label: label,
			URL:   fmt.Sprintf("https://github.com/%s/actions/runs/%s", src.Repository, src.RunID),
		}}
	}
	return nil
}

// parseTime parses an RFC3339 timestamp, returning the zero time on failure.
func parseTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t.UTC()
}

// earliest returns the earlier of two times, ignoring zero values.
func earliest(a, b time.Time) time.Time {
	switch {
	case a.IsZero():
		return b
	case b.IsZero():
		return a
	case b.Before(a):
		return b
	default:
		return a
	}
}

// latest returns the later of two times, ignoring zero values.
func latest(a, b time.Time) time.Time {
	switch {
	case a.IsZero():
		return b
	case b.IsZero():
		return a
	case b.After(a):
		return b
	default:
		return a
	}
}
