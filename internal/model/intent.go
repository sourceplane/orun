package model

import (
	"encoding/json"
	"fmt"
	"strings"

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
	// Work names where the repository declares its work: the epics tree and
	// the task contracts (orun-cloud saas-work-gitops WG-1). Absent means
	// today's layout; see IntentWork.
	Work *IntentWork       `yaml:"work,omitempty" json:"work,omitempty"`
	Env  map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	// SecretEnv maps env var names to secret:// references (never values —
	// the planner rejects literals; specs/orun-secrets/data-model.md §2.1).
	SecretEnv map[string]string `yaml:"secretEnv,omitempty" json:"secretEnv,omitempty"`
}

// IntentExecution holds optional execution-layer configuration in intent.yaml.
// The engine pin of orun-workflows-v2 §6 is gone (orun-workflows-v3 design
// §11/§12): `workflow:` steps run in-process, so the engine's digest is the
// orun binary's digest and a declared pin is a load-time error.
type IntentExecution struct {
	State IntentExecutionState `yaml:"state,omitempty" json:"state,omitempty"`
	// Defaults are run-time bounds every job inherits unless it sets its own
	// (specs/orun-runner-efficiency §5.7).
	Defaults *IntentExecutionDefaults `yaml:"defaults,omitempty" json:"defaults,omitempty"`
	// Lanes groups plan jobs into the units a runner takes. Nil (the
	// default) leaves every job its own lane and the plan byte-identical to
	// a lane-unaware one (specs/orun-lanes §3.1, §4.6).
	Lanes *IntentLanes `yaml:"lanes,omitempty" json:"lanes,omitempty"`
}

// IntentExecutionDefaults is execution.defaults in intent.yaml.
type IntentExecutionDefaults struct {
	// Timeout bounds every step whose job and step set none (a Go duration,
	// e.g. 45m). The runner's own default is 60m: no step holds a runner for
	// the CI limit.
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty"`
}

// DefaultStepTimeout bounds a step when neither it, its job, nor
// execution.defaults.timeout does.
const DefaultStepTimeout = "60m"

// Lane modes (specs/orun-lanes §3).
const (
	LaneModeJob       = "job"
	LaneModeComponent = "component"
	LaneModeEnv       = "env"
	LaneModeAuto      = "auto"
	// LaneIsolated is the component annotation that keeps a component's jobs
	// on runners of their own; not a mode.
	LaneIsolated = "isolated"
	// Cycle policies between lanes (specs/orun-lanes §7).
	LaneOnCycleSplit  = "split"
	LaneOnCycleReject = "reject"
	// Defaults applied when a field is unset.
	DefaultLaneMaxLanes    = 16
	DefaultLaneConcurrency = 4
)

// LaneConcurrency is execution.lanes.concurrency, or a group's: the jobs in
// flight inside one lane. A count, or `auto` (LaneConcurrencyAuto) for the
// runner's core count, which `orun run --lane` resolves on the machine that
// runs the lane (specs/orun-runner-efficiency §5.2). Zero is unset.
type LaneConcurrency int

// LaneConcurrencyAuto is `concurrency: auto`. Only the word maps to it: a
// negative count written in the intent stays what it is and fails
// validation, so no number a person could type is mistaken for auto.
const LaneConcurrencyAuto LaneConcurrency = -1 << 31

// IsAuto reports whether the concurrency is sized to the runner.
func (c LaneConcurrency) IsAuto() bool { return c == LaneConcurrencyAuto }

func (c LaneConcurrency) MarshalYAML() (any, error) {
	if c.IsAuto() {
		return "auto", nil
	}
	return int(c), nil
}

func (c *LaneConcurrency) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode {
		return fmt.Errorf("concurrency must be a number or \"auto\"")
	}
	if value.Value == "auto" {
		*c = LaneConcurrencyAuto
		return nil
	}
	var n int
	if err := value.Decode(&n); err != nil {
		return fmt.Errorf("concurrency must be a number or \"auto\", got %q", value.Value)
	}
	*c = LaneConcurrency(n)
	return nil
}

func (c LaneConcurrency) MarshalJSON() ([]byte, error) {
	if c.IsAuto() {
		return []byte(`"auto"`), nil
	}
	return json.Marshal(int(c))
}

func (c *LaneConcurrency) UnmarshalJSON(b []byte) error {
	if string(b) == `"auto"` {
		*c = LaneConcurrencyAuto
		return nil
	}
	var n int
	if err := json.Unmarshal(b, &n); err != nil {
		return fmt.Errorf("concurrency must be a number or \"auto\", got %s", b)
	}
	*c = LaneConcurrency(n)
	return nil
}

