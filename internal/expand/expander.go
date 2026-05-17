package expand

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	compositionpkg "github.com/sourceplane/orun/internal/composition"
	"github.com/sourceplane/orun/internal/model"
)

// Expander handles environment × component expansion and merging
type Expander struct {
	normalized *model.NormalizedIntent
	groups     map[string]model.Group
	registry   *compositionpkg.Registry
}

// NewExpander creates a new expander
func NewExpander(normalized *model.NormalizedIntent) *Expander {
	return &Expander{
		normalized: normalized,
		groups:     normalized.Groups,
	}
}

// WithRegistry sets the composition registry for profile resolution.
func (e *Expander) WithRegistry(registry *compositionpkg.Registry) *Expander {
	e.registry = registry
	return e
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

			// Resolve execution profile if registry is available
			if e.registry != nil {
				if err := e.resolveProfile(instance, comp, envName); err != nil {
					return nil, err
				}
			}

			// Merge all properties (including path) with template interpolation
			merged := e.mergeProperties(comp, env, envName, compName)
			instance.Parameters = merged

			// Merge explicit environment variables
			instance.Env = e.mergeEnv(comp, env, envName)

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
	envNames := component.Subscribe.EnvironmentNames()
	if len(envNames) > 0 {
		if !matchesAny(envNames, envName) {
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

// mergeProperties applies the merge precedence order with proper override hierarchy.
// Precedence (lowest to highest):
//   env parameterDefaults["*"] → env parameterDefaults[type] →
//   group parameterDefaults["*"] → group parameterDefaults[type] →
//   component parameters → subscription parameters
func (e *Expander) mergeProperties(comp model.Component, env model.Environment, envName, compName string) map[string]interface{} {
	merged := make(map[string]interface{})

	var groupPath, envPath string

	// 1. Environment parameterDefaults["*"] - lowest priority
	if env.ParameterDefaults != nil {
		if wildcardDefaults, ok := env.ParameterDefaults["*"]; ok {
			for k, v := range wildcardDefaults {
				if k == "path" {
					if pathStr, ok := v.(string); ok {
						envPath = pathStr
					}
				} else {
					merged[k] = v
				}
			}
		}

		// 2. Environment parameterDefaults[type]
		if typeDefaults, ok := env.ParameterDefaults[comp.Type]; ok {
			for k, v := range typeDefaults {
				if k == "path" {
					if pathStr, ok := v.(string); ok {
						envPath = pathStr
					}
				} else {
					merged[k] = v
				}
			}
		}
	}

	// 3. Group parameterDefaults["*"]
	if comp.Domain != "" {
		if group, exists := e.groups[comp.Domain]; exists {
			if group.ParameterDefaults != nil {
				if wildcardDefaults, ok := group.ParameterDefaults["*"]; ok {
					for k, v := range wildcardDefaults {
						if k == "path" {
							if pathStr, ok := v.(string); ok {
								groupPath = pathStr
							}
						} else {
							merged[k] = v
						}
					}
				}

				// 4. Group parameterDefaults[type]
				if typeDefaults, ok := group.ParameterDefaults[comp.Type]; ok {
					for k, v := range typeDefaults {
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
	}

	// 5. Component parameters
	if comp.Parameters != nil {
		for k, v := range comp.Parameters {
			merged[k] = v
		}
	}

	// 6. Subscription parameters - highest priority
	sub := comp.Subscribe.FindSubscription(envName)
	if sub != nil && sub.Parameters != nil {
		for k, v := range sub.Parameters {
			merged[k] = v
		}
	}

	// 7. Handle path with explicit override hierarchy: component > group > environment > default
	if comp.Path != "" {
		merged["path"] = comp.Path
	} else if groupPath != "" {
		merged["path"] = groupPath
	} else if envPath != "" {
		merged["path"] = envPath
	}

	// 8. Interpolate template variables in all string values
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

// mergeEnv merges environment variables with 4-layer precedence (lowest to highest):
// 1. intent root env
// 2. intent environment env
// 3. component root env
// 4. component subscription env
func (e *Expander) mergeEnv(comp model.Component, env model.Environment, envName string) map[string]string {
	merged := make(map[string]string)

	for k, v := range e.normalized.Env {
		merged[k] = v
	}

	for k, v := range env.Env {
		merged[k] = v
	}

	for k, v := range comp.Env {
		merged[k] = v
	}

	sub := comp.Subscribe.FindSubscription(envName)
	if sub != nil {
		for k, v := range sub.Env {
			merged[k] = v
		}
	}

	for k, v := range merged {
		merged[k] = e.interpolateString(v, envName, comp.Domain, comp.Name)
	}

	return merged
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

// resolveProfile resolves the execution profile for a component/environment instance.
func (e *Expander) resolveProfile(instance *model.ComponentInstance, comp model.Component, envName string) error {
	if e.registry == nil {
		return nil
	}

	compositionKey := comp.ResolvedComposition
	if compositionKey == "" {
		compositionKey = comp.Type
	}

	composition, exists := e.registry.ByKey[compositionKey]
	if !exists {
		if composition, exists = e.registry.Types[comp.Type]; !exists {
			return nil
		}
	}

	subscription := comp.Subscribe.FindSubscription(envName)

	resolved, err := compositionpkg.ResolveProfileRef(comp.Type, composition, subscription)
	if err != nil {
		return fmt.Errorf("component %s environment %s: %w", comp.Name, envName, err)
	}

	instance.ProfileRef = resolved.Ref
	instance.ProfileName = resolved.Name
	instance.ProfileSource = resolved.Source
	return nil
}
