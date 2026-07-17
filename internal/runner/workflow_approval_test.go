package runner

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/approval"
	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/workflowbackend"
)

func fastPoll(t *testing.T) {
	t.Helper()
	prev := approvalPollInterval
	approvalPollInterval = 10 * time.Millisecond
	t.Cleanup(func() { approvalPollInterval = prev })
}

func gatedStep(digest string) model.PlanStep {
	return model.PlanStep{
		Name: "promote", Workflow: "wf.yaml", WorkflowDigest: digest,
		Approval: &model.StepApproval{Prompt: "ship?", Timeout: "100ms", OnTimeout: "fail"},
	}
}

func newGatedRunner(dir string) (*Runner, *fakeWFEngine) {
	eng := &fakeWFEngine{res: workflowbackend.Result{Status: workflowbackend.StatusSuccess}}
	r := &Runner{WorkflowEngine: eng, ExecID: "exec-a", Stdout: &strings.Builder{}}
	_ = dir
	return r, eng
}

func TestApprovalTimeoutFailPolicy(t *testing.T) {
	fastPoll(t)
	dir, digest := writeWF(t)
	r, eng := newGatedRunner(dir)
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}

	_, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, gatedStep(digest), false)
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("onTimeout=fail must fail the step: %v", err)
	}
	if eng.gotReq.Workflow != "" {
		t.Fatalf("engine must not run on a failed gate")
	}
}

func TestApprovalTimeoutProceedPolicy(t *testing.T) {
	fastPoll(t)
	dir, digest := writeWF(t)
	r, eng := newGatedRunner(dir)
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	step := gatedStep(digest)
	step.Approval.OnTimeout = "proceed"

	out, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step, false)
	if err != nil {
		t.Fatalf("onTimeout=proceed must run the workflow: %v", err)
	}
	if eng.gotReq.Workflow == "" {
		t.Fatalf("engine should have run after policy-proceed")
	}
	if !strings.Contains(out, "policy:onTimeout=proceed") {
		t.Fatalf("the policy verdict must seal into the step output: %q", out)
	}
}

func TestApprovalApprovedRuns(t *testing.T) {
	fastPoll(t)
	dir, digest := writeWF(t)
	r, eng := newGatedRunner(dir)
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	step := gatedStep(digest)
	step.Approval.Timeout = "5s"

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = approval.Decide(dir, "j", "promote", true, "sam")
	}()
	out, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step, false)
	if err != nil {
		t.Fatalf("approved gate must run: %v", err)
	}
	if !strings.Contains(out, "approved by sam") {
		t.Fatalf("verdict must seal into the output: %q", out)
	}
	if eng.gotReq.Workflow == "" {
		t.Fatalf("engine should have run after approval")
	}
}

func TestApprovalRejectedFailsWithoutRunning(t *testing.T) {
	fastPoll(t)
	dir, digest := writeWF(t)
	r, eng := newGatedRunner(dir)
	ec := executor.ExecContext{Context: context.Background(), WorkspaceDir: dir}
	step := gatedStep(digest)
	step.Approval.Timeout = "5s"

	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = approval.Decide(dir, "j", "promote", false, "sam")
	}()
	_, _, err := r.runWorkflowStep(ec, model.PlanJob{ID: "j"}, step, false)
	if err == nil || !strings.Contains(err.Error(), "rejected by sam") {
		t.Fatalf("rejected gate must fail the step: %v", err)
	}
	if eng.gotReq.Workflow != "" {
		t.Fatalf("engine must not run on rejection")
	}
}

func TestApprovalValidation(t *testing.T) {
	if err := (&model.StepApproval{Timeout: "1h", OnTimeout: "fail"}).Validate("s"); err != nil {
		t.Fatalf("valid gate: %v", err)
	}
	if err := (&model.StepApproval{OnTimeout: "fail"}).Validate("s"); err == nil {
		t.Fatalf("missing timeout must fail validation")
	}
	if err := (&model.StepApproval{Timeout: "1h", OnTimeout: "wait"}).Validate("s"); err == nil {
		t.Fatalf("unknown onTimeout must fail validation")
	}
}
