package triggerctx

import (
	"fmt"
	"time"

	"github.com/sourceplane/orun/internal/model"
)

// ResolveOptions parameterizes ResolveTriggerContext. The CLI translates user
// flags (`--trigger`, `--from-ci`, `--changed`, `--replay`, `--api`) into the
// fields below before calling.
type ResolveOptions struct {
	// Kind selects which branch the dispatcher takes.
	Kind ResolveKind

	// TriggerName is required when Kind == ResolveKindDeclaredByName.
	TriggerName string

	// ProviderEvent is required when Kind == ResolveKindFromCI.
	ProviderEvent *model.NormalizedEvent

	// Source describes the workspace VCS state. ResolveTriggerContext does not
	// probe git itself; the caller is responsible for filling Source via the
	// `git` argument's adapter (e.g. internal/git). A nil git adapter is
	// permitted — the dispatcher then uses opts.Source verbatim.
	Source TriggerSource

	// SystemFlavor selects which NewSystem* constructor to invoke when
	// Kind == ResolveKindSystem. Defaults to SystemManual.
	SystemFlavor string

	// Action propagates to TriggerOccurrence.Action when relevant.
	Action string

	// Plan-scope inputs propagated into the resulting occurrence. The
	// dispatcher applies sensible defaults per branch.
	ActivationMode     string
	ActiveEnvironments []string
	ChangedComponents  []string
	PlanScopeMode      string
	PlanBase           string
	PlanHead           string

	// Now, when non-zero, pins CreatedAt for tests.
	Now time.Time
}

// ResolveKind selects the dispatcher branch.
type ResolveKind int

const (
	// ResolveKindSystem invokes one of the New*System constructors. Used for
	// ad-hoc `orun plan`, `orun plan --changed`, `orun run --plan <file>`,
	// `orun state migrate`, and SaaS API entrypoints.
	ResolveKindSystem ResolveKind = iota

	// ResolveKindDeclaredByName resolves the binding named by opts.TriggerName
	// (i.e. `orun plan --trigger <name>`).
	ResolveKindDeclaredByName

	// ResolveKindFromCI matches opts.ProviderEvent against the configured
	// trigger bindings (i.e. `orun plan --from-ci`). No-match returns
	// *NoMatchingBindingError.
	ResolveKindFromCI

	// ResolveKindReplay materializes a system.replay occurrence (used by
	// `orun run --plan <file>`).
	ResolveKindReplay
)

// GitSource is the minimal adapter ResolveTriggerContext consumes to fill the
// occurrence's Source field. Pass nil to skip git probing — opts.Source is
// then used verbatim. Implementations should be cheap to call (cached) since
// the dispatcher may invoke them multiple times.
type GitSource interface {
	// TriggerSource returns the current workspace VCS state, or an error if
	// the workspace is not a git repository. ResolveTriggerContext treats a
	// returned error as a soft signal — Source is then taken from opts.Source.
	TriggerSource() (TriggerSource, error)
}

