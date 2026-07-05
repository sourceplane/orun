package catalogmodel

import (
	"bytes"
	"encoding/json"
)

//go:generate go run ./schema/gen schema/component-yaml.schema.json

// ComponentYAML is the authored, never-mutated `component.yaml` shape. See
// data-model.md §6.
//
// The resolver loads this file from each component path and feeds it into
// the inheritance/inference pipeline that produces a ComponentManifest. The
// JSON Schema in schema/component-yaml.schema.json is generated from this
// type via `go generate ./internal/catalogmodel/...`.
//
// JSON tags match the YAML keys verbatim (lowerCamelCase) — yaml.v3 in this
// repo respects `json:` tags as a fallback when no `yaml:` tag is set, so a
// single tag set keeps the manifest, the schema, and the YAML reader in
// lockstep.
//
// Fields with `omitempty` are optional in the generated JSON Schema; the
// rest are required.
type ComponentYAML struct {
	APIVersion string                `json:"apiVersion"`
	Kind       string                `json:"kind"`
	Metadata   ComponentYAMLMetadata `json:"metadata"`
	Spec       ComponentYAMLSpec     `json:"spec"`
}

// OpenSchema lets authored manifests carry plan-engine or legacy keys the
// catalog does not interpret without failing validation, mirroring the plan
// engine's tolerance for unknown fields. The declared fields are still
// type-checked. Applied to every authored struct an author writes into
// directly; the resolved/internal sub-shapes stay closed.
func (ComponentYAML) OpenSchema() bool { return true }

// OpenSchema — see ComponentYAML.OpenSchema.
func (ComponentYAMLMetadata) OpenSchema() bool { return true }

// OpenSchema — see ComponentYAML.OpenSchema.
func (ComponentYAMLSpec) OpenSchema() bool { return true }

// OpenSchema — see ComponentYAML.OpenSchema. Covers legacy dependency
// attributes (environment, scope, condition) the catalog does not yet model.
func (ComponentYAMLDependency) OpenSchema() bool { return true }

