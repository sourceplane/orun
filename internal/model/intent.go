package model

// Intent is the top-level CRD for declarative deployment
type Intent struct {
	APIVersion   string                 `yaml:"apiVersion" json:"apiVersion"`
	Kind         string                 `yaml:"kind" json:"kind"`
	Metadata     Metadata               `yaml:"metadata" json:"metadata"`
	Discovery    Discovery              `yaml:"discovery" json:"discovery"`
	Compositions CompositionConfig      `yaml:"compositions,omitempty" json:"compositions,omitempty"`
	Groups       map[string]Group       `yaml:"groups" json:"groups"`
	Environments map[string]Environment `yaml:"environments" json:"environments"`
	Components   []Component            `yaml:"components" json:"components"`
	Execution    IntentExecution        `yaml:"execution,omitempty" json:"execution,omitempty"`
}

// IntentExecution holds optional execution-layer configuration in intent.yaml.
type IntentExecution struct {
	State IntentExecutionState `yaml:"state,omitempty" json:"state,omitempty"`
}

// IntentExecutionState configures where execution state is stored.
type IntentExecutionState struct {
	// Mode is "local" (default) or "remote".
	Mode       string `yaml:"mode,omitempty" json:"mode,omitempty"`
	// BackendURL is the URL of the orun-backend instance for remote mode.
	BackendURL string `yaml:"backendUrl,omitempty" json:"backendUrl,omitempty"`
}

// Discovery limits repository scanning for external component manifests.
type Discovery struct {
	Roots []string `yaml:"roots" json:"roots"`
}

// Metadata holds standard object metadata
type Metadata struct {
	Name        string `yaml:"name" json:"name"`
	Description string `yaml:"description" json:"description"`
	Namespace   string `yaml:"namespace" json:"namespace"`
}

// Group defines ownership and policy constraints
type Group struct {
	Path     string                 `yaml:"path" json:"path"`
	Policies map[string]interface{} `yaml:"policies" json:"policies"`
	Defaults map[string]interface{} `yaml:"defaults" json:"defaults"`
}

// Environment defines environment runtime contexts
type Environment struct {
	Path      string                 `yaml:"path" json:"path"`
	Selectors EnvironmentSelectors   `yaml:"selectors" json:"selectors"`
	Defaults  map[string]interface{} `yaml:"defaults" json:"defaults"`
	Policies  map[string]interface{} `yaml:"policies" json:"policies"`
}

// EnvironmentSelectors specifies which components apply to an environment
type EnvironmentSelectors struct {
	Components []string `yaml:"components" json:"components"`
	Domains    []string `yaml:"domains" json:"domains"`
}

// Component is execution-agnostic declaration
type Component struct {
	Name           string                   `yaml:"name" json:"name"`
	Type           string                   `yaml:"type" json:"type"`
	Domain         string                   `yaml:"domain" json:"domain"`
	Enabled        bool                     `yaml:"enabled" json:"enabled"`
	Path           string                   `yaml:"path" json:"path"`
	Subscribe      ComponentSubscribe       `yaml:"subscribe" json:"subscribe"`
	CompositionRef *ComponentCompositionRef `yaml:"compositionRef,omitempty" json:"compositionRef,omitempty"`
	Inputs         map[string]interface{}   `yaml:"inputs" json:"inputs"`
	Overrides      ComponentOverrides       `yaml:"overrides" json:"overrides"`
	Labels         map[string]string        `yaml:"labels" json:"labels"`
	DependsOn      []Dependency             `yaml:"dependsOn" json:"dependsOn"`
	ResolvedComposition       string        `yaml:"-" json:"-"`
	ResolvedCompositionSource string        `yaml:"-" json:"-"`
	SourcePath     string                   `yaml:"-" json:"-"`
}

// ComponentSubscribe declares which environments a component participates in.
type ComponentSubscribe struct {
	Environments []string `yaml:"environments" json:"environments"`
}

// ComponentOverrides defines component-specific planner overrides.
type ComponentOverrides struct {
	Steps []Step `yaml:"steps" json:"steps"`
}

// Dependency specifies inter-component execution constraints
type Dependency struct {
	Component   string `yaml:"component" json:"component"`
	Environment string `yaml:"environment" json:"environment"`
	Scope       string `yaml:"scope" json:"scope"`         // same-environment, cross-environment
	Condition   string `yaml:"condition" json:"condition"` // success, always, failure
}

// NormalizedIntent is the canonical internal representation
type NormalizedIntent struct {
	Metadata       Metadata
	Groups         map[string]Group
	Environments   map[string]Environment
	Components     map[string]Component
	ComponentIndex map[string]Component // for fast lookup
}

// ComponentInstance is the expanded form of Component for a specific environment
type ComponentInstance struct {
	ComponentName string
	Environment   string
	Type          string
	ResolvedComposition       string
	ResolvedCompositionSource string
	Domain        string
	Path          string
	SourcePath    string
	Labels        map[string]string
	Inputs        map[string]interface{}
	StepOverrides []Step
	Policies      map[string]interface{}
	DependsOn     []ResolvedDependency
	Enabled       bool
}

// ResolvedDependency is a dependency with resolved target component
type ResolvedDependency struct {
	ComponentName string
	Environment   string
	Scope         string
	Condition     string
}
