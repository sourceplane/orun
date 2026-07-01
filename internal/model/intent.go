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
	Extends      []ExtendsRef           `yaml:"extends,omitempty" json:"extends,omitempty"`
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
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
	// BackendURL is the URL of the orun-backend instance for remote mode.
	BackendURL string `yaml:"backendUrl,omitempty" json:"backendUrl,omitempty"`
	// Workspace is the declared, enforced tenancy for this repo: a Workspace ID
	// (ws_…), a Workspace slug, or a legacy org_… id. It is the committed,
	// reviewable home for the tenancy claim
	// sent on every remote op (the OIDC exchange body and the API-key request), so
	// enforcement does not depend on a per-invocation --workspace flag that one CI
	// job of twenty can forget. This is the leading spelling of the same field as
	// Org below; when both are present Workspace wins (orun-cloud saas-workspaces
	// A4). A Workspace is any org in the account — the declared value is never the
	// parent Account. Precedence: --workspace/--org → ORUN_WORKSPACE/ORUN_ORG →
	// execution.state.workspace/org → cached link (specs/oidc-ci-tenancy §4.1).
	Workspace string `yaml:"workspace,omitempty" json:"workspace,omitempty"`
	// Org is the legacy spelling of Workspace, retained as an accepted alias so
	// configs committed before the rename keep working unchanged (read either,
	// prefer workspace — saas-workspaces A4).
	Org string `yaml:"org,omitempty" json:"org,omitempty"`
	// Project is an advanced override for the project (repo) scope. The default
	// is project = repo, derived from the git remote server-side; declare this
	// only for a rename or a monorepo split (specs/oidc-ci-tenancy §4.3).
	Project string `yaml:"project,omitempty" json:"project,omitempty"`
	// RequireOrg turns on strict mode: a non-interactive remote op with no
	// resolvable org fails fast (pointing at execution.state.org) instead of
	// silently exchanging an empty claim and landing in an ambiguous scope. It is
	// implied true whenever Org is set (specs/oidc-ci-tenancy §4.1, decision D2).
	RequireOrg bool `yaml:"requireOrg,omitempty" json:"requireOrg,omitempty"`
	// AutopushCatalog, when true, makes `orun plan` best-effort publish the
	// resolved catalog to the configured backend after a successful plan on the
	// default branch (clean tree) — keeping the org-global catalog head fresh
	// without an explicit `--push-catalog`. Off by default; never fails the plan
	// (warn-only, and silent unless ORUN_VERBOSE). The spec calls this
	// `cloud.catalog.autopush` (specs/orun-cloud/design.md §5).
	AutopushCatalog bool `yaml:"autopushCatalog,omitempty" json:"autopushCatalog,omitempty"`
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
	Path              string                            `yaml:"path" json:"path"`
	Policies          map[string]interface{}            `yaml:"policies" json:"policies"`
	ParameterDefaults map[string]map[string]interface{} `yaml:"parameterDefaults" json:"parameterDefaults"`
}

// Environment defines environment runtime contexts
type Environment struct {
	Path              string                            `yaml:"path" json:"path"`
	Activation        EnvironmentActivation             `yaml:"activation,omitempty" json:"activation,omitempty"`
	Promotion         EnvironmentPromotion              `yaml:"promotion,omitempty" json:"promotion,omitempty"`
	Selectors         EnvironmentSelectors              `yaml:"selectors" json:"selectors"`
	ParameterDefaults map[string]map[string]interface{} `yaml:"parameterDefaults" json:"parameterDefaults"`
	Policies          map[string]interface{}            `yaml:"policies" json:"policies"`
	Env               map[string]string                 `yaml:"env,omitempty" json:"env,omitempty"`
	// DependencyMode controls how component dependsOn edges are emitted
	// for components subscribing to this environment. One of "enforced"
	// (default), "advisory", "disabled". See concepts/dependency-rules.md.
	DependencyMode string `yaml:"dependencyMode,omitempty" json:"dependencyMode,omitempty"`
}

