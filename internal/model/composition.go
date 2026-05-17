package model

// CompositionConfig declares where composition definitions are loaded from.
type CompositionConfig struct {
	Sources    []CompositionSource   `yaml:"sources,omitempty" json:"sources,omitempty"`
	Resolution CompositionResolution `yaml:"resolution,omitempty" json:"resolution,omitempty"`
}

// CompositionSource describes one source of composition packages.
type CompositionSource struct {
	Name       string            `yaml:"name" json:"name"`
	Kind       string            `yaml:"kind" json:"kind"`
	Path       string            `yaml:"path,omitempty" json:"path,omitempty"`
	Ref        string            `yaml:"ref,omitempty" json:"ref,omitempty"`
	Digest     string            `yaml:"digest,omitempty" json:"digest,omitempty"`
	PullPolicy string            `yaml:"pullPolicy,omitempty" json:"pullPolicy,omitempty"`
	Verify     *VerifyPolicy     `yaml:"verify,omitempty" json:"verify,omitempty"`
	Metadata   map[string]string `yaml:"metadata,omitempty" json:"metadata,omitempty"`
}

// VerifyPolicy reserves room for future supply-chain verification settings.
type VerifyPolicy struct {
	Provider string `yaml:"provider,omitempty" json:"provider,omitempty"`
	KeyRef   string `yaml:"keyRef,omitempty" json:"keyRef,omitempty"`
	Required bool   `yaml:"required,omitempty" json:"required,omitempty"`
}

// CompositionResolution controls how exported composition types are selected.
type CompositionResolution struct {
	Precedence []string          `yaml:"precedence,omitempty" json:"precedence,omitempty"`
	Bindings   map[string]string `yaml:"bindings,omitempty" json:"bindings,omitempty"`
}

// ComponentCompositionRef overrides the source used for a single component.
type ComponentCompositionRef struct {
	Source string `yaml:"source,omitempty" json:"source,omitempty"`
	Name   string `yaml:"name,omitempty" json:"name,omitempty"`
}

// CompositionDocument is the self-describing composition definition.
type CompositionDocument struct {
	APIVersion string                  `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                  `yaml:"kind" json:"kind"`
	Metadata   Metadata                `yaml:"metadata" json:"metadata"`
	Spec       CompositionDocumentSpec `yaml:"spec" json:"spec"`
}

// CompositionDocumentSpec is the portable contract for one composition type.
type CompositionDocumentSpec struct {
	Type              string                       `yaml:"type" json:"type"`
	Description       string                       `yaml:"description,omitempty" json:"description,omitempty"`
	DefaultJob        string                       `yaml:"defaultJob" json:"defaultJob"`
	DefaultProfile    string                       `yaml:"defaultProfile,omitempty" json:"defaultProfile,omitempty"`
	SchemaRef         *ResourceRef                 `yaml:"schemaRef,omitempty" json:"schemaRef,omitempty"`
	InputSchema       map[string]interface{}       `yaml:"inputSchema,omitempty" json:"inputSchema,omitempty"`
	ExecutionProfiles map[string]ExecutionProfile  `yaml:"executionProfiles,omitempty" json:"executionProfiles,omitempty"`
	Jobs              []CompositionJobEntry        `yaml:"jobs" json:"jobs"`
	Profiles          []CompositionProfileEntry    `yaml:"profiles,omitempty" json:"profiles,omitempty"`
}

// ResourceRef is a named reference to another resource in the same package.
type ResourceRef struct {
	Name string `yaml:"name" json:"name"`
}

// CompositionJobEntry represents a job in the composition — either inline or via templateRef.
type CompositionJobEntry struct {
	Name        string       `yaml:"name" json:"name"`
	TemplateRef *ResourceRef `yaml:"templateRef,omitempty" json:"templateRef,omitempty"`

	// Inline job fields (used when templateRef is nil)
	Description string                 `yaml:"description,omitempty" json:"description,omitempty"`
	RunsOn      string                 `yaml:"runsOn,omitempty" json:"runsOn,omitempty"`
	Timeout     string                 `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Retries     int                    `yaml:"retries,omitempty" json:"retries,omitempty"`
	Steps       []Step                 `yaml:"steps,omitempty" json:"steps,omitempty"`
	Inputs      map[string]interface{} `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Labels      map[string]string      `yaml:"labels,omitempty" json:"labels,omitempty"`
	Capabilities []string              `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
}

// CompositionProfileEntry represents a profile in the composition — either inline or via profileRef.
type CompositionProfileEntry struct {
	Name       string       `yaml:"name" json:"name"`
	ProfileRef *ResourceRef `yaml:"profileRef,omitempty" json:"profileRef,omitempty"`
}