// ResolveTriggerContext is the single entrypoint the planner uses to obtain a
// TriggerOccurrence for the plan it is about to compile. It dispatches to one
// of four branches based on opts.Kind:
//
//   - ResolveKindSystem            → New*System (manual / manual-changed /
//                                    api / migrated) selected via SystemFlavor.
//   - ResolveKindDeclaredByName    → FromDeclaredTrigger using opts.TriggerName.
//   - ResolveKindFromCI            → ResolveProviderEvent against opts.ProviderEvent.
//   - ResolveKindReplay            → NewSystemReplay.
//
// The intent argument is required only for the declared/from-ci branches; the
// system/replay branches accept a nil intent.
//
// A *NoMatchingBindingError is returned (wrapping ErrNoMatchingBinding) when
// Kind == ResolveKindFromCI and no binding matches — this is the typed error
// the CLI maps to a deterministic exit code per design.md §11.
func ResolveTriggerContext(opts ResolveOptions, intent *model.Intent, git GitSource) (TriggerOccurrence, error) {
	source := opts.Source
	if git != nil {
		if probed, err := git.TriggerSource(); err == nil {
			source = mergeSource(source, probed)
		}
	}

	switch opts.Kind {
	case ResolveKindSystem:
		flavor := opts.SystemFlavor
		if flavor == "" {
			flavor = SystemManual
		}
		sysOpts := SystemOptions{
			Source:    source,
			PlanScope: planScopeFromOpts(opts),
			Action:    opts.Action,
			Now:       opts.Now,
		}
		switch flavor {
		case SystemManual:
			return NewSystemManual(sysOpts), nil
		case SystemManualChanged:
			return NewSystemManualChanged(sysOpts), nil
		case SystemAPI:
			return NewSystemAPI(sysOpts), nil
		case SystemMigrated:
			return NewSystemMigrated(sysOpts), nil
		case SystemReplay:
			return NewSystemReplay(sysOpts), nil
		default:
			return TriggerOccurrence{}, fmt.Errorf("triggerctx: unknown system flavor %q", flavor)
		}

	case ResolveKindReplay:
		return NewSystemReplay(SystemOptions{
			Source:    source,
			PlanScope: planScopeFromOpts(opts),
			Action:    opts.Action,
			Now:       opts.Now,
		}), nil

	case ResolveKindDeclaredByName:
		return FromDeclaredTrigger(intent, DeclaredOptions{
			TriggerName:        opts.TriggerName,
			Source:             source,
			Action:             opts.Action,
			ActivationMode:     opts.ActivationMode,
			ActiveEnvironments: opts.ActiveEnvironments,
			ChangedComponents:  opts.ChangedComponents,
			PlanScopeMode:      opts.PlanScopeMode,
			PlanBase:           opts.PlanBase,
			PlanHead:           opts.PlanHead,
			Now:                opts.Now,
		})

	case ResolveKindFromCI:
		return ResolveProviderEvent(intent, opts.ProviderEvent, DeclaredOptions{
			Source:             source,
			Action:             opts.Action,
			ActivationMode:     opts.ActivationMode,
			ActiveEnvironments: opts.ActiveEnvironments,
			ChangedComponents:  opts.ChangedComponents,
			PlanScopeMode:      opts.PlanScopeMode,
			PlanBase:           opts.PlanBase,
			PlanHead:           opts.PlanHead,
			Now:                opts.Now,
		})

	default:
		return TriggerOccurrence{}, fmt.Errorf("triggerctx: unknown resolve kind %d", opts.Kind)
	}
}

// planScopeFromOpts builds a PlanScope from the fields a system-flavor
// invocation carries. System constructors apply their own per-flavor Mode
// default if PlanScope.Mode is empty.
func planScopeFromOpts(opts ResolveOptions) PlanScope {
	return PlanScope{
		Mode:               opts.PlanScopeMode,
		Base:               opts.PlanBase,
		Head:               opts.PlanHead,
		ActivationMode:     opts.ActivationMode,
		ActiveEnvironments: append([]string(nil), opts.ActiveEnvironments...),
		ChangedComponents:  append([]string(nil), opts.ChangedComponents...),
	}
}

// mergeSource lets opts.Source override probed git values per-field so the
// CLI can stamp e.g. a specific Repo while leaving the rest to git probing.
func mergeSource(override, probed TriggerSource) TriggerSource {
	out := probed
	if override.Repo != "" {
		out.Repo = override.Repo
	}
	if override.Ref != "" {
		out.Ref = override.Ref
	}
	if override.SourceScope != "" {
		out.SourceScope = override.SourceScope
	}
	if override.HeadRevision != "" {
		out.HeadRevision = override.HeadRevision
	}
	if override.BaseRevision != "" {
		out.BaseRevision = override.BaseRevision
	}
	if override.WorkingTree != "" {
		out.WorkingTree = override.WorkingTree
	}
	return out
}

// sortStrings provides a small in-place sort without dragging in sort.Strings
// for every file. Internal helper.
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}