// Dependency-mode constants. Default is DependencyModeEnforced.
const (
	DependencyModeEnforced = "enforced"
	DependencyModeAdvisory = "advisory"
	DependencyModeDisabled = "disabled"
)

// IsValidDependencyMode reports whether mode is one of the supported values
// (empty string is also accepted: it means "use the parent default").
func IsValidDependencyMode(mode string) bool {
	switch mode {
	case "", DependencyModeEnforced, DependencyModeAdvisory, DependencyModeDisabled:
		return true
	default:
		return false
	}
}

// EnvironmentPromotion declares ordering/gating relationships between environments.
type EnvironmentPromotion struct {
	DependsOn []PromotionDependency `yaml:"dependsOn,omitempty" json:"dependsOn,omitempty"`
}

// PromotionDependency specifies a dependency on another environment for promotion.
type PromotionDependency struct {
	Environment string         `yaml:"environment" json:"environment"`
	Strategy    string         `yaml:"strategy,omitempty" json:"strategy,omitempty"`
	Condition   string         `yaml:"condition,omitempty" json:"condition,omitempty"`
	Satisfy     string         `yaml:"satisfy,omitempty" json:"satisfy,omitempty"`
	Match       PromotionMatch `yaml:"match,omitempty" json:"match,omitempty"`
}

// PromotionMatch specifies how to match evidence for cross-plan promotion gates.
type PromotionMatch struct {
	Revision string `yaml:"revision,omitempty" json:"revision,omitempty"`
}

// EnvironmentSelectors specifies which components apply to an environment
type EnvironmentSelectors struct {
	Components []string `yaml:"components" json:"components"`
	Domains    []string `yaml:"domains" json:"domains"`
}

// ValidWatchSections lists valid values for ComponentChange.Watches.
var ValidWatchSections = []string{"automation", "compositions", "discovery", "env", "environments", "execution", "groups"}

// ComponentChange declares which intent change signals affect a component.
type ComponentChange struct {
	Watches []string `yaml:"watches,omitempty" json:"watches,omitempty"`
}

// Component is execution-agnostic declaration
type Component struct {
	Name                      string                   `yaml:"name" json:"name"`
	Type                      string                   `yaml:"type" json:"type"`
	Domain                    string                   `yaml:"domain" json:"domain"`
	Enabled                   bool                     `yaml:"enabled" json:"enabled"`
	Path                      string                   `yaml:"path" json:"path"`
	Subscribe                 ComponentSubscribe       `yaml:"subscribe" json:"subscribe"`
	CompositionRef            *ComponentCompositionRef `yaml:"compositionRef,omitempty" json:"compositionRef,omitempty"`
	Parameters                map[string]interface{}   `yaml:"parameters" json:"parameters"`
	Overrides                 ComponentOverrides       `yaml:"overrides" json:"overrides"`
	Labels                    map[string]string        `yaml:"labels" json:"labels"`
	DependsOn                 []Dependency             `yaml:"dependsOn" json:"dependsOn"`
	Env                       map[string]string        `yaml:"env,omitempty" json:"env,omitempty"`
	Change                    ComponentChange          `yaml:"change,omitempty" json:"change,omitempty"`
	ResolvedComposition       string                   `yaml:"-" json:"-"`
	ResolvedCompositionSource string                   `yaml:"-" json:"-"`
	SourcePath                string                   `yaml:"-" json:"-"`
}

// ComponentSubscribe declares which environments a component participates in.
type ComponentSubscribe struct {
	Environments []EnvironmentSubscription `yaml:"environments" json:"environments"`
}

