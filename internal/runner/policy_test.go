package runner

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/approval"
	"github.com/sourceplane/orun/internal/executor"
	"github.com/sourceplane/orun/internal/model"
)

func policyPlan(p *model.PlanPolicies) *model.Plan {
	return &model.Plan{Execution: model.PlanExecution{FailFast: true}, Jobs: []model.PlanJob{{
		ID: "net.production.validate", Name: "validate", Component: "net", Environment: "production",
		Policies: p,
		Steps:    []model.PlanStep{{ID: "plan", Name: "plan"}},
	}}}
}

func newPolicyTestRunner(t *testing.T, exec executor.Executor) (*Runner, string) {
	t.Helper()
	dir := t.TempDir()
	r := NewRunner(dir, true, io.Discard, io.Discard, false, "", false, false,
		exec, executor.RuntimeContext{}, "exec_policy", 1, nil, "")
	r.WorkDir = dir
	r.UseWorkDirOverride = true
	return r, dir
}

func stepsRun(e *envCapturingExecutor) int {
	e.mu.Lock()
	defer e.mu.Unlock()
	return len(e.envs)
}

func TestRequireCleanGitTreeRefusesDirtyTree(t *testing.T) {
	exec := &envCapturingExecutor{}
	r, _ := newPolicyTestRunner(t, exec)
	r.GitTreeStatus = func(string) ([]string, error) { return []string{"M main.tf", "?? scratch.txt"}, nil }

	err := r.Run(policyPlan(&model.PlanPolicies{RequireCleanGitTree: true}))
	if err == nil || !strings.Contains(err.Error(), "policy requireCleanGitTree") || !strings.Contains(err.Error(), "scratch.txt") {
		t.Fatalf("a dirty tree must fail the run: %v", err)
	}
	if stepsRun(exec) != 0 {
		t.Fatalf("no step may run on a dirty tree")
	}
}

func TestRequireCleanGitTreeAllowsCleanTree(t *testing.T) {
	exec := &envCapturingExecutor{}
	r, _ := newPolicyTestRunner(t, exec)
	r.GitTreeStatus = func(string) ([]string, error) { return nil, nil }

	if err := r.Run(policyPlan(&model.PlanPolicies{RequireCleanGitTree: true})); err != nil {
		t.Fatalf("a clean tree must run: %v", err)
	}
	if stepsRun(exec) != 1 {
		t.Fatalf("the job's step must run on a clean tree")
	}
}

func TestRequireCleanGitTreeNotCheckedWithoutPolicy(t *testing.T) {
	r, _ := newPolicyTestRunner(t, &envCapturingExecutor{})
	r.GitTreeStatus = func(string) ([]string, error) { t.Fatal("git must not be consulted"); return nil, nil }
	if err := r.Run(policyPlan(nil)); err != nil {
		t.Fatalf("run: %v", err)
	}
}

func TestGitTreeStatusIgnoresOrunState(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "init")
	if err := os.MkdirAll(filepath.Join(dir, ".orun", "plans"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".orun", "plans", "p.json"), []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dirty, err := gitTreeStatus(dir); err != nil || len(dirty) != 0 {
		t.Fatalf(".orun state must not count as dirty: %v %v", dirty, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.tf"), []byte("y"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dirty, err := gitTreeStatus(dir); err != nil || len(dirty) != 1 {
		t.Fatalf("a modified file must count as dirty: %v %v", dirty, err)
	}
	if _, err := gitTreeStatus(t.TempDir()); err == nil {
		t.Fatalf("a directory outside git must not verify as clean")
	}
}

func TestRequireApprovalApprovedRunsJob(t *testing.T) {
	fastPoll(t)
	exec := &envCapturingExecutor{}
	r, dir := newPolicyTestRunner(t, exec)
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = approval.Decide(dir, "net.production.validate", PolicyApprovalStepID, true, "sam")
	}()

	if err := r.Run(policyPlan(&model.PlanPolicies{RequireApproval: true})); err != nil {
		t.Fatalf("run: %v", err)
	}
	if stepsRun(exec) != 1 {
		t.Fatalf("an approved job must run its steps")
	}
}

func TestRequireApprovalRejectedFailsJob(t *testing.T) {
	fastPoll(t)
	exec := &envCapturingExecutor{}
	r, dir := newPolicyTestRunner(t, exec)
	go func() {
		time.Sleep(50 * time.Millisecond)
		_ = approval.Decide(dir, "net.production.validate", PolicyApprovalStepID, false, "sam")
	}()

	err := r.Run(policyPlan(&model.PlanPolicies{RequireApproval: true}))
	if err == nil || !strings.Contains(err.Error(), "fail-fast") {
		t.Fatalf("a rejected job must fail the run: %v", err)
	}
	if stepsRun(exec) != 0 {
		t.Fatalf("a rejected job must not run any step")
	}
}

func TestRequireApprovalTimeoutFailsJob(t *testing.T) {
	fastPoll(t)
	prev := policyApprovalTimeout
	policyApprovalTimeout = 50 * time.Millisecond
	t.Cleanup(func() { policyApprovalTimeout = prev })
	exec := &envCapturingExecutor{}
	r, _ := newPolicyTestRunner(t, exec)

	if err := r.Run(policyPlan(&model.PlanPolicies{RequireApproval: true})); err == nil {
		t.Fatalf("no decision must fail the run")
	}
	if stepsRun(exec) != 0 {
		t.Fatalf("an undecided job must not run any step")
	}
}

func TestRuntimePoliciesSkippedOnDryRun(t *testing.T) {
	r, _ := newPolicyTestRunner(t, &envCapturingExecutor{})
	r.DryRun = true
	r.GitTreeStatus = func(string) ([]string, error) { return []string{"M x"}, nil }
	if err := r.Run(policyPlan(&model.PlanPolicies{RequireApproval: true, RequireCleanGitTree: true})); err != nil {
		t.Fatalf("a dry run neither gates nor checks the tree: %v", err)
	}
}
