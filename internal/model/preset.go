package model

// ExtendsRef references a preset from a composition source.
type ExtendsRef struct {
	Source string `yaml:"source" json:"source"`
	Preset string `yaml:"preset" json:"preset"`
}

// IntentPreset is the CRD for reusable intent scaffolding published by Stacks.
type IntentPreset struct {
	APIVersion string           `yaml:"apiVersion" json:"apiVersion"`
	Kind       string           `yaml:"kind" json:"kind"`
	Metadata   Metadata         `yaml:"metadata" json:"metadata"`
	Spec       IntentPresetSpec `yaml:"spec" json:"spec"`
}

// IntentPresetSpec holds the preset's declarative fields.
type IntentPresetSpec struct {
	Env          map[string]string      `yaml:"env,omitempty" json:"env,omitempty"`
	Discovery    Discovery              `yaml:"discovery,omitempty" json:"discovery,omitempty"`
	Automation   AutomationConfig       `yaml:"automation,omitempty" json:"automation,omitempty"`
	Environments map[string]Environment `yaml:"environments,omitempty" json:"environments,omitempty"`
	Groups       map[string]Group       `yaml:"groups,omitempty" json:"groups,omitempty"`
}

// PresetProvenance tracks which preset contributed a field for explain output.
type PresetProvenance struct {
	Source string
	Preset string
}

// StackIntentPreset references a preset file within a Stack package.
type StackIntentPreset struct {
	Name string `yaml:"name" json:"name"`
	Path string `yaml:"path" json:"path"`
}
