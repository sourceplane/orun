package triggerctx

import (
	"errors"
	"fmt"
	"time"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/trigger"
)

// ErrNoMatchingBinding is returned by ResolveProviderEvent when a normalized
// provider event does not match any automation.triggerBindings. The CLI
// surfaces this as a deterministic exit code for `--from-ci` — see design.md
// §11 (risk: "CI event unmatched") and cli-surface.md §1.
//
// Callers MUST detect this with errors.Is rather than string sniffing.
var ErrNoMatchingBinding = errors.New("triggerctx: no trigger binding matched the provider event")

// NoMatchingBindingError carries the unmatched provider/event context so the
// CLI can render an actionable diagnostic without re-deriving the fields. It
// wraps ErrNoMatchingBinding so errors.Is(err, ErrNoMatchingBinding) succeeds.
type NoMatchingBindingError struct {
	Provider string
	Event    string
	Action   string
}

func (e *NoMatchingBindingError) Error() string {
	if e.Action == "" {
		return fmt.Sprintf("triggerctx: no trigger binding matched provider=%q event=%q", e.Provider, e.Event)
	}
	return fmt.Sprintf("triggerctx: no trigger binding matched provider=%q event=%q action=%q", e.Provider, e.Event, e.Action)
}

// Unwrap exposes the sentinel so errors.Is succeeds.
func (e *NoMatchingBindingError) Unwrap() error { return ErrNoMatchingBinding }

// DeclaredOptions parameterizes FromDeclaredTrigger.
type DeclaredOptions struct {
	// TriggerName is the binding key from intent.automation.triggerBindings.
	TriggerName string

	// Source describes the workspace VCS state at the moment the trigger fired.
	// Always populated by the caller (CLI extracts via internal/git probe).
	Source TriggerSource

	// Provider / Event / Action describe the originating provider event when
	// the trigger arrived via --from-ci. For --trigger <name> invocations,
	// Provider falls back to the binding's match.provider and Event to its
	// match.event so the resulting TriggerOccurrence is fully self-describing.
	Provider string
	Event    string
	Action   string

	// ActivationMode + ActiveEnvironments + ChangedComponents are determined by
	// the planner using internal/trigger.ResolveActiveEnvironments. They are
	// embedded into PlanScope so revision consumers see the full activation
	// state of the trigger that produced their plan.
	ActivationMode     string
	ActiveEnvironments []string
	ChangedComponents  []string

	// Mode is the value written to TriggerOccurrence.Mode. Defaults to
	// ModeEventFile if Action is non-empty (event-file path), otherwise
	// ModeManual.
	Mode string

	// PlanScopeMode overrides PlanScope.Mode (defaults to the binding's
	// plan.scope, or PlanScopeFull if unset).
	PlanScopeMode string
	PlanBase      string
	PlanHead      string

	// Now, when non-zero, pins the CreatedAt timestamp for tests.
	Now time.Time
}

// FromDeclaredTrigger builds a TriggerOccurrence for a plan that was triggered
// by a declared binding. The binding MUST exist in intent.automation.
// triggerBindings; if not, an error is returned without wrapping
// ErrNoMatchingBinding (the binding name was unknown at lookup time, not the
// provider event).
func FromDeclaredTrigger(intent *model.Intent, opts DeclaredOptions) (TriggerOccurrence, error) {
	if intent == nil {
		return TriggerOccurrence{}, fmt.Errorf("triggerctx: intent is nil")
	}
	if opts.TriggerName == "" {
		return TriggerOccurrence{}, fmt.Errorf("triggerctx: trigger name is empty")
	}
	binding, ok := intent.Automation.TriggerBindings[opts.TriggerName]
	if !ok {
		return TriggerOccurrence{}, fmt.Errorf("triggerctx: trigger binding %q not found in automation.triggerBindings", opts.TriggerName)
	}

	provider := opts.Provider
	if provider == "" {
		provider = binding.On.Provider
	}
	event := opts.Event
	if event == "" {
		event = binding.On.Event
	}
	mode := opts.Mode
	if mode == "" {
		if opts.Action != "" {
			mode = ModeEventFile
		} else {
			mode = ModeManual
		}
	}

	source := opts.Source
	if source.WorkingTree == "" {
		source.WorkingTree = WorkingTreeClean
	}
	if source.SourceScope == "" {
		source.SourceScope = opts.TriggerName
	}

	scopeMode := opts.PlanScopeMode
	if scopeMode == "" {
		if binding.Plan.Scope != "" {
			scopeMode = binding.Plan.Scope
		} else {
			scopeMode = PlanScopeFull
		}
	}
	ps := PlanScope{
		Mode:               scopeMode,
		Base:               opts.PlanBase,
		Head:               opts.PlanHead,
		ActivationMode:     opts.ActivationMode,
		ActiveEnvironments: append([]string(nil), opts.ActiveEnvironments...),
		ChangedComponents:  append([]string(nil), opts.ChangedComponents...),
	}
	if scopeMode == PlanScopeChanged {
		if ps.Base == "" {
			ps.Base = source.BaseRevision
		}
		if ps.Head == "" {
			ps.Head = source.HeadRevision
		}
	}

	now := opts.Now
	if now.IsZero() {
		now = time.Now().UTC()
	} else {
		now = now.UTC()
	}

	occ := TriggerOccurrence{
		APIVersion:      APIVersion,
		Kind:            KindName,
		TriggerID:       NewTriggerID(),
		TriggerType:     TriggerTypeDeclared,
		TriggerName:     opts.TriggerName,
		Mode:            mode,
		Provider:        provider,
		Event:           event,
		Action:          opts.Action,
		MatchedBindings: []string{opts.TriggerName},
		Source:          source,
		PlanScope:       ps,
		CreatedAt:       now,
	}
	occ.TriggerKey = TriggerKey(occ)
	return occ, nil
}

