package runner

import (
	"fmt"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/ui"
)

// GitHub Actions output mode.
//
// Per-job output is buffered in a per-job sink (ui.GHAJobBuffer) so that
// concurrent jobs render as clean, non-interleaved sections in the GHA log
// viewer. The structural pattern is:
//
//	▶ job-name
//	component · env · N steps
//	01 setup-node
//	::group::logs
//	<raw step output>
//	::endgroup::
//	✓ 1.2s · cache hit
//	...
//	✓ component · env
//	job-name  completed in 33.6s · 13/13 steps
//
// One ::group:: per step (not per job) is the modern idiom: GHA cannot nest
// groups, and per-step grouping matches the way the GHA UI itself collapses
// each step independently.

func (r *Runner) ghaJobOut(jobID string) *ui.GHAJobBuffer {
	if r.gha == nil {
		return nil
	}
	return r.gha.JobBuffer(jobID)
}

// ghaJobLabel returns the full display label used in annotations and error messages.
// Prefers CheckName (e.g. "commerce-checkout-chart · dev · Render helm chart") when set.
func (r *Runner) ghaJobLabel(job model.PlanJob) string {
	if cn := strings.TrimSpace(job.CheckName); cn != "" {
		return cn
	}
	parts := []string{}
	if name := strings.TrimSpace(job.DisplayName); name != "" {
		parts = append(parts, name)
	} else if name := strings.TrimSpace(job.Name); name != "" {
		parts = append(parts, name)
	} else if id := strings.TrimSpace(job.ID); id != "" {
		parts = append(parts, id)
	}
	scope := []string{}
	if c := strings.TrimSpace(job.Component); c != "" {
		scope = append(scope, c)
	}
	if e := strings.TrimSpace(job.Environment); e != "" {
		scope = append(scope, e)
	}
	if len(scope) > 0 {
		return fmt.Sprintf("%s [%s]", strings.Join(parts, " "), strings.Join(scope, "·"))
	}
	return strings.Join(parts, " ")
}

// ghaJobName returns the job display name (or ID as fallback).
func (r *Runner) ghaJobName(job model.PlanJob) string {
	if dn := strings.TrimSpace(job.DisplayName); dn != "" {
		return dn
	}
	if name := strings.TrimSpace(job.Name); name != "" {
		return name
	}
	return strings.TrimSpace(job.ID)
}

// ghaJobScope returns "component · env" or "" if neither is set.
func (r *Runner) ghaJobScope(job model.PlanJob) string {
	parts := []string{}
	if c := strings.TrimSpace(job.Component); c != "" {
		parts = append(parts, c)
	}
	if e := strings.TrimSpace(job.Environment); e != "" {
		parts = append(parts, e)
	}
	return strings.Join(parts, " · ")
}

func (r *Runner) ghaPrintJobHeader(job model.PlanJob) {
	buf := r.ghaJobOut(job.ID)
	if buf == nil {
		return
	}
	buf.Println("")
	buf.Println(fmt.Sprintf("▶ %s", r.ghaJobName(job)))

	scope := r.ghaJobScope(job)
	n := len(job.Steps)
	stepStr := fmt.Sprintf("%d step%s", n, pluralSuffix(n))
	if scope != "" {
		buf.Println(fmt.Sprintf("%s · %s", scope, stepStr))
	} else {
		buf.Println(stepStr)
	}
}

func (r *Runner) ghaPrintPhaseHeader(job model.PlanJob, phase string) {
	buf := r.ghaJobOut(job.ID)
	if buf == nil {
		return
	}
	title := phase
	switch phase {
	case "pre":
		title = "Pre-steps"
	case "post":
		title = "Post-steps"
	}
	buf.Println(fmt.Sprintf("── %s ──", title))
}

