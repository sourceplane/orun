package composition

import (
	"fmt"

	"github.com/sourceplane/orun/internal/model"
)

// ResolvedDependencyMode is the outcome of dependency-mode resolution
// for a single component/environment pair under a given trigger context.
type ResolvedDependencyMode struct {
	// Mode is one of model.DependencyMode{Enforced,Advisory,Disabled}.
	// Always set to a concrete value (never empty).
	Mode string
	// Source is the layer that supplied the mode: "default",
	// "environment", "subscription", or "subscription-rule".
	Source string
	// RuleTriggerRef is set only when Source == "subscription-rule"; it
	// records which trigger ref matched.
	RuleTriggerRef string
}

// ResolveDependencyMode evaluates the dependencyMode precedence chain:
//
//  1. subscription.DependencyRules whose when.triggerRef matched
//  2. subscription.DependencyMode
//  3. env.DependencyMode
//  4. built-in default model.DependencyModeEnforced
//
// Any mode that fails the IsValidDependencyMode check is rejected as an
// error so misconfiguration is surfaced at plan time, not at runtime.
func ResolveDependencyMode(
	env model.Environment,
	subscription *model.EnvironmentSubscription,
	matchedTriggers []string,
) (ResolvedDependencyMode, error) {
	// 1. subscription rules (first match wins)
	if subscription != nil && len(subscription.DependencyRules) > 0 && len(matchedTriggers) > 0 {
		triggerSet := make(map[string]struct{}, len(matchedTriggers))
		for _, t := range matchedTriggers {
			triggerSet[t] = struct{}{}
		}
		for i, rule := range subscription.DependencyRules {
			if !model.IsValidDependencyMode(rule.Mode) || rule.Mode == "" {
				return ResolvedDependencyMode{}, fmt.Errorf(
					"dependencyRules[%d].mode %q is invalid (want one of enforced|advisory|disabled)",
					i, rule.Mode,
				)
			}
			if _, ok := triggerSet[rule.When.TriggerRef]; ok {
				return ResolvedDependencyMode{
					Mode:           rule.Mode,
					Source:         "subscription-rule",
					RuleTriggerRef: rule.When.TriggerRef,
				}, nil
			}
		}
	}

	// 2. subscription default
	if subscription != nil && subscription.DependencyMode != "" {
		if !model.IsValidDependencyMode(subscription.DependencyMode) {
			return ResolvedDependencyMode{}, fmt.Errorf(
				"subscription.dependencyMode %q is invalid (want one of enforced|advisory|disabled)",
				subscription.DependencyMode,
			)
		}
		return ResolvedDependencyMode{
			Mode:   subscription.DependencyMode,
			Source: "subscription",
		}, nil
	}

	// 3. environment default
	if env.DependencyMode != "" {
		if !model.IsValidDependencyMode(env.DependencyMode) {
			return ResolvedDependencyMode{}, fmt.Errorf(
				"environment.dependencyMode %q is invalid (want one of enforced|advisory|disabled)",
				env.DependencyMode,
			)
		}
		return ResolvedDependencyMode{
			Mode:   env.DependencyMode,
			Source: "environment",
		}, nil
	}

	// 4. built-in default
	return ResolvedDependencyMode{
		Mode:   model.DependencyModeEnforced,
		Source: "default",
	}, nil
}