// Step is a single execution unit within a job (imported from job.go for composition inline use).
// See job.go for the canonical Step type.

// ToJobSpec converts a CompositionJobEntry to a JobSpec for internal use.
func (e CompositionJobEntry) ToJobSpec() JobSpec {
	return JobSpec{
		Name:         e.Name,
		Description:  e.Description,
		RunsOn:       e.RunsOn,
		Timeout:      e.Timeout,
		Retries:      e.Retries,
		Steps:        e.Steps,
		Inputs:       e.Inputs,
		Labels:       e.Labels,
		Capabilities: e.Capabilities,
	}
}

// ExecutionProfile is a named selection of jobs and steps from the composition.
type ExecutionProfile struct {
	Description string                    `yaml:"description,omitempty" json:"description,omitempty"`
	Policies    *ProfilePolicies          `yaml:"policies,omitempty" json:"policies,omitempty"`
	Jobs        map[string]ProfileJobSpec `yaml:"jobs" json:"jobs"`
}

// ProfilePolicies defines enforcement rules for a profile.
type ProfilePolicies struct {
	RequireCleanGitTree          bool `yaml:"requireCleanGitTree,omitempty" json:"requireCleanGitTree,omitempty"`
	RequirePinnedTerraformVersion bool `yaml:"requirePinnedTerraformVersion,omitempty" json:"requirePinnedTerraformVersion,omitempty"`
	RequireApproval              bool `yaml:"requireApproval,omitempty" json:"requireApproval,omitempty"`
}

// ProfileJobSpec selects which steps from a base job are included in a profile.
type ProfileJobSpec struct {
	StepsEnabled        []string                    `yaml:"stepsEnabled,omitempty" json:"stepsEnabled,omitempty"`
	IncludeCapabilities []string                    `yaml:"includeCapabilities,omitempty" json:"includeCapabilities,omitempty"`
	StepOverrides       map[string]ProfileStepPatch `yaml:"stepOverrides,omitempty" json:"stepOverrides,omitempty"`
}

// ProfileStepPatch allows overriding specific fields of a step within a profile.
type ProfileStepPatch struct {
	Run  string                 `yaml:"run,omitempty" json:"run,omitempty"`
	With map[string]interface{} `yaml:"with,omitempty" json:"with,omitempty"`
	Env  map[string]interface{} `yaml:"env,omitempty" json:"env,omitempty"`
}

// CompositionPackage is the package manifest at the root of a composition package.
type CompositionPackage struct {
	APIVersion string                 `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                 `yaml:"kind" json:"kind"`
	Metadata   Metadata               `yaml:"metadata" json:"metadata"`
	Spec       CompositionPackageSpec `yaml:"spec" json:"spec"`
}

// CompositionPackageSpec defines versioned exported compositions.
type CompositionPackageSpec struct {
	Version      string                   `yaml:"version" json:"version"`
	Orun        CompositionPackageOrun `yaml:"orun,omitempty" json:"orun,omitempty"`
	Exports      []CompositionExport      `yaml:"exports" json:"exports"`
	Dependencies []CompositionDependency  `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
}

// CompositionPackageOrun constrains compatible orun versions.
type CompositionPackageOrun struct {
	MinVersion string `yaml:"minVersion,omitempty" json:"minVersion,omitempty"`
}

// CompositionExport maps a logical composition name to a file in the package.
type CompositionExport struct {
	Composition string `yaml:"composition" json:"composition"`
	Path        string `yaml:"path" json:"path"`
}

// CompositionDependency captures optional transitive package references.
type CompositionDependency struct {
	Name     string `yaml:"name" json:"name"`
	Ref      string `yaml:"ref" json:"ref"`
	Optional bool   `yaml:"optional,omitempty" json:"optional,omitempty"`
}

// Stack is the new-format package manifest (replaces CompositionPackage / orun.yaml).
// It lives at stack.yaml in the package root and uses apiVersion: orun.io/v1.
type Stack struct {
	APIVersion string        `yaml:"apiVersion" json:"apiVersion"`
	Kind       string        `yaml:"kind" json:"kind"`
	Metadata   StackMetadata `yaml:"metadata" json:"metadata"`
	Registry   StackRegistry `yaml:"registry" json:"registry"`
	Spec       StackSpec     `yaml:"spec" json:"spec"`
}

