package model

import (
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Intent is the top-level CRD for declarative deployment
type Intent struct {
	APIVersion   string                 `yaml:"apiVersion" json:"apiVersion"`
	Kind         string                 `yaml:"kind" json:"kind"`
	Metadata     Metadata               `yaml:"metadata" json:"metadata"`
	Discovery    Discovery              `yaml:"discovery" json:"discovery"`
	Compositions CompositionConfig      `yaml:"compositions,omitempty" json:"compositions,omitempty"`
	Automation   AutomationConfig       `yaml:"automation,omitempty" json:"automation,omitempty"`
	Groups       map[string]Group       `yaml:"groups" json:"groups"`
	Environments map[string]Environment `yaml:"environments" json:"environments"`
	Components   []Component            `yaml:"components" json:"components"`
	Execution    IntentExecution        `yaml:"execution,omitempty" json:"execution,omitempty"`
	Env          map[string]string      `yaml:"env,omitempty" json:"env,omitempty"`
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
	Path       string                 `yaml:"path" json:"path"`
	Activation EnvironmentActivation  `yaml:"activation,omitempty" json:"activation,omitempty"`
	Selectors  EnvironmentSelectors   `yaml:"selectors" json:"selectors"`
	Defaults   map[string]interface{} `yaml:"defaults" json:"defaults"`
	Policies   map[string]interface{} `yaml:"policies" json:"policies"`
	Env        map[string]string      `yaml:"env,omitempty" json:"env,omitempty"`
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
	Env            map[string]string        `yaml:"env,omitempty" json:"env,omitempty"`
	ResolvedComposition       string        `yaml:"-" json:"-"`
	ResolvedCompositionSource string        `yaml:"-" json:"-"`
	SourcePath     string                   `yaml:"-" json:"-"`
}

// ComponentSubscribe declares which environments a component participates in.
type ComponentSubscribe struct {
	Environments []EnvironmentSubscription `yaml:"environments" json:"environments"`
}

// EnvironmentSubscription specifies an environment binding with optional profile selection.
type EnvironmentSubscription struct {
	Name    string            `yaml:"name" json:"name"`
	Profile string            `yaml:"profile,omitempty" json:"profile,omitempty"`
	Env     map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}

// UnmarshalYAML supports both string and object forms for environment subscriptions.
func (s *EnvironmentSubscription) UnmarshalYAML(value *yaml.Node) error {
	switch value.Kind {
	case yaml.ScalarNode:
		s.Name = value.Value
		return nil
	case yaml.MappingNode:
		type raw EnvironmentSubscription
		var r raw
		if err := value.Decode(&r); err != nil {
			return err
		}
		if r.Name == "" {
			return fmt.Errorf("environment subscription object requires name")
		}
		*s = EnvironmentSubscription(r)
		return nil
	default:
		return fmt.Errorf("environment subscription must be a string or object")
	}
}

// EnvironmentNames returns the list of environment names from subscriptions.
func (cs ComponentSubscribe) EnvironmentNames() []string {
	names := make([]string, len(cs.Environments))
	for i, env := range cs.Environments {
		names[i] = env.Name
	}
	return names
}

// FindSubscription returns the subscription for a given environment name, or nil.
func (cs ComponentSubscribe) FindSubscription(envName string) *EnvironmentSubscription {
	for i := range cs.Environments {
		if cs.Environments[i].Name == envName {
			return &cs.Environments[i]
		}
	}
	return nil
}

// UnmarshalJSON supports both string and object forms for environment subscriptions in JSON.
func (s *EnvironmentSubscription) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		s.Name = str
		return nil
	}
	type raw EnvironmentSubscription
	var r raw
	if err := json.Unmarshal(data, &r); err != nil {
		return fmt.Errorf("environment subscription must be a string or object: %w", err)
	}
	if r.Name == "" {
		return fmt.Errorf("environment subscription object requires name")
	}
	*s = EnvironmentSubscription(r)
	return nil
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
	Env            map[string]string    // root-level env from intent
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
	Env           map[string]string
	StepOverrides []Step
	Policies      map[string]interface{}
	DependsOn     []ResolvedDependency
	Enabled       bool
	ProfileRef    string
	ProfileName   string
	ProfileSource string
}

// ResolvedDependency is a dependency with resolved target component
type ResolvedDependency struct {
	ComponentName string
	Environment   string
	Scope         string
	Condition     string
}
