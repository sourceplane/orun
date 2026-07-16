package data

import (
	"context"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/tui/services"
)

// PlanSpec is a Compose request (specs/orun-tui-v2 §5 — the Compose flow).
type PlanSpec struct {
	Components  []string
	Environment string
	ChangedOnly bool
}

// PlanPreview is a generated plan ready to preview and dispatch.
type PlanPreview struct {
	Plan       *model.Plan
	Checksum   string
	JobCount   int
	Components []string
	Warnings   []string
}

// RunEvent is one streaming event from a dispatched run — job and step
// lifecycle, then a terminal run_done. It mirrors the services event
// vocabulary without importing its type into every surface.
type RunEvent struct {
	Kind      string // job_started, job_completed, job_failed, step_started, step_completed, run_done
	ExecID    string
	JobID     string
	StepID    string
	Component string
	Env       string
	Status    string
	Error     string
}

// Composer generates and dispatches plans. It is a separate seam from
// Source because dispatch is capability-gated (Caps.Execute) and cloud
// sources may never grow it (design §14 keeps remote execution a
// follow-on).
type Composer interface {
	GeneratePlan(ctx context.Context, spec PlanSpec) (PlanPreview, error)
	// RunPlan dispatches; events stream until run_done, then close.
	RunPlan(ctx context.Context, plan *model.Plan, dryRun bool) (<-chan RunEvent, error)
}

// LocalComposerConfig locates the workspace a LocalComposer plans.
type LocalComposerConfig struct {
	IntentFile string
	IntentRoot string
	ConfigDir  string
	OrunRoot   string
	Version    string
}

// LocalComposer wraps the shared live service — the same plan generation
// and runner dispatch the CLI and v1 cockpit use (internal/tui/services;
// the package moves out from under internal/tui at TR9 cutover).
type LocalComposer struct {
	svc services.OrunService
	cfg LocalComposerConfig
}

// NewLocalComposer builds a composer for the workspace.
func NewLocalComposer(cfg LocalComposerConfig) *LocalComposer {
	return &LocalComposer{
		cfg: cfg,
		svc: services.NewLiveOrunService(services.LiveServiceConfig{
			IntentFile:      cfg.IntentFile,
			IntentRoot:      cfg.IntentRoot,
			ConfigDir:       cfg.ConfigDir,
			ObjectModelRoot: cfg.OrunRoot,
			Version:         cfg.Version,
		}),
	}
}

// GeneratePlan implements Composer.
func (c *LocalComposer) GeneratePlan(ctx context.Context, spec PlanSpec) (PlanPreview, error) {
	res, err := c.svc.GeneratePlan(ctx, services.PlanRequest{
		IntentFile:  c.cfg.IntentFile,
		ConfigDir:   c.cfg.ConfigDir,
		Components:  spec.Components,
		Environment: spec.Environment,
		ChangedOnly: spec.ChangedOnly,
	})
	if err != nil {
		return PlanPreview{}, err
	}
	return PlanPreview{
		Plan:       res.Plan,
		Checksum:   res.Checksum,
		JobCount:   res.JobCount,
		Components: res.Components,
		Warnings:   res.Warnings,
	}, nil
}

// RunPlan implements Composer.
func (c *LocalComposer) RunPlan(ctx context.Context, plan *model.Plan, dryRun bool) (<-chan RunEvent, error) {
	src, err := c.svc.RunPlan(ctx, services.RunRequest{Plan: plan, DryRun: dryRun})
	if err != nil {
		return nil, err
	}
	out := make(chan RunEvent, 64)
	go func() {
		defer close(out)
		for ev := range src {
			out <- RunEvent{
				Kind:      string(ev.Kind),
				ExecID:    ev.ExecID,
				JobID:     ev.JobID,
				StepID:    ev.StepID,
				Component: ev.Component,
				Env:       ev.Env,
				Status:    ev.Status,
				Error:     ev.Error,
			}
		}
	}()
	return out, nil
}

// MockComposer scripts compose flows for tests and the demo.
type MockComposer struct {
	Preview PlanPreview
	PlanErr error
	// Events are replayed (then the channel closes) on each RunPlan.
	Events []RunEvent
	RunErr error
	// DispatchedDry records the dryRun flags of dispatches, in order.
	DispatchedDry []bool
}

// GeneratePlan implements Composer.
func (m *MockComposer) GeneratePlan(context.Context, PlanSpec) (PlanPreview, error) {
	return m.Preview, m.PlanErr
}

// RunPlan implements Composer.
func (m *MockComposer) RunPlan(_ context.Context, _ *model.Plan, dryRun bool) (<-chan RunEvent, error) {
	if m.RunErr != nil {
		return nil, m.RunErr
	}
	m.DispatchedDry = append(m.DispatchedDry, dryRun)
	ch := make(chan RunEvent, len(m.Events)+1)
	for _, e := range m.Events {
		ch <- e
	}
	close(ch)
	return ch, nil
}

// SampleComposer returns a mock with a plausible two-job preview.
func SampleComposer() *MockComposer {
	return &MockComposer{
		Preview: PlanPreview{
			Plan:       &model.Plan{Metadata: model.PlanMetadata{Name: "deploy checkout"}},
			Checksum:   "a1b2c3d4",
			JobCount:   2,
			Components: []string{"checkout", "payments"},
		},
		Events: []RunEvent{
			{Kind: "job_started", ExecID: "exec_mock", JobID: "checkout@deploy", Status: "running"},
			{Kind: "step_started", ExecID: "exec_mock", JobID: "checkout@deploy", StepID: "build", Status: "running"},
			{Kind: "step_completed", ExecID: "exec_mock", JobID: "checkout@deploy", StepID: "build", Status: "completed"},
			{Kind: "job_completed", ExecID: "exec_mock", JobID: "checkout@deploy", Status: "completed"},
			{Kind: "run_done", ExecID: "exec_mock", Status: "completed"},
		},
	}
}