// ResolveProviderEvent backs `orun plan --from-ci`. It walks the configured
// trigger bindings, finds every binding that matches the normalized event, and
// (if exactly one matches, or all matches share the same plan scope) returns a
// fully formed declared TriggerOccurrence.
//
// If no binding matches, ResolveProviderEvent returns a *NoMatchingBindingError
// wrapping ErrNoMatchingBinding so callers can distinguish "the CI plumbing
// fired but the intent didn't claim this event" from other failure classes.
func ResolveProviderEvent(intent *model.Intent, event *model.NormalizedEvent, opts DeclaredOptions) (TriggerOccurrence, error) {
	if intent == nil {
		return TriggerOccurrence{}, fmt.Errorf("triggerctx: intent is nil")
	}
	if event == nil {
		return TriggerOccurrence{}, fmt.Errorf("triggerctx: event is nil")
	}

	matched := matchBindings(intent, event)
	if len(matched) == 0 {
		return TriggerOccurrence{}, &NoMatchingBindingError{
			Provider: event.Provider,
			Event:    event.Event,
			Action:   event.Action,
		}
	}

	// Use the existing internal/trigger plan-scope resolver so we agree with
	// the planner on conflict behavior.
	scope, err := resolvePlanScopeFromBindings(intent, matched)
	if err != nil {
		return TriggerOccurrence{}, err
	}

	primary := matched[0]
	binding := intent.Automation.TriggerBindings[primary]

	// Compose the final DeclaredOptions, propagating the matched binding
	// metadata so the resulting occurrence is fully self-describing.
	declared := opts
	declared.TriggerName = primary
	if declared.Provider == "" {
		declared.Provider = event.Provider
	}
	if declared.Event == "" {
		declared.Event = event.Event
	}
	if declared.Action == "" {
		declared.Action = event.Action
	}
	if declared.Mode == "" {
		declared.Mode = ModeEventFile
	}
	if declared.PlanScopeMode == "" {
		declared.PlanScopeMode = scope
	}
	if declared.Source.HeadRevision == "" {
		declared.Source.HeadRevision = event.HeadSHA
	}
	if declared.Source.BaseRevision == "" {
		declared.Source.BaseRevision = event.BaseSHA
	}
	if declared.Source.Ref == "" {
		declared.Source.Ref = event.Ref
	}
	if declared.Source.Repo == "" {
		declared.Source.Repo = event.Repository
	}
	if declared.Source.WorkingTree == "" {
		declared.Source.WorkingTree = WorkingTreeClean
	}
	if declared.PlanBase == "" {
		declared.PlanBase = event.BaseSHA
	}
	if declared.PlanHead == "" {
		declared.PlanHead = event.HeadSHA
	}

	occ, err := FromDeclaredTrigger(intent, declared)
	if err != nil {
		return TriggerOccurrence{}, err
	}
	occ.MatchedBindings = append([]string(nil), matched...)
	// Refresh derived trigger key only if MatchedBindings changed nothing about
	// the source scope; TriggerKey is a function of Source, so it remains
	// correct.
	_ = binding // retained for future debug logging hooks
	return occ, nil
}

// matchBindings is a small re-implementation of the matcher loop in
// internal/trigger/resolve.go scoped to the names we need (so we can return
// a stable, sorted slice without re-deriving environment activations).
func matchBindings(intent *model.Intent, event *model.NormalizedEvent) []string {
	if intent.Automation.TriggerBindings == nil {
		return nil
	}
	var names []string
	for name, binding := range intent.Automation.TriggerBindings {
		if trigger.MatchTrigger(binding, event) {
			names = append(names, name)
		}
	}
	sortStrings(names)
	return names
}

func resolvePlanScopeFromBindings(intent *model.Intent, names []string) (string, error) {
	first := ""
	for _, n := range names {
		b := intent.Automation.TriggerBindings[n]
		if b.Plan.Scope == "" {
			continue
		}
		if first == "" {
			first = b.Plan.Scope
			continue
		}
		if b.Plan.Scope != first {
			return "", fmt.Errorf("triggerctx: matched bindings disagree on plan scope: %v", names)
		}
	}
	if first == "" {
		return PlanScopeFull, nil
	}
	return first, nil
}
