package model

// ExecutionProfileDocument is the standalone file format for a profile in a stack.
type ExecutionProfileDocument struct {
	APIVersion string               `yaml:"apiVersion" json:"apiVersion"`
	Kind       string               `yaml:"kind" json:"kind"`
	Metadata   Metadata             `yaml:"metadata" json:"metadata"`
	Spec       ExecutionProfileSpec `yaml:"spec" json:"spec"`
}

// ExecutionProfileSpec is the spec section of a standalone profile document.
type ExecutionProfileSpec struct {
	Description string                            `yaml:"description,omitempty" json:"description,omitempty"`
	Plan        ProfilePlan                       `yaml:"plan,omitempty" json:"plan,omitempty"`
	Controls    map[string]map[string]interface{} `yaml:"controls,omitempty" json:"controls,omitempty"`
}

// TriggerBindingDocument is the standalone file format for a trigger in a stack.
type TriggerBindingDocument struct {
	APIVersion string             `yaml:"apiVersion" json:"apiVersion"`
	Kind       string             `yaml:"kind" json:"kind"`
	Metadata   Metadata           `yaml:"metadata" json:"metadata"`
	Spec       TriggerBindingSpec `yaml:"spec" json:"spec"`
}

// TriggerBindingSpec is the spec section of a standalone trigger document.
type TriggerBindingSpec struct {
	On   TriggerOn      `yaml:"on" json:"on"`
	Plan TriggerPlanRef `yaml:"plan" json:"plan"`
}

// TriggerPlanRef references a profile by name.
type TriggerPlanRef struct {
	ProfileRef string `yaml:"profileRef" json:"profileRef"`
}

// StackOverridePolicyDocument is the standalone file format for an override policy.
type StackOverridePolicyDocument struct {
	APIVersion string                  `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                  `yaml:"kind" json:"kind"`
	Metadata   Metadata                `yaml:"metadata" json:"metadata"`
	Spec       StackOverridePolicySpec `yaml:"spec" json:"spec"`
}

// StackOverridePolicySpec defines what intent-level overrides are allowed.
type StackOverridePolicySpec struct {
	Default string              `yaml:"default" json:"default"`
	Allow   OverridePolicyAllow `yaml:"allow,omitempty" json:"allow,omitempty"`
	Deny    []string            `yaml:"deny,omitempty" json:"deny,omitempty"`
}

// OverridePolicyAllow defines what the intent is allowed to override.
type OverridePolicyAllow struct {
	Intent OverridePolicyIntent `yaml:"intent,omitempty" json:"intent,omitempty"`
}

// OverridePolicyIntent specifies allowed overrides at the intent level.
type OverridePolicyIntent struct {
	Environments OverridePolicyEnvironments       `yaml:"environments,omitempty" json:"environments,omitempty"`
	Profiles     map[string]OverridePolicyProfile `yaml:"profiles,omitempty" json:"profiles,omitempty"`
}

// OverridePolicyEnvironments specifies which environment defaults/policies may be set.
type OverridePolicyEnvironments struct {
	Defaults []string `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	Policies []string `yaml:"policies,omitempty" json:"policies,omitempty"`
}

// OverridePolicyProfile specifies allowed control overrides for a profile.
type OverridePolicyProfile struct {
	Controls map[string]map[string]OverridePolicyControl `yaml:"controls,omitempty" json:"controls,omitempty"`
}

// OverridePolicyControl specifies constraints on an individual control override.
type OverridePolicyControl struct {
	Type          string        `yaml:"type,omitempty" json:"type,omitempty"`
	AllowedValues []interface{} `yaml:"allowedValues,omitempty" json:"allowedValues,omitempty"`
}
