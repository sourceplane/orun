package normalize

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/model"
)

// NormalizeIntent transforms raw intent into canonical form
func NormalizeIntent(intent *model.Intent) (*model.NormalizedIntent, error) {
	if intent == nil {
		return nil, fmt.Errorf("intent cannot be nil")
	}

	// Initialize normalized intent
	normalized := &model.NormalizedIntent{
		Metadata:       intent.Metadata,
		Groups:         intent.Groups,
		Environments:   intent.Environments,
		Components:     make(map[string]model.Component),
		ComponentIndex: make(map[string]model.Component),
		Env:            intent.Env,
	}

	if normalized.Env == nil {
		normalized.Env = make(map[string]string)
	}

	// Validate intent root env: ORUN_* prefix is reserved
	if err := validateEnvKeys(normalized.Env, "intent root env"); err != nil {
		return nil, err
	}

	// Normalize components
	for _, comp := range intent.Components {
		// Default enabled to true
		if comp.Name == "" {
			return nil, fmt.Errorf("component must have a name")
		}
		if _, exists := normalized.Components[comp.Name]; exists {
			return nil, fmt.Errorf("duplicate component name: %s", comp.Name)
		}
		if comp.Type == "" {
			return nil, fmt.Errorf("component %s must have a type", comp.Name)
		}

		// Set enabled default
		if !comp.Enabled && comp.Enabled != true {
			comp.Enabled = true
		}

		// Initialize empty maps
		if comp.Labels == nil {
			comp.Labels = make(map[string]string)
		}
		if comp.Inputs == nil {
			comp.Inputs = make(map[string]interface{})
		}
		if comp.Subscribe.Environments == nil {
			comp.Subscribe.Environments = []model.EnvironmentSubscription{}
		}
		if comp.DependsOn == nil {
			comp.DependsOn = []model.Dependency{}
		}
		if comp.Overrides.Steps == nil {
			comp.Overrides.Steps = []model.Step{}
		}
		if comp.Env == nil {
			comp.Env = make(map[string]string)
		}

		// Validate component root env: ORUN_* prefix is reserved
		if err := validateEnvKeys(comp.Env, fmt.Sprintf("component %s root env", comp.Name)); err != nil {
			return nil, err
		}

		// Validate subscription env
		for _, sub := range comp.Subscribe.Environments {
			if err := validateEnvKeys(sub.Env, fmt.Sprintf("component %s subscription %s env", comp.Name, sub.Name)); err != nil {
				return nil, err
			}
		}

		// Normalize dependencies
		for i := range comp.DependsOn {
			dep := &comp.DependsOn[i]
			// Default empty environment to "same-environment"
			if dep.Environment == "" {
				dep.Environment = "__same__"
			}
			// Default scope
			if dep.Scope == "" {
				dep.Scope = "same-environment"
			}
			// Default condition
			if dep.Condition == "" {
				dep.Condition = "success"
			}
		}

		normalized.Components[comp.Name] = comp
		normalized.ComponentIndex[comp.Name] = comp
	}

	// Validate environment selectors and env
	for envName, env := range normalized.Environments {
		if env.Selectors.Components == nil {
			env.Selectors.Components = []string{}
		}
		if env.Selectors.Domains == nil {
			env.Selectors.Domains = []string{}
		}
		if env.Env == nil {
			env.Env = make(map[string]string)
		}

		// Validate environment env: ORUN_* prefix is reserved
		if err := validateEnvKeys(env.Env, fmt.Sprintf("environment %s env", envName)); err != nil {
			return nil, err
		}

		// Normalize promotion dependencies
		for i := range env.Promotion.DependsOn {
			dep := &env.Promotion.DependsOn[i]
			if dep.Environment == "" {
				return nil, fmt.Errorf("environment %s: promotion dependency requires environment field", envName)
			}
			if dep.Environment == envName {
				return nil, fmt.Errorf("environment %s: promotion dependency cannot reference itself", envName)
			}
			if _, exists := normalized.Environments[dep.Environment]; !exists {
				return nil, fmt.Errorf("environment %s: promotion dependency references non-existent environment %q", envName, dep.Environment)
			}
			if dep.Strategy == "" {
				dep.Strategy = "same-component"
			}
			if dep.Condition == "" {
				dep.Condition = "success"
			}
			if dep.Satisfy == "" {
				dep.Satisfy = "same-plan-or-previous-success"
			}
			if dep.Match.Revision == "" {
				dep.Match.Revision = "source"
			}
		}

		// Expand wildcards
		if contains(env.Selectors.Components, "*") {
			expandedComps := make([]string, 0)
			for compName := range normalized.ComponentIndex {
				expandedComps = append(expandedComps, compName)
			}
			sort.Strings(expandedComps)
			env.Selectors.Components = expandedComps
		}

		normalized.Environments[envName] = env
	}

	// Detect promotion dependency cycles
	if err := detectPromotionCycles(normalized.Environments); err != nil {
		return nil, err
	}

	return normalized, nil
}

// contains checks if a slice contains a string
func contains(slice []string, item string) bool {
	for _, v := range slice {
		if v == item {
			return true
		}
	}
	return false
}

// validateEnvKeys rejects user-defined env vars that use the reserved ORUN_ prefix.
func validateEnvKeys(env map[string]string, context string) error {
	for k := range env {
		if strings.HasPrefix(strings.ToUpper(k), "ORUN_") {
			return fmt.Errorf("%s: env key %q uses reserved ORUN_ prefix", context, k)
		}
	}
	return nil
}

// matchesWildcard checks if a component name matches a wildcard pattern
func matchesWildcard(pattern, name string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(name, prefix)
	}
	return pattern == name
}

// detectPromotionCycles checks for cycles in environment promotion dependencies using DFS.
func detectPromotionCycles(environments map[string]model.Environment) error {
	const (
		white = 0 // unvisited
		gray  = 1 // in current path
		black = 2 // fully processed
	)

	color := make(map[string]int)
	for envName := range environments {
		color[envName] = white
	}

	var visit func(envName string, path []string) error
	visit = func(envName string, path []string) error {
		color[envName] = gray
		path = append(path, envName)

		env := environments[envName]
		for _, dep := range env.Promotion.DependsOn {
			target := dep.Environment
			switch color[target] {
			case gray:
				cycle := append(path, target)
				return fmt.Errorf("promotion dependency cycle detected: %s", strings.Join(cycle, " → "))
			case white:
				if err := visit(target, path); err != nil {
					return err
				}
			}
		}

		color[envName] = black
		return nil
	}

	for envName := range environments {
		if color[envName] == white {
			if err := visit(envName, nil); err != nil {
				return err
			}
		}
	}

	return nil
}