// StackMetadata holds human-facing metadata for a stack package.
type StackMetadata struct {
	Name        string   `yaml:"name" json:"name"`
	Title       string   `yaml:"title,omitempty" json:"title,omitempty"`
	Version     string   `yaml:"version" json:"version"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Owner       string   `yaml:"owner,omitempty" json:"owner,omitempty"`
	Tags        []string `yaml:"tags,omitempty" json:"tags,omitempty"`
}

// StackRegistry describes the OCI registry where the stack is published.
type StackRegistry struct {
	Host       string `yaml:"host" json:"host"`
	Namespace  string `yaml:"namespace" json:"namespace"`
	Repository string `yaml:"repository" json:"repository"`
	Visibility string `yaml:"visibility,omitempty" json:"visibility,omitempty"`
}

// StackSpec lists the composition files included in the stack.
// When Compositions is empty the packager auto-discovers every compositions.yaml
// found by walking the directory that contains stack.yaml.
type StackSpec struct {
	Compositions  []StackCompositionEntry `yaml:"compositions,omitempty" json:"compositions,omitempty"`
	IntentPresets []StackIntentPreset     `yaml:"intentPresets,omitempty" json:"intentPresets,omitempty"`
}

// StackCompositionEntry is a path reference to a compositions.yaml within the stack.
type StackCompositionEntry struct {
	Path string `yaml:"path" json:"path"`
}

// CompositionLock records resolved source digests for reproducible planning.
type CompositionLock struct {
	APIVersion string                  `yaml:"apiVersion" json:"apiVersion"`
	Kind       string                  `yaml:"kind" json:"kind"`
	Sources    []CompositionLockSource `yaml:"sources" json:"sources"`
}

// CompositionLockSource stores one resolved source entry in the lock file.
type CompositionLockSource struct {
	Name           string   `yaml:"name" json:"name"`
	Kind           string   `yaml:"kind" json:"kind"`
	Ref            string   `yaml:"ref,omitempty" json:"ref,omitempty"`
	Path           string   `yaml:"path,omitempty" json:"path,omitempty"`
	ResolvedDigest string   `yaml:"resolvedDigest" json:"resolvedDigest"`
	Exports        []string `yaml:"exports,omitempty" json:"exports,omitempty"`
}

// ComponentSchemaDocument is the self-describing schema definition (kind: ComponentSchema).
type ComponentSchemaDocument struct {
	APIVersion string               `yaml:"apiVersion" json:"apiVersion"`
	Kind       string               `yaml:"kind" json:"kind"`
	Metadata   Metadata             `yaml:"metadata" json:"metadata"`
	Spec       ComponentSchemaSpec  `yaml:"spec" json:"spec"`
}

// ComponentSchemaSpec defines the JSON schema for a component type.
type ComponentSchemaSpec struct {
	Type   string                 `yaml:"type" json:"type"`
	Schema map[string]interface{} `yaml:"schema" json:"schema"`
}

// JobTemplateDocument is the self-describing job template definition (kind: JobTemplate).
type JobTemplateDocument struct {
	APIVersion string          `yaml:"apiVersion" json:"apiVersion"`
	Kind       string          `yaml:"kind" json:"kind"`
	Metadata   Metadata        `yaml:"metadata" json:"metadata"`
	Spec       JobTemplateSpec `yaml:"spec" json:"spec"`
}

// JobTemplateSpec defines a reusable job template.
type JobTemplateSpec struct {
	Description  string                 `yaml:"description,omitempty" json:"description,omitempty"`
	RunsOn       string                 `yaml:"runsOn,omitempty" json:"runsOn,omitempty"`
	Timeout      string                 `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Retries      int                    `yaml:"retries,omitempty" json:"retries,omitempty"`
	Labels       map[string]string      `yaml:"labels,omitempty" json:"labels,omitempty"`
	Capabilities []string               `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Steps        []Step                 `yaml:"steps" json:"steps"`
	Inputs       map[string]interface{} `yaml:"inputs,omitempty" json:"inputs,omitempty"`
}

// ExecutionProfileDocument is the self-describing execution profile definition (kind: ExecutionProfile).
type ExecutionProfileDocument struct {
	APIVersion string               `yaml:"apiVersion" json:"apiVersion"`
	Kind       string               `yaml:"kind" json:"kind"`
	Metadata   Metadata             `yaml:"metadata" json:"metadata"`
	Spec       ExecutionProfileSpec `yaml:"spec" json:"spec"`
}

// ExecutionProfileSpec defines the behavior overlay for one execution context.
type ExecutionProfileSpec struct {
	Description string                    `yaml:"description,omitempty" json:"description,omitempty"`
	Policies    *ProfilePolicies          `yaml:"policies,omitempty" json:"policies,omitempty"`
	Jobs        map[string]ProfileJobSpec `yaml:"jobs" json:"jobs"`
}