// EnvironmentSubscription specifies an environment binding with optional profile selection.
type EnvironmentSubscription struct {
	Name         string        `yaml:"name" json:"name"`
	Profile      string        `yaml:"profile,omitempty" json:"profile,omitempty"`
	ProfileRules []ProfileRule `yaml:"profileRules,omitempty" json:"profileRules,omitempty"`
	// DependencyMode optionally overrides the environment's dependency mode
	// for this single component. When unset, the environment default applies.
	DependencyMode string `yaml:"dependencyMode,omitempty" json:"dependencyMode,omitempty"`
	// DependencyRules conditionally override DependencyMode based on the
	// matched triggerRef. First match wins; if nothing matches, the
	// subscription/environment DependencyMode (or default) is used.
	DependencyRules []DependencyRule       `yaml:"dependencyRules,omitempty" json:"dependencyRules,omitempty"`
	Env             map[string]string      `yaml:"env,omitempty" json:"env,omitempty"`
	Parameters      map[string]interface{} `yaml:"parameters,omitempty" json:"parameters,omitempty"`
}

// ProfileRule is a conditional override that selects a different execution profile
// when the specified condition matches. Rules are evaluated in order (first-match-wins).
type ProfileRule struct {
	Profile string          `yaml:"profile" json:"profile"`
	When    ProfileRuleWhen `yaml:"when" json:"when"`
}

// ProfileRuleWhen defines the condition for a profile rule.
type ProfileRuleWhen struct {
	TriggerRef string `yaml:"triggerRef" json:"triggerRef"`
}

// DependencyRule is a conditional override that selects a different
// dependency mode when a particular trigger fires. First-match-wins.
type DependencyRule struct {
	Mode string             `yaml:"mode" json:"mode"`
	When DependencyRuleWhen `yaml:"when" json:"when"`
}

// DependencyRuleWhen defines the condition for a dependency rule.
type DependencyRuleWhen struct {
	TriggerRef string `yaml:"triggerRef" json:"triggerRef"`
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
	// Include controls plan selection behavior (orthogonal to ordering).
	//   "if-selected" (default): order only when both ends are already in
	//                            the plan; do NOT pull the dependency in.
	//   "always":                pull the dependency into the plan when
	//                            the dependent is selected, then order.
	Include string `yaml:"include,omitempty" json:"include,omitempty"`
	// Reason is a free-form note explaining why include=always was chosen.
	// Surfaced in plan output for auditability; never affects behavior.
	Reason string `yaml:"reason,omitempty" json:"reason,omitempty"`
}

// Dependency include modes.
const (
	IncludeIfSelected = "if-selected"
	IncludeAlways     = "always"
)

// IsValidInclude reports whether s is a recognized include mode.
func IsValidInclude(s string) bool {
	switch s {
	case IncludeIfSelected, IncludeAlways:
		return true
	default:
		return false
	}
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
	ComponentName             string
	Environment               string
	Type                      string
	ResolvedComposition       string
	ResolvedCompositionSource string
	Domain                    string
	Path                      string
	SourcePath                string
	Labels                    map[string]string
	Parameters                map[string]interface{}
	Env                       map[string]string
	StepOverrides             []Step
	Policies                  map[string]interface{}
	DependsOn                 []ResolvedDependency
	Enabled                   bool
	ProfileRef                string
	ProfileName               string
	ProfileSource             string
	ProfileRuleTriggerRef     string
	// DependencyMode is the resolved enforcement policy for this instance's
	// dependsOn edges (enforced | advisory | disabled). Default enforced.
	DependencyMode string
	// DependencySource records which layer set DependencyMode:
	// "default", "environment", "subscription", or "subscription-rule".
	DependencySource string
	// DependencyRuleTriggerRef records which trigger ref matched the rule
	// (only when DependencySource == "subscription-rule").
	DependencyRuleTriggerRef string
}

// ResolvedDependency is a dependency with resolved target component
type ResolvedDependency struct {
	ComponentName string
	Environment   string
	Scope         string
	Condition     string
	// Include carries the resolved selection policy: "if-selected" or
	// "always". Always populated (never empty) after normalization so
	// downstream code can switch on it without re-defaulting.
	Include string
	// Reason mirrors Dependency.Reason for audit trails in plan output.
	Reason string
}
