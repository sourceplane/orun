package trigger

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/model"
)

// ResolveActiveEnvironments determines which environments are activated
// by the trigger context, respecting explicit --env filtering.
func ResolveActiveEnvironments(
	intent *model.Intent,
	triggerCtx model.TriggerContext,
	explicitEnv string,
) ([]string, *model.TriggerResolution, error) {
	switch triggerCtx.Mode {
	case "none", "":
		return resolveNoTrigger(intent, explicitEnv)
	case "named-trigger":
		return resolveNamedTrigger(intent, triggerCtx.TriggerName, explicitEnv)
	case "event-file":
		return resolveEventFile(intent, triggerCtx.Event, explicitEnv)
	default:
		return nil, nil, fmt.Errorf("unknown trigger mode: %s", triggerCtx.Mode)
	}
}

func resolveNoTrigger(intent *model.Intent, explicitEnv string) ([]string, *model.TriggerResolution, error) {
	envs := allEnvironmentNames(intent)
	if explicitEnv != "" {
		envs = filterEnvs(envs, explicitEnv)
	}
	return envs, nil, nil
}

func resolveNamedTrigger(intent *model.Intent, triggerName, explicitEnv string) ([]string, *model.TriggerResolution, error) {
	if intent.Automation.TriggerBindings == nil {
		return nil, nil, fmt.Errorf("trigger binding %q was not found in automation.triggerBindings", triggerName)
	}

	binding, exists := intent.Automation.TriggerBindings[triggerName]
	if !exists {
		return nil, nil, fmt.Errorf("trigger binding %q was not found in automation.triggerBindings", triggerName)
	}

	activated := environmentsForTrigger(intent, triggerName)
	if len(activated) == 0 {
		return nil, nil, fmt.Errorf("trigger binding %q matched, but no environment references it in activation.triggerRefs", triggerName)
	}

	if explicitEnv != "" {
		filtered := intersectEnvs(activated, explicitEnv)
		if len(filtered) == 0 {
			return nil, nil, fmt.Errorf("environment %s is not activated by trigger %s", explicitEnv, triggerName)
		}
		activated = filtered
	}

	sort.Strings(activated)
	resolution := &model.TriggerResolution{
		MatchedTriggerNames: []string{triggerName},
		ActiveEnvironments:  activated,
		PlanScope:           binding.Plan.Scope,
		Base:                binding.Plan.Base,
		Head:                binding.Plan.Head,
	}

	return activated, resolution, nil
}

func resolveEventFile(intent *model.Intent, event *model.NormalizedEvent, explicitEnv string) ([]string, *model.TriggerResolution, error) {
	if event == nil {
		return nil, nil, fmt.Errorf("event is nil")
	}
	if intent.Automation.TriggerBindings == nil || len(intent.Automation.TriggerBindings) == 0 {
		return nil, nil, fmt.Errorf("no trigger bindings defined in automation.triggerBindings")
	}

	// Find all matching trigger bindings
	var matchedNames []string
	for name, binding := range intent.Automation.TriggerBindings {
		if MatchTrigger(binding, event) {
			matchedNames = append(matchedNames, name)
		}
	}
	sort.Strings(matchedNames)

	if len(matchedNames) == 0 {
		return nil, nil, fmt.Errorf("no trigger binding matched %s event %s action %s", event.Provider, event.Event, event.Action)
	}

	// Check for conflicting plan scopes
	scope, err := resolvePlanScope(intent, matchedNames)
	if err != nil {
		return nil, nil, err
	}

	// Collect activated environments
	activated := environmentsForTriggers(intent, matchedNames)
	if len(activated) == 0 {
		return nil, nil, fmt.Errorf("trigger binding %q matched, but no environment references it in activation.triggerRefs", strings.Join(matchedNames, ", "))
	}

	if explicitEnv != "" {
		filtered := intersectEnvs(activated, explicitEnv)
		if len(filtered) == 0 {
			return nil, nil, fmt.Errorf("environment %s is not activated by any matched trigger", explicitEnv)
		}
		activated = filtered
	}

	sort.Strings(activated)

	// Resolve base/head from event paths
	base, head := resolveBaseHead(intent, matchedNames, event)

	resolution := &model.TriggerResolution{
		MatchedTriggerNames: matchedNames,
		ActiveEnvironments:  activated,
		PlanScope:           scope,
		Base:                base,
		Head:                head,
	}

	return activated, resolution, nil
}

