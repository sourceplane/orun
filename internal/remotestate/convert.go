package remotestate

import (
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
)

// BackendPlan is the plan format expected by the orun-backend CreateRun API.
type BackendPlan struct {
	Checksum  string           `json:"checksum"`
	Version   string           `json:"version"`
	Jobs      []BackendPlanJob `json:"jobs"`
	CreatedAt string           `json:"createdAt"`
}

// BackendPlanJob is a job entry in BackendPlan.
type BackendPlanJob struct {
	JobID     string            `json:"jobId"`
	Component string            `json:"component"`
	Deps      []string          `json:"deps"`
	Steps     []BackendPlanStep `json:"steps"`
}

// BackendPlanStep is a step entry in BackendPlanJob.
type BackendPlanStep struct {
	StepID string                 `json:"stepId"`
	Uses   string                 `json:"uses"`
	With   map[string]interface{} `json:"with"`
}

// ConvertPlan converts a CLI model.Plan to the BackendPlan format without
// mutating the original plan.
func ConvertPlan(plan *model.Plan) *BackendPlan {
	planID := execmodel.PlanChecksumShort(plan)
	bp := &BackendPlan{
		Checksum:  planID,
		Version:   "sourceplane.io/v1",
		CreatedAt: time.Now().UTC().Format(time.RFC3339),
		Jobs:      make([]BackendPlanJob, 0, len(plan.Jobs)),
	}
	for _, job := range plan.Jobs {
		deps := job.DependsOn
		if deps == nil {
			deps = []string{}
		}
		bj := BackendPlanJob{
			JobID:     job.ID,
			Component: job.Component,
			Deps:      deps,
			Steps:     make([]BackendPlanStep, 0, len(job.Steps)),
		}
		for _, step := range job.Steps {
			sid := backendStepID(step)
			bs := BackendPlanStep{
				StepID: sid,
				With:   map[string]interface{}{},
			}
			if strings.TrimSpace(step.Use) != "" {
				bs.Uses = step.Use
			} else {
				bs.Uses = "run"
				if strings.TrimSpace(step.Run) != "" {
					bs.With["raw"] = step.Run
				}
			}
			if strings.TrimSpace(step.Name) != "" {
				bs.With["name"] = step.Name
			}
			bj.Steps = append(bj.Steps, bs)
		}
		bp.Jobs = append(bp.Jobs, bj)
	}
	return bp
}

// backendStepID derives a stable step identifier from a plan step, mirroring
// the logic in runner.stepIdentifier so IDs stay consistent.
func backendStepID(step model.PlanStep) string {
	if s := strings.TrimSpace(step.ID); s != "" {
		return s
	}
	if s := strings.TrimSpace(step.Name); s != "" {
		return s
	}
	if s := strings.TrimSpace(step.Use); s != "" {
		return s
	}
	return "unnamed-step"
}

// BackendJobStatusToLocal converts a backend job status string to the local
// status string used by execmodel.JobState.
func BackendJobStatusToLocal(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "success":
		return "completed"
	case "skipped":
		return "completed"
	default:
		return strings.ToLower(strings.TrimSpace(s))
	}
}

// LocalJobStatusToBackend converts a local job status to a backend status value.
func LocalJobStatusToBackend(s string) string {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "completed":
		return "success"
	default:
		return strings.ToLower(strings.TrimSpace(s))
	}
}
