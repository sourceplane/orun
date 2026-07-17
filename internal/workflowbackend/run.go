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
	// Connections are orun-resolved, in-memory credential payloads keyed by the
	// workflow's own connection names (orun-workflows-v2 §3/§4). Populated by
	// the connections grant; nil is fine for a workflow with no connections.
	Connections map[string]any
	// Metadata is run context (job/component/env for a step; blueprint for a hook).
	Metadata map[string]any
	// RunDir is a scratch directory the engine may use for its own run state.
	RunDir string
	// Resume asks the engine to re-execute only steps that did not succeed in
	// the prior run recorded under RunDir (orun-workflows-v2 §8). The caller
	// guards that workflow and engine digests match the failed attempt.
	Resume bool
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
		Contract:    ContractV1,
		Workflow:    spec.WorkflowPath,
		With:        spec.With,
		Connections: spec.Connections,
		Metadata:    spec.Metadata,
		RunDir:      spec.RunDir,
		Resume:      spec.Resume,
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
