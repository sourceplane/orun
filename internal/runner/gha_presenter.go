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
//	  component   cluster-addons
//	  env         development
//	  job         validate-terraform
//	  job-id      cluster-addons@development.validate-terraform
//	  workdir     infra/cluster-addons
//	  runner      github-actions
//	  steps       5
//
//	────────────────────────────────────────────────────────────
//	steps
//
//	01 setup-terraform
//	::group::logs · 01 setup-terraform · cluster-addons@development.validate-terraform
//	<raw step output — protected by stop-commands>
//	::endgroup::
//	  ✓ setup-terraform · 1.8s · terraform cached
//	...
//	────────────────────────────────────────────────────────────
//	✓ validate-terraform completed · 7.3s
//	  steps       5 passed, 0 failed, 0 skipped
//	  slowest     terraform-init 4.6s
//
// One ::group:: per step (not per job) is the modern idiom: GHA cannot nest
// groups, and per-step grouping matches the way the GHA UI itself collapses
// each step independently.

const ghaSeparator = "────────────────────────────────────────────────────────────"

func (r *Runner) ghaJobOut(jobID string) *ui.GHAJobBuffer {
	if r.gha == nil {
		return nil
	}
	return r.gha.JobBuffer(jobID)
}

// ghaJobLabel returns the full display label used in annotations and error messages.
func (r *Runner) ghaJobLabel(job model.PlanJob) string {
	parts := []string{}
	if name := strings.TrimSpace(job.Name); name != "" {
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

// ghaJobName returns just the job name (or ID as fallback).
func (r *Runner) ghaJobName(job model.PlanJob) string {
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

	// Structured detail block — only emit non-empty fields.
	if c := strings.TrimSpace(job.Component); c != "" {
		buf.Println(fmt.Sprintf("  %-12s%s", "component", c))
	}
	if e := strings.TrimSpace(job.Environment); e != "" {
		buf.Println(fmt.Sprintf("  %-12s%s", "env", e))
	}
	if name := r.ghaJobName(job); name != "" {
		buf.Println(fmt.Sprintf("  %-12s%s", "job", name))
	}
	if id := strings.TrimSpace(job.ID); id != "" {
		buf.Println(fmt.Sprintf("  %-12s%s", "job-id", id))
	}
	if workdir := strings.TrimSpace(job.Path); workdir != "" {
		buf.Println(fmt.Sprintf("  %-12s%s", "workdir", workdir))
	}
	if r.Executor != nil {
		if runnerName := strings.TrimSpace(r.Executor.Name()); runnerName != "" {
			buf.Println(fmt.Sprintf("  %-12s%s", "runner", runnerName))
		}
	}
	buf.Println(fmt.Sprintf("  %-12s%d", "steps", len(job.Steps)))

	buf.Println("")
	buf.Println(ghaSeparator)
	buf.Println("steps")
	buf.Println("")
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
	// Include step number, name, and job-id in the group title so the
	// collapsed "▸ logs" row identifies which step owns the content.
	groupTitle := fmt.Sprintf("logs · %02d %s · %s", index, stepID, job.ID)
	buf.OpenGroup(groupTitle)
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
// executor) into the job buffer, wrapped in stop-commands markers to prevent
// accidental workflow-command injection from child process output.
func (r *Runner) ghaEmitStepOutput(jobID, output string) {
	buf := r.ghaJobOut(jobID)
	if buf == nil {
		return
	}
	trimmed := strings.TrimRight(output, "\n")
	if trimmed == "" {
		return
	}
	// stop-commands prevents the child process output from accidentally
	// triggering GHA workflow commands (::set-output::, ::error::, etc.).
	token := "orun-stop-" + jobID
	buf.StopCommands(token)
	buf.PrintBlock(strings.Split(trimmed, "\n"))
	buf.ResumeCommands(token)
}

// ghaPrintStepResult writes the visible step summary OUTSIDE the collapsed
// group. It must be called after ghaCloseStepGroup so the summary line is
// always visible without expanding the logs.
func (r *Runner) ghaPrintStepResult(job model.PlanJob, stepID string, index int, success bool, duration time.Duration, err error, summary string) {
	buf := r.ghaJobOut(job.ID)
	if buf == nil {
		return
	}
	if success {
		line := fmt.Sprintf("  ✓ %s · %s", stepID, formatStepDuration(duration))
		if summary != "" {
			line += " · " + summary
		}
		buf.Println(line)
		return
	}
	// Failure: print error outside the collapsed group so it is immediately
	// visible without expanding logs.
	msg := summarizeExecError(err)
	buf.Annotation("error", fmt.Sprintf("%s › %s: %s", r.ghaJobLabel(job), stepID, msg), nil)
	buf.Println(fmt.Sprintf("  ✕ %s · %s · %s", stepID, formatStepDuration(duration), msg))
	buf.Println("")
	buf.Println("  error")
	buf.Println(fmt.Sprintf("    %s", msg))
	buf.Println("")
	buf.Println("  next")
	buf.Println(fmt.Sprintf("    Expand logs · %02d %s", index, stepID))
	if jobID := strings.TrimSpace(job.ID); jobID != "" {
		buf.Println(fmt.Sprintf("    Retry: orun run --job-id %s --retry", jobID))
	}
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

	buf.Println("")
	buf.Println(ghaSeparator)
	if success {
		buf.Println(fmt.Sprintf("✓ %s completed · %s", name, formatStepDuration(duration)))
	} else {
		buf.Println(fmt.Sprintf("✕ %s failed · %s", name, formatStepDuration(duration)))
	}

	if report != nil {
		if c := strings.TrimSpace(job.Component); c != "" {
			buf.Println(fmt.Sprintf("  %-12s%s", "component", c))
		}
		if e := strings.TrimSpace(job.Environment); e != "" {
			buf.Println(fmt.Sprintf("  %-12s%s", "env", e))
		}
		buf.Println(fmt.Sprintf("  %-12s%d passed, %d failed, %d skipped",
			"steps", report.stepPassed, report.stepFailed, report.stepSkipped))
		if report.slowestStep != "" {
			buf.Println(fmt.Sprintf("  %-12s%s %s", "slowest", report.slowestStep, formatStepDuration(report.slowestDur)))
		}
		if !success && report.failedStep != "" {
			buf.Println(fmt.Sprintf("  %-12s%s", "failed step", report.failedStep))
		}
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

// ghaPrintWaiting emits waiting/dependency info directly to the GHA log
// (not buffered in a job buffer, since the job has not started yet).
func (r *Runner) ghaPrintWaiting(job model.PlanJob, unmet []string) {
	if r.gha == nil {
		return
	}
	r.gha.Print(fmt.Sprintf("○ %s  waiting on %s", r.ghaJobLabel(job), strings.Join(unmet, ", ")))
}
