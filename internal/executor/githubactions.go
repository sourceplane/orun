package executor

import (
	"github.com/sourceplane/arx/internal/gha"
	"github.com/sourceplane/arx/internal/model"
)

type githubActionsExecutor struct {
	engine *gha.Engine
}

func (e *githubActionsExecutor) Name() string {
	return "github-actions"
}

func (e *githubActionsExecutor) Prepare(ctx ExecContext) error {
	return e.engine.Prepare(toGHAContext(ctx))
}

func (e *githubActionsExecutor) RunStep(execCtx ExecContext, job model.PlanJob, step model.PlanStep) (string, error) {
	return e.engine.RunStep(toGHAContext(execCtx), job, step)
}

func (e *githubActionsExecutor) FinalizeJob(ctx ExecContext, job model.PlanJob) (string, error) {
	return e.engine.FinalizeJob(toGHAContext(ctx), job)
}

func (e *githubActionsExecutor) Cleanup(ctx ExecContext) error {
	return e.engine.Cleanup(toGHAContext(ctx))
}

func toGHAContext(ctx ExecContext) gha.ExecContext {
	return gha.ExecContext{
		Context:      ctx.Context,
		WorkspaceDir: ctx.WorkspaceDir,
		WorkDir:      ctx.WorkDir,
		BaseEnv:      ctx.BaseEnv,
		JobEnv:       ctx.JobEnv,
		StepEnv:      ctx.StepEnv,
		Env:          ctx.Env,
	}
}
