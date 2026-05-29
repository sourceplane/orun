package expand

import (
	"github.com/sourceplane/orun/internal/model"
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

// dependencyForEdge looks up the Dependency declaration on a component
// for a specific target component name. Returns nil if not found.
func (dr *DependencyResolver) dependencyForEdge(componentName, depName string) *model.Dependency {
	comp, exists := dr.components[componentName]
	if !exists {
		return nil
	}
	for i := range comp.DependsOn {
		if comp.DependsOn[i].Component == depName {
			return &comp.DependsOn[i]
		}
	}
	return nil
}

// ResolveComponentSet takes a set of seed components and grows it
// according to each dependency edge's include policy:
//
//   - include: if-selected — do NOT pull the dependency in (order-only).
//   - include: always      — pull the dependency in and recurse.
//
// This is the include-aware replacement for the legacy "pull every
// transitive dependency" behavior. The default include policy is
// "if-selected" (set during normalization), so by default the seed
// set is returned unchanged — change-detection no longer silently
// includes unchanged components.
//
// Backwards-compatible callers that want the old "include everything
// transitively reachable" behavior should call ResolveComponentSetAll
// instead.
func (dr *DependencyResolver) ResolveComponentSet(seedComponents map[string]bool) map[string]bool {
	included := make(map[string]bool, len(seedComponents))
	for comp := range seedComponents {
		included[comp] = true
	}

	// Iterate until the set stops growing. Only "include: always" edges
	// add new members.
	changed := true
	for changed {
		changed = false
		for comp := range included {
			c, exists := dr.components[comp]
			if !exists {
				continue
			}
			for _, dep := range c.DependsOn {
				if dep.Include != model.IncludeAlways {
					continue
				}
				if !included[dep.Component] {
					included[dep.Component] = true
					changed = true
				}
			}
		}
	}

	return included
}

// ResolveComponentSetAll is the legacy "pull every transitive
// dependency" behavior, retained for callers that need it (e.g. the
// context-aware CWD scope banner, which lists what a fresh full plan
// would touch).
func (dr *DependencyResolver) ResolveComponentSetAll(seedComponents map[string]bool) map[string]bool {
	included := make(map[string]bool)

	// Add all seed components
	for comp := range seedComponents {
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
