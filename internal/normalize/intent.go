package normalize

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/arx/internal/model"
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
			comp.Subscribe.Environments = []string{}
		}
		if comp.DependsOn == nil {
			comp.DependsOn = []model.Dependency{}
		}
		if comp.Overrides.Steps == nil {
			comp.Overrides.Steps = []model.Step{}
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

	// Validate environment selectors
	for envName, env := range normalized.Environments {
		if env.Selectors.Components == nil {
			env.Selectors.Components = []string{}
		}
		if env.Selectors.Domains == nil {
			env.Selectors.Domains = []string{}
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
