package workflowbackend

// Status values reported by the engine for a completed workflow run.
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
)

// Request is the JSON contract orun writes to the pinned engine's stdin
// (design §5). It carries the workflow reference, the declared inputs (handed to
// the engine as its Trigger context), the orun-resolved credentials (in-memory
// only, never written to disk — design §6), run metadata (job/component/env for a
// plan step; blueprint/inputs-hash for a hook), and a scratch run directory.
//
// Credentials are intentionally omitted from any serialization orun persists;
// this struct is streamed to the child process and never written to state.
type Request struct {
	Workflow    string         `json:"workflow"`
	With        map[string]any `json:"with,omitempty"`
	Credentials map[string]any `json:"credentials,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	RunDir      string         `json:"runDir,omitempty"`
}

// Result is the JSON contract orun reads from the engine's stdout (design §5).
// The final Context and the Steps timeline are what the caller seals into .orun/
// (design §7); this type is the wire shape of a run, not the durable record.
type Result struct {
	Status  string         `json:"status"`
	Context map[string]any `json:"context,omitempty"`
	Steps   []StepResult   `json:"steps,omitempty"`
	Error   string         `json:"error,omitempty"`
}

// StepResult is one step's outcome in the workflow's timeline.
type StepResult struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}

// Succeeded reports whether the workflow completed successfully. A Result with a
// non-success Status is a workflow-level failure the caller acts on (a failed
// step, a failed hook) — distinct from an infrastructure error returned by
// Engine.Invoke (the engine could not run or produced invalid output).
func (r Result) Succeeded() bool { return r.Status == StatusSuccess }
