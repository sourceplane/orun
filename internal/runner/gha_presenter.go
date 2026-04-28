package runner

import (
	"fmt"
	"strings"
	"time"

	"github.com/sourceplane/gluon/internal/model"
	"github.com/sourceplane/gluon/internal/ui"
)

// GitHub Actions output mode.
//
// All per-job output is buffered in a per-job sink (ui.GHAJobBuffer) so that
// concurrent jobs render as clean, non-interleaved sections in the GHA log
// viewer. The structural pattern is:
//
//   ▶ <job display label>
//     <component>/<environment>
//   ::group::<job> › <step> (1/N)
//   <raw step output, no compaction>
//   ✓ <step>  duration
//   ::endgroup::
//   ...
//   ✓ <job>  duration  (M steps)
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

func (r *Runner) ghaPrintJobHeader(job model.PlanJob) {
	buf := r.ghaJobOut(job.ID)
	if buf == nil {
		return
	}
	buf.Println("")
	buf.Println(fmt.Sprintf("▶ %s", r.ghaJobLabel(job)))
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
	short := r.ghaJobLabel(job)
	if name := strings.TrimSpace(job.Name); name != "" {
		short = name
	} else if id := strings.TrimSpace(job.ID); id != "" {
		short = id
	}
	buf.OpenGroup(fmt.Sprintf("%s › %s  (%d/%d)", short, stepID, index, total))
}

func (r *Runner) ghaCloseStepGroup(jobID string) {
	buf := r.ghaJobOut(jobID)
	if buf == nil {
		return
	}
	buf.CloseGroup()
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

func (r *Runner) ghaPrintStepResult(job model.PlanJob, stepID string, success bool, duration time.Duration, err error) {
	buf := r.ghaJobOut(job.ID)
	if buf == nil {
		return
	}
	if success {
		buf.Println(fmt.Sprintf("✓ %s  %s", stepID, formatStepDuration(duration)))
		return
	}
	msg := summarizeExecError(err)
	buf.Annotation("error", fmt.Sprintf("%s › %s: %s", r.ghaJobLabel(job), stepID, msg), nil)
	buf.Println(fmt.Sprintf("✗ %s  %s  %s", stepID, formatStepDuration(duration), msg))
}

func (r *Runner) ghaPrintStepSkipped(job model.PlanJob, stepID string, index, total int) {
	buf := r.ghaJobOut(job.ID)
	if buf == nil {
		return
	}
	buf.Println(fmt.Sprintf("↷ %s  (%d/%d) cached", stepID, index, total))
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
	stepNote := ""
	if report != nil && report.stepCount > 0 {
		stepNote = fmt.Sprintf("  %d step%s", report.stepCount, pluralSuffix(report.stepCount))
	}
	if success {
		buf.Println(fmt.Sprintf("✓ %s  %s%s", r.ghaJobLabel(job), formatStepDuration(duration), stepNote))
	} else {
		buf.Println(fmt.Sprintf("✗ %s  %s%s  failed", r.ghaJobLabel(job), formatStepDuration(duration), stepNote))
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
