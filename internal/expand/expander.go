package expand

import (
	"regexp"
	"sort"
	"strings"

	"github.com/sourceplane/gluon/internal/model"
)

// Expander handles environment × component expansion and merging
type Expander struct {
	normalized *model.NormalizedIntent
	groups     map[string]model.Group
}

// NewExpander creates a new expander
func NewExpander(normalized *model.NormalizedIntent) *Expander {
	return &Expander{
		normalized: normalized,
		groups:     normalized.Groups,
	}
}

// Expand produces ComponentInstances for each environment × component pair
func (e *Expander) Expand() (map[string][]*model.ComponentInstance, error) {
	result := make(map[string][]*model.ComponentInstance)

	for envName, env := range e.normalized.Environments {
		instances := make([]*model.ComponentInstance, 0)

		// Get applicable components for this environment
		applicableComps := e.getApplicableComponents(envName, env)

		for _, compName := range applicableComps {
			comp, exists := e.normalized.ComponentIndex[compName]
			if !exists {
				continue
			}

			// Skip disabled components
			if !comp.Enabled {
				continue
			}

			// Create instance with merged properties
			instance := &model.ComponentInstance{
				ComponentName: compName,
				Environment:   envName,
				Type:          comp.Type,
				ResolvedComposition:       comp.ResolvedComposition,
				ResolvedCompositionSource: comp.ResolvedCompositionSource,
				Domain:        comp.Domain,
				Labels:        comp.Labels,
				StepOverrides: comp.Overrides.Steps,
				SourcePath:    comp.SourcePath,
				Enabled:       comp.Enabled,
			}

			// Merge all properties (including path) with template interpolation
			merged := e.mergeProperties(comp, env, envName, compName)
			instance.Inputs = merged

			// Extract path from merged properties if it exists
			if pathVal, exists := merged["path"]; exists {
				if pathStr, ok := pathVal.(string); ok {
					instance.Path = pathStr
					// Remove path from inputs so it's not duplicated
					delete(merged, "path")
				}
			} else {
				instance.Path = "./"
			}

			// Extract and apply policies (cannot be overridden)
			instance.Policies = e.resolvePolicies(comp, envName)

			// Resolve dependencies
			deps := e.resolveDependencies(comp, envName)
			instance.DependsOn = deps

			instances = append(instances, instance)
		}

		result[envName] = instances
	}

	return result, nil
}

// getApplicableComponents returns components that apply to an environment
func (e *Expander) getApplicableComponents(envName string, env model.Environment) []string {
	componentNames := make([]string, 0, len(e.normalized.ComponentIndex))
	for name := range e.normalized.ComponentIndex {
		componentNames = append(componentNames, name)
	}
	sort.Strings(componentNames)

	applicable := make([]string, 0, len(componentNames))
	for _, name := range componentNames {
		component := e.normalized.ComponentIndex[name]
		if !componentMatchesEnvironment(component, envName, env) {
			continue
		}
		applicable = append(applicable, name)
	}

	return applicable
}

func componentMatchesEnvironment(component model.Component, envName string, env model.Environment) bool {
	if len(component.Subscribe.Environments) > 0 {
		if !matchesAny(component.Subscribe.Environments, envName) {
			return false
		}
	} else if !matchesAny(env.Selectors.Components, component.Name) {
		return false
	}

	if len(env.Selectors.Domains) > 0 && !matchesAny(env.Selectors.Domains, component.Domain) {
		return false
	}

	return true
}

func matchesAny(patterns []string, value string) bool {
	if len(patterns) == 0 {
		return false
	}
	for _, pattern := range patterns {
		if matchesPattern(pattern, value) {
			return true
		}
	}
	return false
}

