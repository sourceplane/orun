package runner

import (
	"fmt"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/approval"
	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
)

// approvalPollInterval is how often a paused gate checks for a decision.
// Overridable in tests.
var approvalPollInterval = 2 * time.Second

// awaitStepApproval pauses a gated workflow step until a human decides or the
// declared timeout policy does (orun-workflows-v2 §9). Returns a summary line
// (sealed as part of the step output) on approval/proceed; an error rejects the
// step. The pending request and the verdict are also sealed as files under
// .orun/approvals — run facts, never plan content (S-9).
func (r *Runner) awaitStepApproval(execCtx executor.ExecContext, job model.PlanJob, step model.PlanStep) (string, error) {
	gate := step.Approval
	if gate == nil {
		return "", nil
	}
	workspace := execCtx.WorkspaceDir
	if workspace == "" {
		workspace = execCtx.WorkDir
	}
	timeout, err := time.ParseDuration(gate.Timeout)
	if err != nil {
		return "", fmt.Errorf("step %q: approval timeout %q: %w", step.Name, gate.Timeout, err)
	}

	prompt := gate.Prompt
	if prompt == "" {
		prompt = fmt.Sprintf("Approve workflow step %s of %s?", step.Name, job.ID)
	}
	gateDir, err := approval.Ask(workspace, approval.Request{
		Prompt: prompt, ExecID: r.ExecID, JobID: job.ID, StepID: stepIdentifier(step), RequestedAt: time.Now().UTC(),
	})
	if err != nil {
		return "", fmt.Errorf("step %q: record approval request: %w", step.Name, err)
	}
	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "  │ ⏸ awaiting approval: %s\n  │   decide with: orun approve %q %q [--reject]\n", prompt, job.ID, stepIdentifier(step))
	})

	dec, err := approval.Await(execCtx.Context, gateDir, timeout, approvalPollInterval)
	switch {
	case err == approval.ErrTimeout:
		// The declared policy decides; the policy verdict seals like any other.
		dec = approval.Decision{Approved: gate.OnTimeout == "proceed", By: "policy:onTimeout=" + gate.OnTimeout, DecidedAt: time.Now().UTC(), OnTimeout: true}
		_ = approval.Seal(gateDir, dec)
		if !dec.Approved {
			return "", fmt.Errorf("step %q: approval timed out after %s (onTimeout: fail)", step.Name, gate.Timeout)
		}
	case err != nil:
		return "", fmt.Errorf("step %q: awaiting approval: %w", step.Name, err)
	}
	if !dec.Approved {
		by := dec.By
		if by == "" {
			by = "unspecified"
		}
		return "", fmt.Errorf("step %q: rejected by %s", step.Name, by)
	}
	by := dec.By
	if by == "" {
		by = "unspecified"
	}
	return fmt.Sprintf("approval: approved by %s at %s\n", strings.TrimSpace(by), dec.DecidedAt.Format(time.RFC3339)), nil
}