func resolvePlanScope(intent *model.Intent, matchedNames []string) (string, error) {
	var scopes []string
	for _, name := range matchedNames {
		binding := intent.Automation.TriggerBindings[name]
		if binding.Plan.Scope != "" {
			scopes = append(scopes, binding.Plan.Scope)
		}
	}

	if len(scopes) == 0 {
		return "full", nil
	}

	first := scopes[0]
	for _, s := range scopes[1:] {
		if s != first {
			return "", fmt.Errorf("multiple trigger bindings matched this event but define conflicting plan scopes: %s", formatConflicts(intent, matchedNames))
		}
	}
	return first, nil
}

func resolveBaseHead(intent *model.Intent, matchedNames []string, event *model.NormalizedEvent) (string, string) {
	var base, head string
	for _, name := range matchedNames {
		binding := intent.Automation.TriggerBindings[name]
		if binding.Plan.Base != "" && base == "" {
			if resolved, ok := ResolvePath(event.Raw, binding.Plan.Base); ok {
				base = resolved
			}
		}
		if binding.Plan.Head != "" && head == "" {
			if resolved, ok := ResolvePath(event.Raw, binding.Plan.Head); ok {
				head = resolved
			}
		}
	}

	// Fall back to event-level base/head
	if base == "" {
		base = event.BaseSHA
	}
	if head == "" {
		head = event.HeadSHA
	}

	return base, head
}

// ResolvePath resolves a dot-separated path against the raw event payload.
func ResolvePath(raw map[string]any, path string) (string, bool) {
	if raw == nil || path == "" {
		return "", false
	}

	parts := strings.Split(path, ".")
	current := any(raw)

	for i, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return "", false
		}
		val, exists := m[part]
		if !exists {
			return "", false
		}
		if i == len(parts)-1 {
			if s, ok := val.(string); ok {
				return s, true
			}
			return fmt.Sprintf("%v", val), true
		}
		current = val
	}
	return "", false
}

func environmentsForTrigger(intent *model.Intent, triggerName string) []string {
	var envs []string
	for envName, env := range intent.Environments {
		for _, ref := range env.Activation.TriggerRefs {
			if ref == triggerName {
				envs = append(envs, envName)
				break
			}
		}
	}
	sort.Strings(envs)
	return envs
}

func environmentsForTriggers(intent *model.Intent, triggerNames []string) []string {
	triggerSet := make(map[string]struct{}, len(triggerNames))
	for _, n := range triggerNames {
		triggerSet[n] = struct{}{}
	}

	envSet := make(map[string]struct{})
	for envName, env := range intent.Environments {
		for _, ref := range env.Activation.TriggerRefs {
			if _, ok := triggerSet[ref]; ok {
				envSet[envName] = struct{}{}
				break
			}
		}
	}

	envs := make([]string, 0, len(envSet))
	for name := range envSet {
		envs = append(envs, name)
	}
	sort.Strings(envs)
	return envs
}

func allEnvironmentNames(intent *model.Intent) []string {
	envs := make([]string, 0, len(intent.Environments))
	for name := range intent.Environments {
		envs = append(envs, name)
	}
	sort.Strings(envs)
	return envs
}

func filterEnvs(envs []string, filter string) []string {
	filters := parseCommaSeparated(filter)
	var result []string
	for _, env := range envs {
		for _, f := range filters {
			if f == env {
				result = append(result, env)
				break
			}
		}
	}
	return result
}

func intersectEnvs(envs []string, filter string) []string {
	filters := parseCommaSeparated(filter)
	filterSet := make(map[string]struct{}, len(filters))
	for _, f := range filters {
		filterSet[f] = struct{}{}
	}
	var result []string
	for _, env := range envs {
		if _, ok := filterSet[env]; ok {
			result = append(result, env)
		}
	}
	return result
}

func parseCommaSeparated(s string) []string {
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		trimmed := strings.TrimSpace(p)
		if trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func formatConflicts(intent *model.Intent, matchedNames []string) string {
	var parts []string
	for _, name := range matchedNames {
		binding := intent.Automation.TriggerBindings[name]
		parts = append(parts, fmt.Sprintf("%s=%s", name, binding.Plan.Scope))
	}
	return strings.Join(parts, ", ")
}