// ComponentYAMLMetadata is the authored metadata block. Only `name` is
// required; the rest are optional and may be filled in by inheritance or
// inference.
type ComponentYAMLMetadata struct {
	Name        string            `json:"name"`
	Title       string            `json:"title,omitempty"`
	Description string            `json:"description,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// ComponentYAMLSpec is the authored spec block. Only `type` is required; the
// rest are optional and resolved by the catalog pipeline.
//
// Two environment-binding forms are accepted. `environments` is the catalog
// map form (envName → {profile}). `subscribe` is the legacy list form shared
// with the plan engine (see internal/model.ComponentSubscribe); it carries
// the richer per-environment authoring vocabulary (profileRules, env,
// parameters, dependency overrides). Components may use either; the resolver
// folds both into the resolved manifest's `environments` map.
//
// `domain`, `labels`, `parameters`, and `env` mirror the plan-engine
// authoring contract so a single component.yaml validates against both the
// catalog schema and the plan engine. `env` is accepted for compatibility but
// is not surfaced in the resolved manifest (the catalog has no env slot).
type ComponentYAMLSpec struct {
	Type         string                              `json:"type"`
	Lifecycle    string                              `json:"lifecycle,omitempty"`
	Owner        string                              `json:"owner,omitempty"`
	System       string                              `json:"system,omitempty"`
	Domain       string                              `json:"domain,omitempty"`
	Path         string                              `json:"path,omitempty"`
	DependsOn    []ComponentYAMLDependency           `json:"dependsOn,omitempty"`
	ProvidesAPIs []string                            `json:"providesApis,omitempty"`
	ConsumesAPIs []string                            `json:"consumesApis,omitempty"`
	Environments map[string]ComponentYAMLEnvironment `json:"environments,omitempty"`
	Subscribe    *ComponentYAMLSubscribe             `json:"subscribe,omitempty"`
	Parameters   map[string]any                      `json:"parameters,omitempty"`
	Labels       map[string]string                   `json:"labels,omitempty"`
	Env          map[string]string                   `json:"env,omitempty"`
	Change       *ComponentYAMLChange                `json:"change,omitempty"`
	// Integrations / Links / Docs / Extensions are the catalog-hub authoring
	// blocks (orun-service-catalog SC6). `integrations` is a map of typed join
	// keys (datadog/pagerduty/…); `extensions` carries namespaced x-<vendor>
	// blocks preserved verbatim on round-trip (data-model.md §8).
	Integrations map[string]any      `json:"integrations,omitempty"`
	Links        []ComponentYAMLLink `json:"links,omitempty"`
	Docs         *ComponentYAMLDocs  `json:"docs,omitempty"`
	Extensions   map[string]any      `json:"extensions,omitempty"`
}

// ComponentYAMLLink is one authored external link (data-model.md §2).
type ComponentYAMLLink struct {
	Title string `json:"title"`
	URL   string `json:"url"`
	Icon  string `json:"icon,omitempty"`
}

// ComponentYAMLDocs points at overview/techdocs/runbooks/ADRs (data-model.md §2).
// `overview` is the reserved front-page md for the entity (saas-workspace-
// overview WO3); `pages` is the ordered multi-doc set (saas-catalog-docs CD1) —
// each entry a mapping with `path` required and optional key/title/role.
// Relative paths resolve against the component directory and are normalized
// repo-relative on the wire.
type ComponentYAMLDocs struct {
	Overview string    `json:"overview,omitempty"`
	Pages    []DocPage `json:"pages,omitempty"`
	TechDocs string    `json:"techdocs,omitempty"`
	Runbooks []string  `json:"runbooks,omitempty"`
	ADRs     []string  `json:"adrs,omitempty"`
}

// ComponentYAMLChange declares which intent change signals affect this
// component — the change-detection "watch" inputs (the catalog-canonical home,
// orun-catalog-state). Optional; omitted when no signals are watched.
type ComponentYAMLChange struct {
	Watches []string `json:"watches,omitempty"`
}

// ComponentYAMLDependency is one entry in the authored `dependsOn` list. Only
// `component` is required; `relationship` and `optional` default to "calls"
// and false respectively in the resolver.
type ComponentYAMLDependency struct {
	Component    string `json:"component"`
	Relationship string `json:"relationship,omitempty"`
	Optional     bool   `json:"optional,omitempty"`
	// Include controls change-detection plan selection: "always" pulls this
	// dependency into a --changed plan when the dependent is selected;
	// "if-selected" (the omitted default) does not. (orun-catalog-state CS5.)
	Include string `json:"include,omitempty"`
	// Input declares the dependency's sources as build inputs of this
	// component's artifact (a bundled shared package, not a runtime peer).
	// Change detection then treats a change to the dependency as a direct
	// change to this component — the reverse direction of Include — closing
	// the shared-package rescope gap where `--changed` only matched files
	// under a component's own directory. Transitive over other input edges.
	Input bool `json:"input,omitempty"`
}

// ComponentYAMLEnvironment is one entry in the authored `environments` map.
type ComponentYAMLEnvironment struct {
	Profile string `json:"profile"`
}

// ComponentYAMLSubscribe is the legacy `spec.subscribe` block. It mirrors
// internal/model.ComponentSubscribe so a component.yaml authored for the plan
// engine validates unchanged against the catalog schema.
type ComponentYAMLSubscribe struct {
	Environments []ComponentYAMLSubscribeEnvironment `json:"environments,omitempty"`
}

// ComponentYAMLSubscribeEnvironment is one entry in `spec.subscribe.environments`.
// Only the base `profile` is folded into the resolved manifest; the remaining
// fields (profileRules, dependency overrides, per-env env/parameters) are
// accepted for plan-engine compatibility but are not interpreted by the
// catalog resolver. The shape tracks internal/model.EnvironmentSubscription.
//
// Note: unlike the plan engine, the catalog requires the object form
// (`- name: dev`); the bare-string shorthand (`environments: [dev]`) is not
// accepted here.
type ComponentYAMLSubscribeEnvironment struct {
	Name            string                        `json:"name"`
	Profile         string                        `json:"profile,omitempty"`
	ProfileRules    []ComponentYAMLProfileRule    `json:"profileRules,omitempty"`
	DependencyMode  string                        `json:"dependencyMode,omitempty"`
	DependencyRules []ComponentYAMLDependencyRule `json:"dependencyRules,omitempty"`
	Env             map[string]string             `json:"env,omitempty"`
	Parameters      map[string]any                `json:"parameters,omitempty"`
}

// UnmarshalJSON accepts both the object form (`- name: dev`) and the
// bare-string shorthand (`- dev`), matching internal/model's YAML decoder.
// A bare string is treated as the environment name with no explicit profile.
func (e *ComponentYAMLSubscribeEnvironment) UnmarshalJSON(data []byte) error {
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] == '"' {
		var name string
		if err := json.Unmarshal(trimmed, &name); err != nil {
			return err
		}
		*e = ComponentYAMLSubscribeEnvironment{Name: name}
		return nil
	}
	// Object form. Alias breaks the recursion into this method.
	type alias ComponentYAMLSubscribeEnvironment
	var a alias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*e = ComponentYAMLSubscribeEnvironment(a)
	return nil
}

// JSONSchemaOverride emits a oneOf accepting the bare-string shorthand or an
// object. The object branch is intentionally open (no additionalProperties
// restriction) so the full legacy per-environment vocabulary — profileRules,
// dependency overrides, env, parameters — validates without the catalog
// having to track every plan-engine field. See schema/gen.
func (ComponentYAMLSubscribeEnvironment) JSONSchemaOverride() map[string]any {
	return map[string]any{
		"oneOf": []any{
			map[string]any{"type": "string"},
			map[string]any{
				"type":     "object",
				"required": []any{"name"},
				"properties": map[string]any{
					"name":    map[string]any{"type": "string"},
					"profile": map[string]any{"type": "string"},
				},
			},
		},
	}
}

// ComponentYAMLProfileRule conditionally selects an execution profile when a
// trigger fires. Mirrors internal/model.ProfileRule.
type ComponentYAMLProfileRule struct {
	Profile string                       `json:"profile"`
	When    ComponentYAMLProfileRuleWhen `json:"when"`
}

// ComponentYAMLProfileRuleWhen is the condition for a profile rule.
type ComponentYAMLProfileRuleWhen struct {
	TriggerRef string `json:"triggerRef"`
}

// ComponentYAMLDependencyRule conditionally overrides the dependency mode when
// a trigger fires. Mirrors internal/model.DependencyRule.
type ComponentYAMLDependencyRule struct {
	Mode string                          `json:"mode"`
	When ComponentYAMLDependencyRuleWhen `json:"when"`
}

// ComponentYAMLDependencyRuleWhen is the condition for a dependency rule.
type ComponentYAMLDependencyRuleWhen struct {
	TriggerRef string `json:"triggerRef"`
}
