package runner

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/workflowbackend"
)

// runWorkflowStep executes a `workflow:` plan step through the workflow backend
// (orun-workflows WF2). It resolves the pinned engine, verifies the workflow's
// digest still matches what the plan pinned, invokes it, and returns a
// human-readable summary of the run as the step output — sealed into .orun/ by
// the caller via the same AfterStepLog path as any other step (§7). A workflow
// that fails yields a non-nil error so the step is marked failed and honors the
// job's onFailure/retry policy (§8).
func (r *Runner) runWorkflowStep(execCtx executor.ExecContext, job model.PlanJob, step model.PlanStep) (string, error) {
	eng, err := r.workflowEngine()
	if err != nil {
		return "", err
	}

	// The connections grant (orun-workflows-v2 §4): resolve exactly the refs the
	// plan granted, keyed by the workflow's own connection names. The job's wider
	// SecretEnv never crosses the boundary (invariant 10). Values stay in-memory
	// and are masked by the runner's single redaction site.
	connections, err := buildConnectionPayloads(job, step, execCtx.SecretEnv)
	if err != nil {
		return "", err
	}

	spec := workflowbackend.StepSpec{
		WorkflowPath:   r.resolveWorkflowPath(execCtx, step.Workflow),
		ExpectedDigest: step.WorkflowDigest,
		With:           step.With,
		Connections:    connections,
		Metadata: map[string]any{
			"jobId":          job.ID,
			"component":      job.Component,
			"environment":    job.Environment,
			"step":           step.Name,
			"workflowRef":    step.Workflow,
			"workflowDigest": step.WorkflowDigest,
		},
	}

	res, err := workflowbackend.RunStep(execCtx.Context, eng, spec)
	if err != nil {
		return "", err
	}
	output := formatWorkflowResult(step, res)
	if !res.Succeeded() {
		msg := res.Error
		if msg == "" {
			msg = "workflow reported status " + res.Status
		}
		return output, fmt.Errorf("workflow step %q failed: %s", step.Name, msg)
	}
	return output, nil
}

// workflowEngine returns the injected engine, or lazily resolves the pinned
// engine from the environment, caching it on the runner. A run whose steps
// declare no workflow: never calls this, so a missing engine is not an error for
// ordinary plans (S-4).
func (r *Runner) workflowEngine() (workflowbackend.Engine, error) {
	if r.WorkflowEngine != nil {
		return r.WorkflowEngine, nil
	}
	eng, err := workflowbackend.ResolveEngine(workflowbackend.EngineOptions{})
	if err != nil {
		return nil, fmt.Errorf("cannot run workflow step: %w", err)
	}
	r.WorkflowEngine = eng
	return eng, nil
}

// resolveWorkflowPath resolves a workflow reference to an on-disk path against
// the workspace root (where the intent lives), falling back to the step working
// directory — the same base the compiler pinned the digest against (§5).
func (r *Runner) resolveWorkflowPath(execCtx executor.ExecContext, ref string) string {
	if filepath.IsAbs(ref) {
		return ref
	}
	base := execCtx.WorkspaceDir
	if base == "" {
		base = execCtx.WorkDir
	}
	if base == "" {
		return ref
	}
	return filepath.Join(base, ref)
}

// buildConnectionPayloads materializes the step's compile-checked grant into
// credential payloads: for each granted connection, each field's secret://
// reference is looked up among the job's SecretRefs and resolved from the job's
// already-resolved SecretEnv. Only granted refs are injected — an unmapped
// secret provably never crosses (orun-workflows-v2 §4, invariant 10). A grant
// referencing a secret the job does not carry is a launch error, fail-closed.
func buildConnectionPayloads(job model.PlanJob, step model.PlanStep, secretEnv map[string]string) (map[string]any, error) {
	if len(step.Connections) == 0 {
		return nil, nil
	}
	refToEnv := make(map[string]string, len(job.SecretRefs))
	for _, sr := range job.SecretRefs {
		refToEnv[sr.Ref] = sr.AsEnv
	}
	out := make(map[string]any, len(step.Connections))
	for conn, fields := range step.Connections {
		payload := make(map[string]any, len(fields))
		for field, ref := range fields {
			asEnv, ok := refToEnv[ref]
			if !ok {
				return nil, fmt.Errorf("step %q connection %q field %q references %s, which is not among the job's secretRefs — declare it on the job so the resolver can lease it", step.Name, conn, field, ref)
			}
			value, ok := secretEnv[asEnv]
			if !ok {
				return nil, fmt.Errorf("step %q connection %q field %q: secret %s (%s) was not resolved for this job", step.Name, conn, field, ref, asEnv)
			}
			payload[field] = value
		}
		out[conn] = payload
	}
	return out, nil
}

// formatWorkflowResult renders a readable summary of a workflow run for the step
// log. Any secret values in the summary are masked by the runner's single
// redaction site before the output reaches any sink.
func formatWorkflowResult(step model.PlanStep, res workflowbackend.Result) string {
	var b strings.Builder
	fmt.Fprintf(&b, "workflow %s: %s\n", step.Workflow, res.Status)
	for _, s := range res.Steps {
		if s.Error != "" {
			fmt.Fprintf(&b, "  - %s: %s (%s)\n", s.Name, s.Status, s.Error)
		} else {
			fmt.Fprintf(&b, "  - %s: %s\n", s.Name, s.Status)
		}
	}
	if len(res.Outputs) > 0 {
		if out, err := json.MarshalIndent(res.Outputs, "", "  "); err == nil {
			fmt.Fprintf(&b, "outputs:\n%s\n", out)
		}
	}
	if res.Error != "" {
		fmt.Fprintf(&b, "error: %s\n", res.Error)
	}
	return strings.TrimRight(b.String(), "\n")
}
