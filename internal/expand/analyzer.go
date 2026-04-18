package expand

import (
	"sort"

	"github.com/sourceplane/arx/internal/model"
)

// ComponentAnalyzer provides analysis of components and their resolved properties
type ComponentAnalyzer struct {
	expander  *Expander
	instances map[string][]*model.ComponentInstance
}

// NewComponentAnalyzer creates a new component analyzer
func NewComponentAnalyzer(normalized *model.NormalizedIntent) *ComponentAnalyzer {
	expander := NewExpander(normalized)
	return &ComponentAnalyzer{
		expander: expander,
	}
}

// AnalyzeAll expands all components and returns merged instances
func (ca *ComponentAnalyzer) AnalyzeAll() (map[string][]*model.ComponentInstance, error) {
	if ca.instances != nil {
		return ca.instances, nil
	}

	instances, err := ca.expander.Expand()
	if err != nil {
		return nil, err
	}

	ca.instances = instances
	return ca.instances, nil
}

// GetComponent returns merged component data for a specific component across all environments
type ComponentMerged struct {
	Name         string
	Type         string
	Domain       string
	Enabled      bool
	SourcePath   string
	Instances    []*model.ComponentInstance
	Dependencies []string
}

// GetComponentByName returns merged component info for a single component
func (ca *ComponentAnalyzer) GetComponentByName(compName string) (*ComponentMerged, error) {
	instances, err := ca.AnalyzeAll()
	if err != nil {
		return nil, err
	}

	comp := &ComponentMerged{
		Name:         compName,
		Instances:    make([]*model.ComponentInstance, 0),
		Dependencies: make([]string, 0),
	}

	depSet := make(map[string]bool)

	// Collect all instances of this component across environments
	for _, envInstances := range instances {
		for _, inst := range envInstances {
			if inst.ComponentName == compName {
				// Set component-level info from first instance
				if comp.Type == "" {
					comp.Type = inst.Type
					comp.Domain = inst.Domain
					comp.Enabled = inst.Enabled
					comp.SourcePath = inst.SourcePath
				}

				for _, dep := range inst.DependsOn {
					depSet[dep.ComponentName] = true
				}
				comp.Instances = append(comp.Instances, inst)
			}
		}
	}

	if len(depSet) > 0 {
		comp.Dependencies = make([]string, 0, len(depSet))
		for dep := range depSet {
			comp.Dependencies = append(comp.Dependencies, dep)
		}
		sort.Strings(comp.Dependencies)
	}

	return comp, nil
}

// ListAll lists all components with their merged properties
func (ca *ComponentAnalyzer) ListAll() ([]*ComponentMerged, error) {
	instances, err := ca.AnalyzeAll()
	if err != nil {
		return nil, err
	}

	byName := make(map[string]*ComponentMerged)

	// Build merged result in a single pass
	for _, envInstances := range instances {
		for _, inst := range envInstances {
			comp, exists := byName[inst.ComponentName]
			if !exists {
				comp = &ComponentMerged{
					Name:         inst.ComponentName,
					Type:         inst.Type,
					Domain:       inst.Domain,
					Enabled:      inst.Enabled,
					SourcePath:   inst.SourcePath,
					Instances:    make([]*model.ComponentInstance, 0),
					Dependencies: make([]string, 0),
				}
				byName[inst.ComponentName] = comp
			}

			comp.Instances = append(comp.Instances, inst)
			for _, dep := range inst.DependsOn {
				if !contains(comp.Dependencies, dep.ComponentName) {
					comp.Dependencies = append(comp.Dependencies, dep.ComponentName)
				}
			}
		}
	}

	result := make([]*ComponentMerged, 0, len(byName))
	for _, comp := range byName {
		sort.Strings(comp.Dependencies)
		result = append(result, comp)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result, nil
}

func contains(items []string, target string) bool {
	for _, item := range items {
		if item == target {
			return true
		}
	}
	return false
}
