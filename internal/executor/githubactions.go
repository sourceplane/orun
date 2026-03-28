package executor

import "github.com/sourceplane/liteci/internal/model"

type githubActionsExecutor struct {
	delegate *localExecutor
}

func (e *githubActionsExecutor) Name() string {
	return "github-actions"
}

func (e *githubActionsExecutor) Prepare(ctx ExecContext) error {
	return e.delegate.Prepare(ctx)
}

func (e *githubActionsExecutor) RunStep(execCtx ExecContext, job model.PlanJob, step model.PlanStep) (string, error) {
	delegated := execCtx
	delegated.Env = MergeEnvironment(execCtx.Env, map[string]string{
		"CI":             "true",
		"GITHUB_ACTIONS": "true",
		"LITECI_CONTEXT": "ci",
		"LITECI_RUNNER":  e.Name(),
	})
	return e.delegate.RunStep(delegated, job, step)
}

func (e *githubActionsExecutor) Cleanup(ctx ExecContext) error {
	return e.delegate.Cleanup(ctx)
}
