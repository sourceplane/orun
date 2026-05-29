// Package triggerctx provides the trigger context model and resolver used by
// the orun planner to capture *why* a plan was compiled. Every PlanRevision in
// the orun-state-redesign Phase 1 layout is paired with a TriggerOccurrence
// produced by this package — either a "declared" trigger (an automation
// binding matched a provider event) or a "system" trigger (an ad-hoc invocation
// such as a manual `orun plan`).
//
// This package owns no persistence. See specs/orun-state-redesign/data-model.md
// §2 for the on-disk JSON schema and design.md §5.1 for the position of
// triggerctx in the broader architecture.
package triggerctx

import "time"

// API constants. These are written verbatim into every persisted
// TriggerOccurrence and must remain in lockstep with data-model.md §2.
const (
	APIVersion = "orun.io/v1alpha1"
	KindName   = "TriggerOccurrence"

	TriggerTypeDeclared = "declared"
	TriggerTypeSystem   = "system"

	// System trigger names. See data-model.md §2.2.
	SystemManual        = "system.manual"
	SystemManualChanged = "system.manual-changed"
	SystemReplay        = "system.replay"
	SystemAPI           = "system.api"
	SystemMigrated      = "system.migrated"
	// SystemCIUnmatched is defined for symmetry with data-model.md §2.2 but is
	// not emitted in Phase 1. Callers should surface FromCINoMatchError instead.
	SystemCIUnmatched = "system.ci-unmatched"

	// ProviderOrun is the synthetic provider used for system triggers that
	// originate inside orun rather than from an external CI provider.
	ProviderOrun = "orun"

	// EventManual is the canonical event name for system.* triggers.
	EventManual = "manual"

	// Mode values for TriggerOccurrence.Mode.
	ModeManual    = "manual"
	ModeChanged   = "changed"
	ModeReplay    = "replay"
	ModeAPI       = "api"
	ModeMigration = "migration"
	ModeEventFile = "event-file"

	// WorkingTree values.
	WorkingTreeClean = "clean"
	WorkingTreeDirty = "dirty"

	// Plan scope values.
	PlanScopeFull    = "full"
	PlanScopeChanged = "changed"
)

// TriggerOccurrence captures the trigger that produced a PlanRevision. The
// JSON shape MUST match data-model.md §2.1 — downstream readers (statestore,
// revision resolver, CLI) discriminate purely on these field names.
type TriggerOccurrence struct {
	APIVersion      string        `json:"apiVersion"`
	Kind            string        `json:"kind"`
	TriggerID       string        `json:"triggerId"`
	TriggerKey      string        `json:"triggerKey"`
	TriggerType     string        `json:"triggerType"`
	TriggerName     string        `json:"triggerName"`
	Mode            string        `json:"mode"`
	Provider        string        `json:"provider"`
	Event           string        `json:"event"`
	Action          string        `json:"action,omitempty"`
	MatchedBindings []string      `json:"matchedBindings,omitempty"`
	Source          TriggerSource `json:"source"`
	PlanScope       PlanScope     `json:"planScope"`
	CreatedAt       time.Time     `json:"createdAt"`
}

// TriggerSource describes the VCS state at the moment the trigger fired. The
// JSON shape MUST match data-model.md §2.1.
type TriggerSource struct {
	Repo         string `json:"repo"`
	Ref          string `json:"ref"`
	SourceScope  string `json:"sourceScope"`
	HeadRevision string `json:"headRevision"`
	BaseRevision string `json:"baseRevision,omitempty"`
	WorkingTree  string `json:"workingTree"`
}

// PlanScope records how the planner restricted the compiled plan. The JSON
// shape MUST match data-model.md §2.1.
type PlanScope struct {
	Mode               string   `json:"mode"`
	Base               string   `json:"base,omitempty"`
	Head               string   `json:"head,omitempty"`
	ActivationMode     string   `json:"activationMode,omitempty"`
	ActiveEnvironments []string `json:"activeEnvironments,omitempty"`
	ChangedComponents  []string `json:"changedComponents,omitempty"`
}

// Clone returns a deep copy of the occurrence so callers can safely retain a
// snapshot across mutations. TriggerOccurrence is intended to be copy-safe by
// value — Clone exists to make slice-field independence explicit.
func (t TriggerOccurrence) Clone() TriggerOccurrence {
	out := t
	if len(t.MatchedBindings) > 0 {
		out.MatchedBindings = append([]string(nil), t.MatchedBindings...)
	}
	out.PlanScope = t.PlanScope.Clone()
	return out
}

// Clone returns a deep copy of the plan scope, independent of the receiver's
// slice fields.
func (p PlanScope) Clone() PlanScope {
	out := p
	if len(p.ActiveEnvironments) > 0 {
		out.ActiveEnvironments = append([]string(nil), p.ActiveEnvironments...)
	}
	if len(p.ChangedComponents) > 0 {
		out.ChangedComponents = append([]string(nil), p.ChangedComponents...)
	}
	return out
}