func matchesPattern(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(value, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == value
}

// mergeProperties applies the merge precedence order with proper override hierarchy
// Override hierarchy: component > group > environment > default
// Path is handled separately: component path > group path (from defaults) > environment path (from defaults) > default "./"
func (e *Expander) mergeProperties(comp model.Component, env model.Environment, envName, compName string) map[string]interface{} {
	merged := make(map[string]interface{})

	// Collect paths from each level for later use
	var groupPath, envPath string

	// 1. Environment defaults - lowest priority
	if env.Defaults != nil {
		for k, v := range env.Defaults {
			// Extract path from defaults but don't add to merged yet
			if k == "path" {
				if pathStr, ok := v.(string); ok {
					envPath = pathStr
				}
			} else {
				merged[k] = v
			}
		}
	}

	// 2. Group defaults - middle priority (overwrites environment defaults)
	if comp.Domain != "" {
		if group, exists := e.groups[comp.Domain]; exists {
			if group.Defaults != nil {
				for k, v := range group.Defaults {
					// Extract path from defaults but don't add to merged yet
					if k == "path" {
						if pathStr, ok := v.(string); ok {
							groupPath = pathStr
						}
					} else {
						merged[k] = v
					}
				}
			}
		}
	}

	// 3. Component properties - highest priority (overwrites group and environment defaults)
	if comp.Inputs != nil {
		for k, v := range comp.Inputs {
			merged[k] = v
		}
	}

	// 4. Handle path with explicit override hierarchy: component > group > environment > default
	if comp.Path != "" {
		// Component level (highest priority)
		merged["path"] = comp.Path
	} else if groupPath != "" {
		// Group level (from group defaults)
		merged["path"] = groupPath
	} else if envPath != "" {
		// Environment level (from environment defaults)
		merged["path"] = envPath
	}

	// 5. Interpolate template variables in all string values
	return e.interpolateProperties(merged, envName, comp.Domain, compName)
}

// interpolateProperties applies template variable substitution to all string properties
// Supported variables: {{ .environment }}, {{ .group }}, {{ .component }}
func (e *Expander) interpolateProperties(props map[string]interface{}, envName, groupName, compName string) map[string]interface{} {
	result := make(map[string]interface{})

	for k, v := range props {
		if str, ok := v.(string); ok {
			result[k] = e.interpolateString(str, envName, groupName, compName)
		} else {
			result[k] = v
		}
	}

	return result
}

// interpolateString replaces template variables in a string
func (e *Expander) interpolateString(s, envName, groupName, compName string) string {
	result := s

	// Replace template variables
	result = strings.ReplaceAll(result, "{{.environment}}", envName)
	result = strings.ReplaceAll(result, "{{ .environment }}", envName)
	result = strings.ReplaceAll(result, "{{.group}}", groupName)
	result = strings.ReplaceAll(result, "{{ .group }}", groupName)
	result = strings.ReplaceAll(result, "{{.component}}", compName)
	result = strings.ReplaceAll(result, "{{ .component }}", compName)

	// Clean up any remaining unresolved template syntax
	re := regexp.MustCompile(`{{.*?}}`)
	result = re.ReplaceAllString(result, "")

	result = strings.TrimSpace(result)
	return result
}

// resolvePolicies extracts policies that apply to this component in this environment
func (e *Expander) resolvePolicies(comp model.Component, envName string) map[string]interface{} {
	policies := make(map[string]interface{})

	// Get group policies
	if comp.Domain != "" {
		if group, exists := e.groups[comp.Domain]; exists {
			if group.Policies != nil {
				for k, v := range group.Policies {
					policies[k] = v
				}
			}
		}
	}

	// Get environment policies
	if env, exists := e.normalized.Environments[envName]; exists {
		if env.Policies != nil {
			for k, v := range env.Policies {
				policies[k] = v
			}
		}
	}

	return policies
}

// resolveDependencies transforms component dependencies into resolved form
func (e *Expander) resolveDependencies(comp model.Component, envName string) []model.ResolvedDependency {
	resolved := make([]model.ResolvedDependency, 0)

	for _, dep := range comp.DependsOn {
		// Handle same-environment marker
		targetEnv := dep.Environment
		if dep.Environment == "__same__" {
			targetEnv = envName
		}

		resolved = append(resolved, model.ResolvedDependency{
			ComponentName: dep.Component,
			Environment:   targetEnv,
			Scope:         dep.Scope,
			Condition:     dep.Condition,
		})
	}

	return resolved
}

// GetComponentInstance retrieves a specific component instance
func (e *Expander) GetComponentInstance(envName, compName string, instances map[string][]*model.ComponentInstance) *model.ComponentInstance {
	if envInstances, exists := instances[envName]; exists {
		for _, inst := range envInstances {
			if inst.ComponentName == compName {
				return inst
			}
		}
	}
	return nil
}
