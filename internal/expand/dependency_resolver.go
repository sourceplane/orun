package expand

import (
	"github.com/sourceplane/arx/internal/model"
)

// DependencyResolver provides utilities for resolving component dependencies
type DependencyResolver struct {
	components map[string]model.Component
}

// NewDependencyResolver creates a new dependency resolver
func NewDependencyResolver(normalized *model.NormalizedIntent) *DependencyResolver {
	return &DependencyResolver{
		components: normalized.Components,
	}
}

// GetDependencies returns all direct dependencies of a component
func (dr *DependencyResolver) GetDependencies(componentName string) []string {
	comp, exists := dr.components[componentName]
	if !exists {
		return []string{}
	}

	deps := make([]string, 0)
	for _, dep := range comp.DependsOn {
		deps = append(deps, dep.Component)
	}
	return deps
}

// GetDependents returns all components that depend on the given component
func (dr *DependencyResolver) GetDependents(componentName string) []string {
	dependents := make([]string, 0)

	for name, comp := range dr.components {
		for _, dep := range comp.DependsOn {
			if dep.Component == componentName {
				dependents = append(dependents, name)
				break
			}
		}
	}

	return dependents
}

// GetTransitiveDependencies returns all transitive dependencies of a component
func (dr *DependencyResolver) GetTransitiveDependencies(componentName string) map[string]bool {
	result := make(map[string]bool)
	visited := make(map[string]bool)

	var traverse func(string)
	traverse = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true

		deps := dr.GetDependencies(name)
		for _, dep := range deps {
			result[dep] = true
			traverse(dep)
		}
	}

	traverse(componentName)
	return result
}

// GetTransitiveDependents returns all components that transitively depend on the given component
func (dr *DependencyResolver) GetTransitiveDependents(componentName string) map[string]bool {
	result := make(map[string]bool)
	visited := make(map[string]bool)

	var traverse func(string)
	traverse = func(name string) {
		if visited[name] {
			return
		}
		visited[name] = true

		dependents := dr.GetDependents(name)
		for _, dep := range dependents {
			result[dep] = true
			traverse(dep)
		}
	}

	traverse(componentName)
	return result
}

// ResolveComponentSet takes a set of changed components and returns:
// - All changed components
// - All their dependencies (so planner can resolve)
// Useful for plan --changed scenarios
func (dr *DependencyResolver) ResolveComponentSet(changedComponents map[string]bool) map[string]bool {
	included := make(map[string]bool)

	// Add all changed components
	for comp := range changedComponents {
		included[comp] = true
	}

	// Recursively add all dependencies
	changed := true
	for changed {
		changed = false
		for comp := range included {
			deps := dr.GetDependencies(comp)
			for _, dep := range deps {
				if !included[dep] {
					included[dep] = true
					changed = true
				}
			}
		}
	}

	return included
}

// CategorizeDependencies takes changed components and returns three sets:
// - Changed: the original changed components
// - Dependencies: components needed by changed ones
// - Dependents: components that depend on changed ones
// Useful for component --changed scenarios
func (dr *DependencyResolver) CategorizeDependencies(changedComponents map[string]bool) (
	changed map[string]bool,
	dependencies map[string]bool,
	dependents map[string]bool,
) {
	changed = make(map[string]bool)
	dependencies = make(map[string]bool)
	dependents = make(map[string]bool)

	// Copy changed components
	for comp := range changedComponents {
		changed[comp] = true
	}

	// Collect all dependencies and dependents
	for comp := range changedComponents {
		// Get transitive dependencies
		deps := dr.GetTransitiveDependencies(comp)
		for dep := range deps {
			if !changed[dep] {
				dependencies[dep] = true
			}
		}

		// Get transitive dependents
		depts := dr.GetTransitiveDependents(comp)
		for dept := range depts {
			if !changed[dept] {
				dependents[dept] = true
			}
		}
	}

	return
}
