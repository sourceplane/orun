package runbundle

import (
	"fmt"
	"time"
)

// Synthesize merges one plan shard and a set of job shards into a
// SynthesizedExecution. Missing job shards produce status="partial".
func Synthesize(plan *PlanShard, jobs []*JobShard) (*SynthesizedExecution, error) {
	if plan == nil {
		return nil, fmt.Errorf("plan shard is required")
	}
	if plan.Manifest == nil {
		return nil, fmt.Errorf("plan shard manifest is required")
	}

	execID := plan.Manifest.ExecID
	planID := plan.Manifest.PlanID

	// Index job shards by JobUID
	jobMap := make(map[string]*JobShard, len(jobs))
	for _, j := range jobs {
		if j == nil || j.Manifest == nil {
			continue
		}
		uid := j.Manifest.JobUID
		if uid == "" {
			uid = j.Manifest.JobID
		}
		if uid != "" {
			jobMap[uid] = j
		}
	}

	// Build synthesis state
	var totalJobs int
	if plan.Plan != nil {
		totalJobs = len(plan.Plan.Jobs)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	exec := &SynthesizedExecution{
		ExecID:    execID,
		PlanID:    planID,
		CreatedAt: now,
		Source:    plan.Manifest.Source,
		PlanShard: ShardRef{
			Name: fmt.Sprintf("orun.v1.%s.%s.%s.%s", execID, ShardRolePlan, planID, "created"),
			Role: string(ShardRolePlan),
		},
		Counts: JobCounts{Total: totalJobs},
		Jobs:   make(map[string]JobShardRef),
	}

	// Process each plan job and match with available shards
	plannedJobs := make(map[string]bool)
	if plan.Plan != nil {
		for _, pj := range plan.Plan.Jobs {
			uid := pj.UID
			if uid == "" {
				uid = pj.ID
			}
			plannedJobs[uid] = true

			js, found := jobMap[uid]
			if !found {
				// Also try by JobID
				for uid2, j := range jobMap {
					if j.Manifest.JobID == pj.ID {
						js = j
						found = true
						delete(jobMap, uid2)
						jobMap[uid] = js
						break
					}
				}
			}

			if found && js != nil {
				exec.Jobs[uid] = shardRefFromJob(js)
				updateCounts(exec, js.Manifest.Status)
			} else {
				// Job was planned but has no shard — counts as pending
				exec.Counts.Pending++
				exec.Jobs[uid] = JobShardRef{
					JobUid: uid,
					JobID:  pj.ID,
					Status: "pending",
				}
			}
		}
	}

	// Check for orphan shards (not in the plan)
	var orphanFound bool
	for uid, js := range jobMap {
		if !plannedJobs[uid] {
			orphanFound = true
			exec.Jobs[uid] = shardRefFromJob(js)
			updateCounts(exec, js.Manifest.Status)
		}
	}

	// Determine overall status
	exec.Status = computeStatus(exec.Counts)
	if orphanFound {
		exec.PartialReason = "orphan_job_shards"
	}
	exec.Partial = exec.Status == "partial"

	return exec, nil
}

// shardRefFromJob builds a JobShardRef from a JobShard.
func shardRefFromJob(js *JobShard) JobShardRef {
	uid := js.Manifest.JobUID
	if uid == "" {
		uid = js.Manifest.JobID
	}
	return JobShardRef{
		JobUid:     js.Manifest.JobUID,
		JobID:      js.Manifest.JobID,
		Status:     js.Manifest.Status,
		ShardName:  ArtifactName(js.Manifest.ExecID, ShardRoleJob, uid, js.Manifest.Status),
		StartedAt:  js.Manifest.StartedAt,
		FinishedAt: js.Manifest.FinishedAt,
	}
}

// updateCounts increments the appropriate counter based on status.
func updateCounts(exec *SynthesizedExecution, status string) {
	switch status {
	case "completed", "success", "succeeded":
		exec.Counts.Completed++
	case "failed":
		exec.Counts.Failed++
	case "cancelled":
		exec.Counts.Cancelled++
	case "skipped":
		exec.Counts.Skipped++
	default:
		exec.Counts.Pending++
	}
}

// computeStatus determines the overall execution status from counts.
func computeStatus(counts JobCounts) string {
	if counts.Total == 0 {
		return "unknown"
	}

	totalSeen := counts.Completed + counts.Failed + counts.Cancelled + counts.Skipped
	if totalSeen < counts.Total {
		return "partial"
	}

	if counts.Failed > 0 {
		return "failed"
	}
	if counts.Cancelled > 0 && counts.Completed == 0 {
		return "cancelled"
	}

	return "completed"
}

// SynthesizedStatus returns a compact display string for the execution.
// Examples: "completed", "failed", "◐ partial", "cancelled"
func SynthesizedStatus(exec *SynthesizedExecution) string {
	if exec == nil {
		return "unknown"
	}
	if exec.Partial {
		return "partial"
	}
	return exec.Status
}

// SynthesizedSummary returns a one-line summary string.
func SynthesizedSummary(exec *SynthesizedExecution) string {
	if exec == nil {
		return "no execution"
	}
	status := SynthesizedStatus(exec)
	seen := exec.Counts.Completed + exec.Counts.Failed + exec.Counts.Cancelled + exec.Counts.Skipped
	return fmt.Sprintf("%s  %d/%d shards", status, seen, exec.Counts.Total)
}