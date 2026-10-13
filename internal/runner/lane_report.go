package runner

import (
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/model"
)

// CheckReporter posts one GitHub check run per job of a multi-job lane
// (specs/orun-lanes §9.2). Implementations are best effort and never fail a
// job.
type CheckReporter interface {
	Start(jobID, name string)
	Finish(jobID, name, conclusion, summary string)
}

const (
	laneSucceeded = "succeeded"
	laneFailed    = "failed"
	laneResumed   = "already done"
	laneBlocked   = "blocked"
	laneNotRun    = "not run"
)

// laneRecord is one job's outcome in a lane run.
type laneRecord struct {
	job      model.PlanJob
	status   string
	duration time.Duration
	waited   []string
	note     string
}

// conclusion is the check-run conclusion for the record. A job that did not
// run because of a failure is cancelled, never skipped: a required check that
// is skipped counts as passing.
func (r *laneRecord) conclusion() string {
	switch r.status {
	case laneSucceeded, laneResumed:
		return "success"
	case laneFailed:
		return "failure"
	default:
		return "cancelled"
	}
}

// laneCheckName is the job's check title, the name branch protection knows.
func laneCheckName(job model.PlanJob) string {
	if n := strings.TrimSpace(job.CheckName); n != "" {
		return n
	}
	return job.ID
}

// writeLaneStepSummary appends the lane's per-job table to
// $GITHUB_STEP_SUMMARY (§9.3). Best effort.
func writeLaneStepSummary(laneID string, records []*laneRecord) {
	path := os.Getenv("GITHUB_STEP_SUMMARY")
	if path == "" {
		return
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(laneSummaryMarkdown(laneID, records))
}

func laneSummaryMarkdown(laneID string, records []*laneRecord) string {
	var b strings.Builder
	fmt.Fprintf(&b, "### Lane `%s`\n\n| Job | Status | Duration | Waited on |\n|---|---|---|---|\n", laneID)
	for _, rec := range records {
		dur := "—"
		if rec.duration > 0 {
			dur = formatStepDuration(rec.duration)
		}
		waited := "—"
		if len(rec.waited) > 0 {
			waited = strings.Join(rec.waited, ", ")
		}
		fmt.Fprintf(&b, "| %s | %s %s | %s | %s |\n", mdCell(laneCheckName(rec.job)), statusIcon(rec.status), rec.status, dur, mdCell(waited))
	}
	b.WriteString("\n")
	return b.String()
}

func statusIcon(status string) string {
	switch status {
	case laneSucceeded, laneResumed:
		return "✅"
	case laneFailed:
		return "❌"
	default:
		return "⊘"
	}
}

func mdCell(s string) string { return strings.ReplaceAll(s, "|", "\\|") }
