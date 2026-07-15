package workflowbackend

import "context"

// StepSpec describes one `workflow:` unit to execute — a plan step (Surface A) or
// a blueprint hook (Surface B). It is the runner-agnostic input to RunStep.
type StepSpec struct {
	// WorkflowPath is the resolved, on-disk path to the workflow file.
	WorkflowPath string
	// ExpectedDigest is the digest pinned at compile/scaffold time. When set,
	// RunStep re-hashes the file and refuses to run a workflow that changed since
	// it was pinned (fail-closed integrity — orun-workflows §5/§7).
	ExpectedDigest string
	// With is the declared inputs handed to the engine as its Trigger context.
	With map[string]any
	// Credentials are orun-resolved, in-memory secrets injected for this run
	// (orun-workflows §6). Populated by the secret bridge (WF3); nil is fine.
	Credentials map[string]any
	// Metadata is run context (job/component/env for a step; blueprint for a hook).
	Metadata map[string]any
	// RunDir is a scratch directory the engine may use for its own run state.
	RunDir string
}

// RunStep verifies the pinned digest (if any) and then invokes the engine. It
// returns the engine Result; a workflow that ran and failed is a Result with a
// non-success Status (not an error). An error means the workflow could not be
// run: a digest mismatch (the file changed since it was pinned), or an
// infrastructure failure from the engine.
func RunStep(ctx context.Context, eng Engine, spec StepSpec) (Result, error) {
	if spec.ExpectedDigest != "" {
		got, err := WorkflowDigest(spec.WorkflowPath)
		if err != nil {
			return Result{}, err
		}
		if got != spec.ExpectedDigest {
			return Result{}, &DigestMismatchError{
				Path:   spec.WorkflowPath,
				Pinned: spec.ExpectedDigest,
				OnDisk: got,
			}
		}
	}
	return eng.Invoke(ctx, Request{
		Workflow:    spec.WorkflowPath,
		With:        spec.With,
		Credentials: spec.Credentials,
		Metadata:    spec.Metadata,
		RunDir:      spec.RunDir,
	})
}

// DigestMismatchError is returned when a workflow file's on-disk content no
// longer matches the digest pinned into the plan or provenance lock.
type DigestMismatchError struct {
	Path   string
	Pinned string
	OnDisk string
}

func (e *DigestMismatchError) Error() string {
	return "workflow " + e.Path + " changed since it was pinned: on-disk " + e.OnDisk + " != pinned " + e.Pinned
}
