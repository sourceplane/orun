package runner

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/approval"
	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
)

// PolicyApprovalStepID is the step id a requireApproval gate is recorded
// under, so `orun approve <jobID> policy.requireApproval` resolves it.
const PolicyApprovalStepID = "policy.requireApproval"

// policyApprovalTimeout bounds how long a requireApproval gate waits for a
// decision before failing the job. Overridable in tests.
var policyApprovalTimeout = 24 * time.Hour

// checkCleanTreePolicy enforces requireCleanGitTree: when any job about to run
// carries the policy, the workspace must be a git work tree with no
// uncommitted or untracked changes (orun's own .orun/ state is ignored). It
// runs once, before any job starts, so concurrent jobs cannot race it.
func (r *Runner) checkCleanTreePolicy(jobs []model.PlanJob, workspaceDir string) error {
	var gated []string
	for _, job := range jobs {
		if r.JobID != "" && job.ID != r.JobID {
			continue
		}
		if job.Policies != nil && job.Policies.RequireCleanGitTree {
			gated = append(gated, job.ID)
		}
	}
	if len(gated) == 0 {
		return nil
	}
	status := r.GitTreeStatus
	if status == nil {
		status = gitTreeStatus
	}
	dirty, err := status(workspaceDir)
	if err != nil {
		return fmt.Errorf("policy requireCleanGitTree (jobs: %s): cannot verify the git tree at %s: %w", strings.Join(gated, ", "), workspaceDir, err)
	}
	if len(dirty) > 0 {
		shown := dirty
		if len(shown) > 10 {
			shown = append(append([]string(nil), shown[:10]...), fmt.Sprintf("… and %d more", len(dirty)-10))
		}
		return fmt.Errorf("policy requireCleanGitTree (jobs: %s): the working tree has uncommitted or untracked changes:\n  %s\ncommit or stash them, then run again", strings.Join(gated, ", "), strings.Join(shown, "\n  "))
	}
	return nil
}

// gitTreeStatus lists the paths `git status --porcelain` reports for dir,
// excluding orun's own .orun/ state directory. nil means clean.
func gitTreeStatus(dir string) ([]string, error) {
	top, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return nil, fmt.Errorf("not a git work tree")
	}
	root := strings.TrimSpace(string(top))
	out, err := exec.Command("git", "-C", dir, "status", "--porcelain", "--untracked-files=normal").Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	var dirty []string
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 4 {
			continue
		}
		p := strings.Trim(line[3:], `"`)
		if i := strings.Index(p, " -> "); i >= 0 {
			p = p[i+4:]
		}
		if isOrunState(filepath.Join(root, filepath.FromSlash(p))) {
			continue
		}
		dirty = append(dirty, strings.TrimSpace(line))
	}
	sort.Strings(dirty)
	return dirty, nil
}

func isOrunState(abs string) bool {
	for _, part := range strings.Split(filepath.ToSlash(abs), "/") {
		if part == ".orun" {
			return true
		}
	}
	return false
}

// awaitPolicyApproval enforces requireApproval: the job pauses before any
// step runs until `orun approve <jobID> policy.requireApproval` decides. The
// request and verdict are sealed under .orun/approvals like a workflow-step
// gate; no decision within the timeout fails the job.
func (r *Runner) awaitPolicyApproval(execCtx executor.ExecContext, job model.PlanJob) error {
	workspace := execCtx.WorkspaceDir
	if workspace == "" {
		workspace = execCtx.WorkDir
	}
	sources := strings.Join(job.Policies.Sources["requireApproval"], ", ")
	prompt := fmt.Sprintf("Approve job %s? (policy requireApproval from %s)", job.ID, sources)
	gateDir, err := approval.Ask(workspace, approval.Request{
		Prompt: prompt, ExecID: r.ExecID, JobID: job.ID, StepID: PolicyApprovalStepID, RequestedAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("policy requireApproval: record approval request: %w", err)
	}
	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "  │ ⏸ awaiting approval: %s\n  │   decide with: orun approve %q %q [--reject]\n", prompt, job.ID, PolicyApprovalStepID)
	})

	dec, err := approval.Await(execCtx.Context, gateDir, policyApprovalTimeout, approvalPollInterval)
	switch {
	case err == approval.ErrTimeout:
		_ = approval.Seal(gateDir, approval.Decision{Approved: false, By: "policy:requireApproval timeout", DecidedAt: time.Now().UTC(), OnTimeout: true})
		return fmt.Errorf("policy requireApproval: no decision within %s", policyApprovalTimeout)
	case err != nil:
		return fmt.Errorf("policy requireApproval: awaiting approval: %w", err)
	}
	by := strings.TrimSpace(dec.By)
	if by == "" {
		by = "unspecified"
	}
	if !dec.Approved {
		return fmt.Errorf("policy requireApproval: rejected by %s", by)
	}
	r.withPrintLock(func() {
		fmt.Fprintf(r.Stdout, "  │ ✓ approved by %s at %s\n", by, dec.DecidedAt.Format(time.RFC3339))
	})
	return nil
}
