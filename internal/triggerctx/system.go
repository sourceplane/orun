package triggerctx

import "time"

// SystemOptions parameterizes the New*System constructors. All fields are
// optional; defaults mirror an ad-hoc local invocation. Callers building a
// system trigger from a real workspace are expected to fill at minimum Source
// and PlanScope.
type SystemOptions struct {
	// Source describes the working-tree state. WorkingTree should be set to
	// triggerctx.WorkingTreeClean or WorkingTreeDirty. SourceScope, if empty,
	// defaults to a sensible per-trigger fallback (e.g. "manual" for
	// NewSystemManual).
	Source TriggerSource

	// PlanScope is copied verbatim into the resulting TriggerOccurrence.
	// If PlanScope.Mode is empty, NewSystemManual / NewSystemAPI default it to
	// PlanScopeFull and NewSystemManualChanged defaults it to PlanScopeChanged.
	PlanScope PlanScope

	// Action is optional and propagates to TriggerOccurrence.Action; system
	// triggers leave this empty by convention.
	Action string

	// Now, when non-zero, pins the CreatedAt timestamp. Tests use this to make
	// occurrences reproducible; production callers should leave it zero so the
	// constructor stamps time.Now().UTC().
	Now time.Time
}

// NewSystemManual builds the TriggerOccurrence used for an ad-hoc `orun plan`
// invocation that did not flow through a CI binding.
func NewSystemManual(opts SystemOptions) TriggerOccurrence {
	return newSystem(SystemManual, ModeManual, defaultScope(opts.Source, "manual"), opts, PlanScopeFull)
}

// NewSystemManualChanged is the variant of NewSystemManual used when the user
// scoped the plan with `--changed` rather than a full compile.
func NewSystemManualChanged(opts SystemOptions) TriggerOccurrence {
	return newSystem(SystemManualChanged, ModeChanged, defaultScope(opts.Source, "manual-changed"), opts, PlanScopeChanged)
}

// NewSystemReplay records that a plan was materialized from a stored artifact
// (e.g. `orun run --plan <file>`), not freshly compiled.
func NewSystemReplay(opts SystemOptions) TriggerOccurrence {
	return newSystem(SystemReplay, ModeReplay, defaultScope(opts.Source, "replay"), opts, PlanScopeFull)
}

// NewSystemAPI marks plans synthesized by an in-process API caller (used by
// future SaaS/Cloud control planes).
func NewSystemAPI(opts SystemOptions) TriggerOccurrence {
	return newSystem(SystemAPI, ModeAPI, defaultScope(opts.Source, "api"), opts, PlanScopeFull)
}

// NewSystemMigrated is used by `orun state migrate` to attach legacy plans
// that have no original trigger record.
func NewSystemMigrated(opts SystemOptions) TriggerOccurrence {
	return newSystem(SystemMigrated, ModeMigration, defaultScope(opts.Source, "migrated"), opts, PlanScopeFull)
}

// defaultScope returns the SourceScope to use when the caller has not supplied
// one. For a dirty working tree we always return "local-dirty"; for a clean
// tree without a meaningful source scope we fall back to the constructor's
// per-flavor default (e.g. "manual").
func defaultScope(src TriggerSource, fallback string) string {
	if src.SourceScope != "" {
		return src.SourceScope
	}
	if src.WorkingTree == WorkingTreeDirty {
		return "local-dirty"
	}
	return fallback
}

func newSystem(name, mode, scope string, opts SystemOptions, defaultPlanMode string) TriggerOccurrence {
	source := opts.Source
	if source.SourceScope == "" {
		source.SourceScope = scope
	}
	if source.WorkingTree == "" {
		source.WorkingTree = WorkingTreeClean
	}

	ps := opts.PlanScope.Clone()
	if ps.Mode == "" {
		ps.Mode = defaultPlanMode
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
		TriggerType:     TriggerTypeSystem,
		TriggerName:     name,
		Mode:            mode,
		Provider:        ProviderOrun,
		Event:           EventManual,
		Action:          opts.Action,
		MatchedBindings: []string{name},
		Source:          source,
		PlanScope:       ps,
		CreatedAt:       now,
	}
	occ.TriggerKey = TriggerKey(occ)
	return occ
}