// IntentLanes is execution.lanes in intent.yaml.
type IntentLanes struct {
	// Mode is job (default) | component | env | auto.
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
	// MaxLanes caps the lanes auto mode emits (default 16). `orun plan
	// --max-lanes` and ORUN_MAX_LANES override it for one plan.
	MaxLanes int `yaml:"maxLanes,omitempty" json:"maxLanes,omitempty"`
	// Concurrency is the jobs in flight inside one lane (default 4), or
	// auto for the runner's core count.
	Concurrency LaneConcurrency `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`
	// OnCycle is split (default) | reject.
	OnCycle string `yaml:"onCycle,omitempty" json:"onCycle,omitempty"`
	// UpstreamWait bounds how long a lane job waits for an upstream in
	// another lane through remote state (a Go duration; default 60m).
	UpstreamWait string `yaml:"upstreamWait,omitempty" json:"upstreamWait,omitempty"`
	// Groups are named lanes a component joins with spec.lane.
	Groups map[string]LaneGroup `yaml:"groups,omitempty" json:"groups,omitempty"`
	// Rules switch the mode by trigger; the first whose triggerRef is active
	// wins.
	Rules []LaneRule `yaml:"rules,omitempty" json:"rules,omitempty"`
}

// LaneGroup is one named lane group.
type LaneGroup struct {
	// Concurrency overrides execution.lanes.concurrency for this group; a
	// count or auto.
	Concurrency LaneConcurrency `yaml:"concurrency,omitempty" json:"concurrency,omitempty"`
}

// LaneRule selects a mode when a trigger binding is active.
type LaneRule struct {
	Mode string          `yaml:"mode" json:"mode"`
	When ProfileRuleWhen `yaml:"when" json:"when"`
}

// IsValidLaneMode reports whether mode is a lane mode (empty means default).
func IsValidLaneMode(mode string) bool {
	switch mode {
	case "", LaneModeJob, LaneModeComponent, LaneModeEnv, LaneModeAuto:
		return true
	}
	return false
}

