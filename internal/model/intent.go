package model

import "gopkg.in/yaml.v3"

// Intent is the top-level CRD for declarative deployment
type Intent struct {
	APIVersion   string                 `yaml:"apiVersion" json:"apiVersion"`
	Kind         string                 `yaml:"kind" json:"kind"`
	Metadata     Metadata               `yaml:"metadata" json:"metadata"`
	Discovery    Discovery              `yaml:"discovery" json:"discovery"`
	Stacks       []IntentStackRef       `yaml:"stacks,omitempty" json:"stacks,omitempty"`
	Compositions CompositionConfig      `yaml:"compositions,omitempty" json:"compositions,omitempty"`
	Groups       map[string]Group       `yaml:"groups" json:"groups"`
	Environments map[string]Environment `yaml:"environments" json:"environments"`
	Components   []Component            `yaml:"components" json:"components"`
	Execution    IntentExecution        `yaml:"execution,omitempty" json:"execution,omitempty"`
	Automation   IntentAutomation       `yaml:"automation,omitempty" json:"automation,omitempty"`
}

// IntentStackRef references a stack package by name and source.
type IntentStackRef struct {
	Name   string            `yaml:"name" json:"name"`
	Source CompositionSource `yaml:"source" json:"source"`
}

// IntentExecution holds optional execution-layer configuration in intent.yaml.
type IntentExecution struct {
	State    IntentExecutionState        `yaml:"state,omitempty" json:"state,omitempty"`
	Profiles map[string]ExecutionProfile `yaml:"profiles,omitempty" json:"profiles,omitempty"`
}

// ExecutionProfile defines a named set of controls per composition type.
type ExecutionProfile struct {
	Description    string                            `yaml:"description,omitempty" json:"description,omitempty"`
	CompositionRef string                            `yaml:"compositionRef,omitempty" json:"compositionRef,omitempty"`
	Plan           ProfilePlan                       `yaml:"plan,omitempty" json:"plan,omitempty"`
	Controls       map[string]map[string]interface{} `yaml:"controls,omitempty" json:"controls,omitempty"`
}

// ProfilePlan holds plan-generation settings for an execution profile.
type ProfilePlan struct {
	Scope string `yaml:"scope,omitempty" json:"scope,omitempty"`
}

// IntentAutomation holds the automation section of an intent.
type IntentAutomation struct {
	Triggers        []AutomationTrigger `yaml:"triggers,omitempty" json:"triggers,omitempty"`
	TriggerBindings []AutomationTrigger `yaml:"triggerBindings,omitempty" json:"triggerBindings,omitempty"`
}

// AutomationTrigger maps a CI event to an execution profile.
type AutomationTrigger struct {
	Name string      `yaml:"name" json:"name"`
	On   TriggerOn   `yaml:"on" json:"on"`
	Plan TriggerPlan `yaml:"plan" json:"plan"`
}

// TriggerOn defines the event conditions that activate a trigger.
type TriggerOn struct {
	Provider string   `yaml:"provider,omitempty" json:"provider,omitempty"`
	Event    string   `yaml:"event" json:"event"`
	Actions  []string `yaml:"actions,omitempty" json:"actions,omitempty"`
	Branches []string `yaml:"branches,omitempty" json:"branches,omitempty"`
	Tags     []string `yaml:"tags,omitempty" json:"tags,omitempty"`
}

// TriggerPlan specifies which execution profile a trigger selects.
type TriggerPlan struct {
	Profile string `yaml:"profile,omitempty" json:"profile,omitempty"`
	Scope   string `yaml:"scope,omitempty" json:"scope,omitempty"`
	Base    string `yaml:"base,omitempty" json:"base,omitempty"`
	Head    string `yaml:"head,omitempty" json:"head,omitempty"`
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
	Execution EnvironmentExecution   `yaml:"execution,omitempty" json:"execution,omitempty"`
}

// EnvironmentExecution selects an execution profile and optional control overrides.
type EnvironmentExecution struct {
	Profile          string                            `yaml:"profile,omitempty" json:"profile,omitempty"`
	TriggerBindings  []string                          `yaml:"triggerBindings,omitempty" json:"triggerBindings,omitempty"`
	Profiles         map[string]string                 `yaml:"profiles,omitempty" json:"profiles,omitempty"`
	ControlOverrides map[string]map[string]interface{} `yaml:"controlOverrides,omitempty" json:"controlOverrides,omitempty"`
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
	ControlOverrides map[string]interface{} `yaml:"controlOverrides,omitempty" json:"controlOverrides,omitempty"`
	ResolvedComposition       string        `yaml:"-" json:"-"`
	ResolvedCompositionSource string        `yaml:"-" json:"-"`
	SourcePath     string                   `yaml:"-" json:"-"`
}

// SubscriptionEntry is a rich subscription binding environments to optional profile/triggerBinding overrides.
type SubscriptionEntry struct {
	Environments    []string `yaml:"environments" json:"environments"`
	Profile         string   `yaml:"profile,omitempty" json:"profile,omitempty"`
	TriggerBindings []string `yaml:"triggerBindings,omitempty" json:"triggerBindings,omitempty"`
}

// ComponentSubscribe declares which environments a component participates in.
// Supports two YAML formats:
//   - Old: {environments: [dev, staging]}
//   - New: [{environments: [dev], profile: dry-run}, {environments: [staging]}]
type ComponentSubscribe struct {
	Environments []string            `yaml:"-" json:"environments,omitempty"`
	Entries      []SubscriptionEntry `yaml:"-" json:"entries,omitempty"`
}

func (cs *ComponentSubscribe) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind == yaml.SequenceNode {
		var entries []SubscriptionEntry
		if err := value.Decode(&entries); err != nil {
			return err
		}
		cs.Entries = entries
		envSet := make(map[string]struct{})
		for _, e := range entries {
			for _, env := range e.Environments {
				envSet[env] = struct{}{}
			}
		}
		cs.Environments = make([]string, 0, len(envSet))
		for env := range envSet {
			cs.Environments = append(cs.Environments, env)
		}
		return nil
	}

	var old struct {
		Environments []string `yaml:"environments"`
	}
	if err := value.Decode(&old); err != nil {
		return err
	}
	cs.Environments = old.Environments
	if len(old.Environments) > 0 {
		cs.Entries = []SubscriptionEntry{{Environments: old.Environments}}
	}
	return nil
}

func (cs ComponentSubscribe) MarshalYAML() (interface{}, error) {
	hasOverrides := false
	for _, e := range cs.Entries {
		if e.Profile != "" || len(e.TriggerBindings) > 0 {
			hasOverrides = true
			break
		}
	}
	if hasOverrides {
		return cs.Entries, nil
	}
	return struct {
		Environments []string `yaml:"environments"`
	}{Environments: cs.Environments}, nil
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
	Execution      IntentExecution
	Automation     IntentAutomation
	Groups         map[string]Group
	Environments   map[string]Environment
	Components     map[string]Component
	ComponentIndex map[string]Component
	OverridePolicy *StackOverridePolicySpec
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
	Controls      map[string]interface{}
	ResolvedProfile        string
	ResolvedTriggerBinding string
}

// ResolvedDependency is a dependency with resolved target component
type ResolvedDependency struct {
	ComponentName string
	Environment   string
	Scope         string
	Condition     string
}
