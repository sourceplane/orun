package model

// AutomationConfig holds trigger binding declarations for CI/event-driven planning.
type AutomationConfig struct {
	TriggerBindings map[string]TriggerBinding `yaml:"triggerBindings,omitempty" json:"triggerBindings,omitempty"`
}

// TriggerBinding maps a provider event to environment activation and plan scope.
type TriggerBinding struct {
	Description string             `yaml:"description,omitempty" json:"description,omitempty"`
	On          TriggerMatch       `yaml:"on" json:"on"`
	Plan        TriggerPlanOptions `yaml:"plan,omitempty" json:"plan,omitempty"`
}

// TriggerMatch defines the event filter criteria for a trigger binding.
type TriggerMatch struct {
	Provider     string   `yaml:"provider" json:"provider"`
	Event        string   `yaml:"event" json:"event"`
	Actions      []string `yaml:"actions,omitempty" json:"actions,omitempty"`
	Branches     []string `yaml:"branches,omitempty" json:"branches,omitempty"`
	BaseBranches []string `yaml:"baseBranches,omitempty" json:"baseBranches,omitempty"`
	Tags         []string `yaml:"tags,omitempty" json:"tags,omitempty"`
}

// TriggerPlanOptions controls how the plan is scoped when a trigger fires.
type TriggerPlanOptions struct {
	// Scope is full (the default), changed, or selected. A selected plan is
	// exactly the components and environments named by Components and
	// Environments (orun-manual-triggers §3).
	Scope string `yaml:"scope,omitempty" json:"scope,omitempty"`
	Base  string `yaml:"base,omitempty" json:"base,omitempty"`
	Head  string `yaml:"head,omitempty" json:"head,omitempty"`
	// Components and Environments are dot paths into the raw event payload
	// (e.g. inputs.component) naming a selected plan's components and
	// environments: a comma-separated string or a list of strings.
	Components   string `yaml:"components,omitempty" json:"components,omitempty"`
	Environments string `yaml:"environments,omitempty" json:"environments,omitempty"`
	// AutoPush set to false skips the plan-time catalog auto-push for a plan
	// this binding resolved. Unset leaves execution.state.autopushCatalog in
	// charge.
	AutoPush *bool `yaml:"autopush,omitempty" json:"autopush,omitempty"`
}

// PlanScopeSelected is the plan scope whose selection comes from the trigger
// itself: the event's named components in its named environments.
const PlanScopeSelected = "selected"

// EnvironmentActivation specifies which triggers activate an environment.
type EnvironmentActivation struct {
	TriggerRefs []string `yaml:"triggerRefs,omitempty" json:"triggerRefs,omitempty"`
}

// NormalizedEvent is the provider-neutral representation of a CI/webhook event.
type NormalizedEvent struct {
	Provider   string
	Event      string
	Action     string
	Ref        string
	RefType    string // branch, tag, pull_request, unknown
	Branch     string
	Tag        string
	BaseBranch string
	BaseSHA    string
	HeadSHA    string
	Repository string
	Actor      string
	Raw        map[string]any
}

// TriggerContext carries the trigger mode and data into the planner.
type TriggerContext struct {
	Mode        string // none, named-trigger, event-file
	TriggerName string
	Event       *NormalizedEvent
}

// TriggerResolution is the result of resolving triggers against environments.
type TriggerResolution struct {
	MatchedTriggerNames []string
	ActiveEnvironments  []string
	PlanScope           string
	Base                string
	Head                string
	// SelectedComponents and SelectedEnvironments are a selected plan's
	// selection, resolved from the event. Empty for a named trigger, whose
	// selection comes from --component and --env.
	SelectedComponents   []string
	SelectedEnvironments []string
	// AutoPushDisabled is true when a matched binding sets plan.autopush: false.
	AutoPushDisabled bool
}
