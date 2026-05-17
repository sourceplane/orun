package model

// ComponentManifest is the CRD representation stored in component.yaml files.
type ComponentManifest struct {
	APIVersion string    `yaml:"apiVersion" json:"apiVersion"`
	Kind       string    `yaml:"kind" json:"kind"`
	Metadata   Metadata  `yaml:"metadata" json:"metadata"`
	Spec       Component `yaml:"spec" json:"spec"`
}

// ComponentTree captures the discovered component graph for cache reuse.
type ComponentTree struct {
	APIVersion string                   `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                   `yaml:"kind" json:"kind"`
	Metadata   ComponentTreeMetadata    `yaml:"metadata" json:"metadata"`
	Discovery  ComponentTreeDiscovery   `yaml:"discovery" json:"discovery"`
	Components []ComponentTreeComponent `yaml:"components" json:"components"`
}

// ComponentTreeMetadata describes the cache document itself.
type ComponentTreeMetadata struct {
	Intent      string `yaml:"intent" json:"intent"`
	GeneratedAt string `yaml:"generatedAt" json:"generatedAt"`
}

// ComponentTreeDiscovery records discovery settings used to build the tree.
type ComponentTreeDiscovery struct {
	Roots []string `yaml:"roots" json:"roots"`
}

// ComponentTreeComponent stores a single component entry in the discovery cache.
type ComponentTreeComponent struct {
	Name         string                 `yaml:"name" json:"name"`
	Type         string                 `yaml:"type" json:"type"`
	Domain       string                 `yaml:"domain,omitempty" json:"domain,omitempty"`
	Enabled      bool                   `yaml:"enabled" json:"enabled"`
	Path         string                 `yaml:"path,omitempty" json:"path,omitempty"`
	Subscribe    ComponentSubscribe     `yaml:"subscribe,omitempty" json:"subscribe,omitempty"`
	Parameters   map[string]interface{} `yaml:"parameters,omitempty" json:"parameters,omitempty"`
	Overrides    ComponentOverrides     `yaml:"overrides,omitempty" json:"overrides,omitempty"`
	Labels       map[string]string      `yaml:"labels,omitempty" json:"labels,omitempty"`
	DependsOn    []Dependency           `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
	Source       string                 `yaml:"source" json:"source"`
	SourcePath   string                 `yaml:"sourcePath,omitempty" json:"sourcePath,omitempty"`
	FileSize     int64                  `yaml:"fileSize,omitempty" json:"fileSize,omitempty"`
	FileModTime  string                 `yaml:"fileModTime,omitempty" json:"fileModTime,omitempty"`
}

// ToComponent converts a cache entry back into the internal component model.
func (entry ComponentTreeComponent) ToComponent() Component {
	return Component{
		Name:       entry.Name,
		Type:       entry.Type,
		Domain:     entry.Domain,
		Enabled:    entry.Enabled,
		Path:       entry.Path,
		Subscribe:  entry.Subscribe,
		Parameters: entry.Parameters,
		Overrides:  entry.Overrides,
		Labels:     entry.Labels,
		DependsOn:  entry.DependsOn,
		SourcePath: entry.SourcePath,
	}
}

// FromComponent creates a cache entry from the internal component model.
func FromComponent(component Component, source string) ComponentTreeComponent {
	return ComponentTreeComponent{
		Name:       component.Name,
		Type:       component.Type,
		Domain:     component.Domain,
		Enabled:    component.Enabled,
		Path:       component.Path,
		Subscribe:  component.Subscribe,
		Parameters: component.Parameters,
		Overrides:  component.Overrides,
		Labels:     component.Labels,
		DependsOn:  component.DependsOn,
		Source:     source,
		SourcePath: component.SourcePath,
	}
}