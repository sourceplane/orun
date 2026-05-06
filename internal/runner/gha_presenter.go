package runner

import (
	"fmt"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/state"
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
//	dependencies
//
//	  ✓  network@production.apply  completed
//	  ✓  iam@production.apply      completed
//
//	✓ 2/2 ready
//
//	────────────────────────────────────────────────────────────
//	steps
//
//	::group::✓  01 setup-terraform  1.8s  ·  terraform cached
//	<raw step output>
//	::endgroup::
//	...
//	────────────────────────────────────────────────────────────
//	✓ validate-terraform completed · 7.3s
//	  steps       5 passed, 0 failed, 0 skipped
//	  slowest     terraform-init 4.6s
//
// One ::group:: per step (not per job): group is opened AFTER the step
// completes so the title can include the final result, making the collapsed
// row fully informative without expanding the logs.

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

func (r *Runner) ghaPrintJobHeader(job model.PlanJob, execState *state.ExecState) {
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

	// Dependencies section — shown when the job declares explicit ordering.
	if len(job.DependsOn) > 0 {
		buf.Println("")
		buf.Println(ghaSeparator)
		buf.Println("dependencies")
		buf.Println("")

		type depEntry struct{ id, status string }
		deps := make([]depEntry, 0, len(job.DependsOn))
		completedCount := 0
		failedDep := ""

		r.stateMu.Lock()
		for _, dep := range job.DependsOn {
			status := "pending"
			if execState != nil {
				if js, ok := execState.Jobs[dep]; ok && js != nil && js.Status != "" {
					status = js.Status
				}
			}
			// In GHA matrix mode the remote coordinator (GHA needs) guarantees
			// deps completed before this job started. If remote state hasn't
			// propagated yet, trust the GHA guarantee and show completed.
			if r.SkipLocalDepsForJob && (status == "pending" || status == "") {
				status = "completed"
			}
			deps = append(deps, depEntry{dep, status})
			switch status {
			case "completed":
				completedCount++
			case "failed":
				if failedDep == "" {
					failedDep = dep
				}
			}
		}
		r.stateMu.Unlock()

		maxLen := 0
		for _, d := range deps {
			if len(d.id) > maxLen {
				maxLen = len(d.id)
			}
		}
		for _, d := range deps {
			icon := "○"
			switch d.status {
			case "completed":
				icon = "✓"
			case "running":
				icon = "●"
			case "failed":
				icon = "✕"
			}
			buf.Println(fmt.Sprintf("  %s  %-*s  %s", icon, maxLen, d.id, d.status))
		}

		buf.Println("")
		total := len(job.DependsOn)
		switch {
		case failedDep != "":
			buf.Println(fmt.Sprintf("✕ dependency failed · %s", failedDep))
		case completedCount == total:
			buf.Println(fmt.Sprintf("✓ %d/%d ready", completedCount, total))
		default:
			buf.Println(fmt.Sprintf("● waiting · %d/%d ready", completedCount, total))
		}
		buf.Println("")
	}

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

// ghaEmitStep emits a single collapsible group for a completed step.
// It is called AFTER the step finishes so the group title can include the
// final result — making the collapsed row fully informative without expanding.
//
// Title format: "✓/✕  01 step-name  1.3s  ·  summary/error"
// Body: raw step output (stop-commands protected so child GHA commands are
// not processed by the parent runner when orun is nested inside GHA).
// For failures: a GHA ::error:: annotation is emitted AFTER ::endgroup::
// so it surfaces in the PR check summary even when the group is collapsed.
func (r *Runner) ghaEmitStep(job model.PlanJob, stepID string, index int, output string, success bool, duration time.Duration, err error, summary string) {
	buf := r.ghaJobOut(job.ID)
	if buf == nil {
		return
	}
	icon := "✓"
	if !success {
		icon = "✕"
	}
	title := fmt.Sprintf("%s  %02d %s  %s", icon, index, stepID, formatStepDuration(duration))
	if success && summary != "" {
		title += "  ·  " + summary
	} else if !success {
		title += "  ·  " + summarizeExecError(err)
	}

	buf.OpenGroup(title)
	trimmed := strings.TrimRight(output, "\n")
	if trimmed != "" {
		token := "orun-stop-" + job.ID
		buf.StopCommands(token)
		buf.PrintBlock(strings.Split(trimmed, "\n"))
		buf.ResumeCommands(token)
	}
	if !success {
		if jobID := strings.TrimSpace(job.ID); jobID != "" {
			buf.Println("")
			buf.Println("  retry:  orun run --job " + jobID + " --retry")
		}
	}
	buf.CloseGroup()

	if !success {
		msg := summarizeExecError(err)
		buf.Annotation("error", fmt.Sprintf("%s › %s: %s", r.ghaJobLabel(job), stepID, msg), nil)
	}
	r.gha.FlushStep(job.ID)
}

// ghaPrintStepDryRun emits a single visible line for a dry-run step
// (no collapsible group since there is no output to hide).
func (r *Runner) ghaPrintStepDryRun(job model.PlanJob, stepID string, index int) {
	buf := r.ghaJobOut(job.ID)
	if buf == nil {
		return
	}
	buf.Println(fmt.Sprintf("  ↷  %02d %s  dry-run", index, stepID))
	r.gha.FlushStep(job.ID)
}

func (r *Runner) ghaPrintStepSkipped(job model.PlanJob, stepID string, index, total int) {
	buf := r.ghaJobOut(job.ID)
	if buf == nil {
		return
	}
	buf.Println(fmt.Sprintf("  ↷  %02d %s  cached", index, stepID))
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
