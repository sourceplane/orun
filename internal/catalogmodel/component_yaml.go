package catalogmodel

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
type ComponentYAMLSpec struct {
	Type         string                              `json:"type"`
	Lifecycle    string                              `json:"lifecycle,omitempty"`
	Owner        string                              `json:"owner,omitempty"`
	System       string                              `json:"system,omitempty"`
	Path         string                              `json:"path,omitempty"`
	DependsOn    []ComponentYAMLDependency           `json:"dependsOn,omitempty"`
	ProvidesAPIs []string                            `json:"providesApis,omitempty"`
	ConsumesAPIs []string                            `json:"consumesApis,omitempty"`
	Environments map[string]ComponentYAMLEnvironment `json:"environments,omitempty"`
}

// ComponentYAMLDependency is one entry in the authored `dependsOn` list. Only
// `component` is required; `relationship` and `optional` default to "calls"
// and false respectively in the resolver.
type ComponentYAMLDependency struct {
	Component    string `json:"component"`
	Relationship string `json:"relationship,omitempty"`
	Optional     bool   `json:"optional,omitempty"`
}

// ComponentYAMLEnvironment is one entry in the authored `environments` map.
type ComponentYAMLEnvironment struct {
	Profile string `json:"profile"`
}