// IsReservedLaneName reports whether name cannot be a group name.
func IsReservedLaneName(name string) bool {
	switch name {
	case LaneModeJob, LaneModeAuto, LaneIsolated:
		return true
	}
	return false
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

// EnvironmentLink is one door into a running environment beyond its front
// door: an API host, an admin console, a status page. Surfaced on the
// environment's catalog entity after the front door (url) — the Overview's
// product head and `orun catalog describe environment:<name>` show them.
type EnvironmentLink struct {
	Title string `yaml:"title" json:"title"`
	URL   string `yaml:"url" json:"url"`
	Icon  string `yaml:"icon,omitempty" json:"icon,omitempty"`
}

// Environment defines environment runtime contexts
type Environment struct {
	Path string `yaml:"path" json:"path"`
	// Description is a one-line "what is this environment" for the catalog
	// entity the environment materializes as (fill-empty; a
	// catalog.entities enrichment for the same environment no longer needs
	// to carry one).
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// URL is the environment's front door — the absolute http(s) address a
	// person opens to reach the product running in this environment. It
	// rides the catalog snapshot as the environment entity's primary link
	// (links[0], role=primary) and spec.url, so the Orunbase Overview can
	// show one door per environment next to the product's description. A
	// relative or non-http(s) value is a resolve error.
	URL string `yaml:"url,omitempty" json:"url,omitempty"`
	// Links are further doors (API, admin, status page), after the URL.
	Links             []EnvironmentLink                 `yaml:"links,omitempty" json:"links,omitempty"`
	Activation        EnvironmentActivation             `yaml:"activation,omitempty" json:"activation,omitempty"`
	Promotion         EnvironmentPromotion              `yaml:"promotion,omitempty" json:"promotion,omitempty"`
	Selectors         EnvironmentSelectors              `yaml:"selectors" json:"selectors"`
	ParameterDefaults map[string]map[string]interface{} `yaml:"parameterDefaults" json:"parameterDefaults"`
	Policies          map[string]interface{}            `yaml:"policies" json:"policies"`
	Env               map[string]string                 `yaml:"env,omitempty" json:"env,omitempty"`
	// SecretEnv maps env var names to secret:// references (never values).
	SecretEnv map[string]string `yaml:"secretEnv,omitempty" json:"secretEnv,omitempty"`
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

// ComponentChange declares which change signals affect a component.
type ComponentChange struct {
	Watches []string `yaml:"watches,omitempty" json:"watches,omitempty"`
	// Inputs are repository-root-relative glob patterns (doublestar `**`
	// supported) naming files outside the component's own directory that
	// still select it under --changed: a root lockfile, turbo.json, a shared
	// tooling tree. Selection then propagates over dependsOn input:true edges
	// exactly as for an owned file. Validated by internal/inputglob. Mirrors
	// internal/catalogmodel.ComponentYAMLChange.Inputs.
	Inputs []string `yaml:"inputs,omitempty" json:"inputs,omitempty"`
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
	Parameters     map[string]interface{}   `yaml:"parameters" json:"parameters"`
	Overrides      ComponentOverrides       `yaml:"overrides" json:"overrides"`
	Labels         map[string]string        `yaml:"labels" json:"labels"`
	DependsOn      []Dependency             `yaml:"dependsOn" json:"dependsOn"`
	Env            map[string]string        `yaml:"env,omitempty" json:"env,omitempty"`
	// SecretEnv maps env var names to secret:// references (never values —
	// the planner rejects literals; specs/orun-secrets/data-model.md §2.1).
	SecretEnv map[string]string `yaml:"secretEnv,omitempty" json:"secretEnv,omitempty"`
	// OptionalSecretEnv maps env var names to secret:// references that
	// resolve BEST-EFFORT: a key absent from the backend is skipped at
	// resolve (no env var, no failure) instead of failing the job. The
	// wire-now-seed-later shape for runtime secrets a component CAN consume
	// before anyone has stored them. Same leak guard; a key may not appear
	// in env, secretEnv, and optionalSecretEnv at once.
	OptionalSecretEnv         map[string]string `yaml:"optionalSecretEnv,omitempty" json:"optionalSecretEnv,omitempty"`
	Change                    ComponentChange   `yaml:"change,omitempty" json:"change,omitempty"`
	// Lane places this component's jobs: the name of a group declared in
	// execution.lanes.groups, or LaneIsolated so they never share a runner.
	// Empty leaves them to execution.lanes.mode (specs/orun-lanes §3.2).
	Lane                      string            `yaml:"lane,omitempty" json:"lane,omitempty"`
	ResolvedComposition       string            `yaml:"-" json:"-"`
	ResolvedCompositionSource string            `yaml:"-" json:"-"`
	SourcePath                string            `yaml:"-" json:"-"`
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
	DependencyRules []DependencyRule  `yaml:"dependencyRules,omitempty" json:"dependencyRules,omitempty"`
	Env             map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
	// SecretEnv maps env var names to secret:// references (never values).
	SecretEnv  map[string]string      `yaml:"secretEnv,omitempty" json:"secretEnv,omitempty"`
	Parameters map[string]interface{} `yaml:"parameters,omitempty" json:"parameters,omitempty"`
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
	// Input declares the dependency's sources as build inputs of this
	// component. Interpreted by the catalog change engine (--changed
	// rescopes the dependent when the dependency changes); inert to the
	// plan engine's ordering semantics. Declared here so both parsers of
	// component.yaml accept the same authored shape.
	Input bool `yaml:"input,omitempty" json:"input,omitempty"`
	// Mode optionally overrides the lane's resolved dependencyMode for this
	// edge only (enforced | advisory | disabled). When unset, the edge
	// follows the dependent's lane-level mode (subscription rules >
	// subscription dependencyMode > environment dependencyMode > enforced).
	Mode string `yaml:"mode,omitempty" json:"mode,omitempty"`
	// ModeRules conditionally override Mode for this edge based on the
	// matched triggerRef. First match wins; if nothing matches, Mode (or,
	// when unset, the lane-level mode) applies.
	ModeRules []DependencyRule `yaml:"modeRules,omitempty" json:"modeRules,omitempty"`
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
	SecretEnv      map[string]string    // root-level secretEnv from intent (references only)
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
	// SecretEnv maps env var names to validated secret:// references,
	// merged with the same 4-layer precedence as Env. References only —
	// the expander's leak guard rejects literals.
	SecretEnv map[string]string
	// OptionalSecretEnv maps env var names to validated secret:// references
	// that resolve BEST-EFFORT: a key that does not exist (yet) in the backend
	// is skipped at resolve — no env var, no failure — instead of failing the
	// job fail-closed. The wire-now-seed-later shape: a component declares the
	// runtime secrets it CAN consume before anyone has stored them. Same leak
	// guard as SecretEnv; a key may not appear in both maps.
	OptionalSecretEnv     map[string]string
	StepOverrides         []Step
	Policies              map[string]interface{}
	DependsOn             []ResolvedDependency
	Enabled               bool
	ProfileRef            string
	ProfileName           string
	ProfileSource         string
	ProfileRuleTriggerRef string
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
	// Mode is the edge-level dependency mode resolved from
	// Dependency.ModeRules / Dependency.Mode. Empty means the edge follows
	// the dependent instance's lane-level DependencyMode.
	Mode string
	// ModeSource records which edge layer set Mode: "edge" or "edge-rule".
	// Empty when Mode is empty.
	ModeSource string
	// ModeRuleTriggerRef records the matched triggerRef when ModeSource is
	// "edge-rule".
	ModeRuleTriggerRef string
}

// IntentWork is the `work:` section of intent.yaml (orun-cloud
// saas-work-gitops, design §1): where the epics live (one directory per
// epic, named by its slug, holding epic.yaml, the design docs and the status
// file), where the task contracts live (`<KEY>.TaskContract.yaml`), and
// whether `orun work sync` reconciles the tree into the platform on the
// default branch. Every field is optional; Layout applies the defaults.
type IntentWork struct {
	// Epics is the epics directory, repo-relative. Default "specs/epics".
	Epics string `yaml:"epics,omitempty" json:"epics,omitempty"`
	// Tasks is the task-contracts directory, repo-relative. Default "tasks".
	Tasks string `yaml:"tasks,omitempty" json:"tasks,omitempty"`
	// Sync is "off" (default) or "on-merge".
	Sync string `yaml:"sync,omitempty" json:"sync,omitempty"`
}

// The defaults a repository with no `work:` section has always had.
const (
	DefaultWorkEpicsDir = "specs/epics"
	DefaultWorkTasksDir = "tasks"
	WorkSyncOff         = "off"
	WorkSyncOnMerge     = "on-merge"
)

// WorkLayout is the resolved `work:` section: every field set, slashes
// forward, no leading "./" or trailing "/".
type WorkLayout struct {
	Epics string
	Tasks string
	Sync  string
}

// DefaultWorkLayout is the layout of a repository that declares nothing.
func DefaultWorkLayout() WorkLayout {
	return WorkLayout{Epics: DefaultWorkEpicsDir, Tasks: DefaultWorkTasksDir, Sync: WorkSyncOff}
}

// Declared reports whether the repository has a `work:` section at all —
// the rules that only make sense for a declared tree (work-manifest) are
// silent otherwise.
func (w WorkLayout) Declared() bool { return w != DefaultWorkLayout() }

// WorkLayout resolves the section with its defaults. A nil intent is the
// default layout, so callers need no nil check.
func (i *Intent) WorkLayout() WorkLayout {
	l := DefaultWorkLayout()
	if i == nil || i.Work == nil {
		return l
	}
	if s := cleanWorkDir(i.Work.Epics); s != "" {
		l.Epics = s
	}
	if s := cleanWorkDir(i.Work.Tasks); s != "" {
		l.Tasks = s
	}
	if s := strings.ToLower(strings.TrimSpace(i.Work.Sync)); s != "" {
		l.Sync = s
	}
	return l
}

// ValidateWork refuses a `work:` section that names an absolute path, an
// escape from the repository, or an unknown sync mode. Called by the
// loader; a repository with no section always validates.
func (i *Intent) ValidateWork() error {
	if i == nil || i.Work == nil {
		return nil
	}
	for _, f := range []struct{ name, value string }{{"epics", i.Work.Epics}, {"tasks", i.Work.Tasks}} {
		v := cleanWorkDir(f.value)
		if f.value != "" && v == "" || strings.HasPrefix(v, "/") || v == ".." || strings.HasPrefix(v, "../") {
			return fmt.Errorf("intent.yaml work.%s: %q must be a repo-relative directory", f.name, f.value)
		}
	}
	switch s := strings.ToLower(strings.TrimSpace(i.Work.Sync)); s {
	case "", WorkSyncOff, WorkSyncOnMerge:
		return nil
	default:
		return fmt.Errorf("intent.yaml work.sync: %q is not one of off, on-merge", i.Work.Sync)
	}
}

// cleanWorkDir normalizes a declared directory: forward slashes, no "./",
// no trailing slash. "" for an empty or dot-only value.
func cleanWorkDir(s string) string {
	s = strings.TrimSpace(strings.ReplaceAll(s, "\\", "/"))
	s = strings.TrimPrefix(s, "./")
	s = strings.TrimSuffix(s, "/")
	if s == "." {
		return ""
	}
	return s
}