func (r *Runner) ghaOpenStepGroup(job model.PlanJob, stepID string, index, total int) {
	buf := r.ghaJobOut(job.ID)
	if buf == nil {
		return
	}
	// Print the step heading before the collapsible group so it is always
	// visible in the GHA log viewer even when the group is collapsed.
	buf.Println(fmt.Sprintf("%02d %s", index, stepID))
	buf.OpenGroup("logs")
}

func (r *Runner) ghaCloseStepGroup(jobID string) {
	buf := r.ghaJobOut(jobID)
	if buf == nil {
		return
	}
	buf.CloseGroup()
	r.gha.FlushStep(jobID)
}

// ghaEmitStepOutput dumps the raw step output (already mask-sanitized by the
// executor) into the job buffer. No compaction or path shortening: CI logs
// are archival and users want the full text.
func (r *Runner) ghaEmitStepOutput(jobID, output string) {
	buf := r.ghaJobOut(jobID)
	if buf == nil {
		return
	}
	trimmed := strings.TrimRight(output, "\n")
	if trimmed == "" {
		return
	}
	buf.PrintBlock(strings.Split(trimmed, "\n"))
}

func (r *Runner) ghaPrintStepResult(job model.PlanJob, stepID string, success bool, duration time.Duration, err error, summary string) {
	buf := r.ghaJobOut(job.ID)
	if buf == nil {
		return
	}
	if success {
		line := fmt.Sprintf("✓ %s", formatStepDuration(duration))
		if summary != "" {
			line += " · " + summary
		}
		buf.Println(line)
		return
	}
	msg := summarizeExecError(err)
	buf.Annotation("error", fmt.Sprintf("%s › %s: %s", r.ghaJobLabel(job), stepID, msg), nil)
	buf.Println(fmt.Sprintf("✗ %s  %s", formatStepDuration(duration), msg))
}

func (r *Runner) ghaPrintStepSkipped(job model.PlanJob, stepID string, index, total int) {
	buf := r.ghaJobOut(job.ID)
	if buf == nil {
		return
	}
	buf.Println(fmt.Sprintf("%02d %s  ↷ cached", index, stepID))
}

func (r *Runner) ghaPrintStepRetry(jobID string, attempt, attempts int) {
	buf := r.ghaJobOut(jobID)
	if buf == nil {
		return
	}
	buf.Println(fmt.Sprintf("↻ retry %d/%d", attempt, attempts))
}

func (r *Runner) ghaPrintJobFooter(job model.PlanJob, report *jobReport, success bool, duration time.Duration) {
	buf := r.ghaJobOut(job.ID)
	if buf == nil {
		return
	}

	name := r.ghaJobName(job)
	scope := r.ghaJobScope(job)

	n := 0
	if report != nil {
		n = report.stepCount
	}
	stepNote := fmt.Sprintf("%d/%d step%s", n, n, pluralSuffix(n))

	if success {
		if scope != "" {
			buf.Println(fmt.Sprintf("✓ %s", scope))
			buf.Println(fmt.Sprintf("%s  completed in %s · %s", name, formatStepDuration(duration), stepNote))
		} else {
			buf.Println(fmt.Sprintf("✓ %s  %s · %s", name, formatStepDuration(duration), stepNote))
		}
	} else {
		if scope != "" {
			buf.Println(fmt.Sprintf("✗ %s", scope))
			buf.Println(fmt.Sprintf("%s  failed in %s", name, formatStepDuration(duration)))
		} else {
			buf.Println(fmt.Sprintf("✗ %s  failed  %s", name, formatStepDuration(duration)))
		}
	}

	if report != nil {
		for _, link := range report.links {
			buf.Println(fmt.Sprintf("  ↗ %s  %s", linkLabel(link.Label), link.URL))
		}
	}
	r.gha.FlushJob(job.ID)
}

func (r *Runner) ghaPrintJobResumed(job model.PlanJob) {
	if r.gha == nil {
		return
	}
	r.gha.Print(fmt.Sprintf("⚡ %s  cached", r.ghaJobLabel(job)))
}
