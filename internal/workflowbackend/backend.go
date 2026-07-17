package workflowbackend

// ContractV1 is the wire contract version this package speaks. The schema and
// golden fixtures live in contract/v1/, vendored byte-identically in the
// torkflow repo; both CIs run conformance against the same files (invariant 9).
// A wire change is a new contract directory, never an edit to v1.
const ContractV1 = "v1"

// Status values reported by the engine for a completed workflow run.
const (
	StatusSuccess = "success"
	StatusFailed  = "failed"
	// StatusPaused is reserved for approval gates (WX7): the engine (or orun,
	// pre-invocation) reports a run paused awaiting a human decision.
	StatusPaused = "paused"
)

// Request is the contract/v1 document orun writes to the pinned engine's stdin
// (design §3). Connections carries resolved credential payloads keyed by the
// workflow's OWN connection names — the engine fans each payload to steps
// referencing `connection: <name>`, bypassing its file registry. Values are
// streamed to the child process and never persisted on either side (§4).
type Request struct {
	Contract    string         `json:"contract"`
	Workflow    string         `json:"workflow"`
	With        map[string]any `json:"with,omitempty"`
	Connections map[string]any `json:"connections,omitempty"`
	Metadata    map[string]any `json:"metadata,omitempty"`
	RunDir      string         `json:"runDir,omitempty"`
	// ActionStores optionally names provider-module directories for the engine.
	// Omitted: the engine applies its own default resolution.
	ActionStores []string `json:"actionStores,omitempty"`
	// Resume is reserved for WX6: re-execute only non-succeeded steps from the
	// prior run recorded under RunDir.
	Resume bool `json:"resume,omitempty"`
}

// Result is the contract/v1 document orun reads from the engine's stdout
// (design §3, §5). Outputs carries the workflow's declared spec.outputs only —
// the raw context never crosses the boundary, so sealing is an allowlist by
// construction (invariant 12).
type Result struct {
	Contract string         `json:"contract"`
	Status   string         `json:"status"`
	Outputs  map[string]any `json:"outputs,omitempty"`
	Steps    []StepResult   `json:"steps,omitempty"`
	Error    string         `json:"error,omitempty"`
}

// StepResult is one step's outcome in the workflow's structured timeline,
// sealed by the caller as data (design §5).
type StepResult struct {
	Name       string `json:"name"`
	Status     string `json:"status"`
	Error      string `json:"error,omitempty"`
	DurationMs int64  `json:"durationMs,omitempty"`
}

// Succeeded reports whether the workflow completed successfully. A Result with a
// non-success Status is a workflow-level failure the caller acts on — distinct
// from an infrastructure error returned by Engine.Invoke.
func (r Result) Succeeded() bool { return r.Status == StatusSuccess }
