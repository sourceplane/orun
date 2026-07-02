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
	Type        string `yaml:"type" json:"type"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	// Version is the composition's semver (orun-service-catalog SC7,
	// data-model.md §5); carried onto the resolved Composition entity so the
	// golden path is a versioned, owned artifact.
	Version string `yaml:"version,omitempty" json:"version,omitempty"`
	// Lifecycle is the composition's maturity stage (stable|beta|deprecated).
	Lifecycle         string                      `yaml:"lifecycle,omitempty" json:"lifecycle,omitempty"`
	DefaultJob        string                      `yaml:"defaultJob" json:"defaultJob"`
	DefaultProfile    string                      `yaml:"defaultProfile,omitempty" json:"defaultProfile,omitempty"`
	SchemaRef         *ResourceRef                `yaml:"schemaRef,omitempty" json:"schemaRef,omitempty"`
	ParameterSchema   map[string]interface{}      `yaml:"parameterSchema,omitempty" json:"parameterSchema,omitempty"`
	ExecutionProfiles map[string]ExecutionProfile `yaml:"executionProfiles,omitempty" json:"executionProfiles,omitempty"`
	Jobs              []CompositionJobEntry       `yaml:"jobs" json:"jobs"`
	Profiles          []CompositionProfileEntry   `yaml:"profiles,omitempty" json:"profiles,omitempty"`
	// Effects declares what running this golden path contributes back to the
	// catalog (orun-service-catalog SC8, compositions.md §3/§4): integration
	// join-keys it registers, APIs/Resources it provisions, and the scorecard
	// contributions it satisfies. Declarations only — the live plane records what
	// actually happened (declared-vs-actual, S-7).
	Effects *CompositionEffects `yaml:"effects,omitempty" json:"effects,omitempty"`
}

// CompositionEffects is the producer declaration on a composition (data-model.md
// §5). All fields are intent, not observation.
type CompositionEffects struct {
	// Integrations is the join-key shape the golden path registers (datadog,
	// pagerduty, …) — merged into the integrations block of every component the
	// composition backs.
	Integrations map[string]interface{} `yaml:"integrations,omitempty" json:"integrations,omitempty"`
	// Provides names the Resource entity keys the golden path provisions
	// (each becomes a derived Resource the backed components dependsOn).
	Provides []string `yaml:"provides,omitempty" json:"provides,omitempty"`
	// Exposes names the API entity keys the golden path exposes (each becomes a
	// derived API the backed components providesApi via contracts).
	Exposes []string `yaml:"exposes,omitempty" json:"exposes,omitempty"`
	// Scorecards is the scorecard-contribution declaration, carried verbatim into
	// the resolved Composition node but NOT evaluated here (the scorecard engine
	// is the v2 epic specs/orun-scorecards/).
	Scorecards map[string]interface{} `yaml:"scorecards,omitempty" json:"scorecards,omitempty"`
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
	Description  string                 `yaml:"description,omitempty" json:"description,omitempty"`
	RunsOn       string                 `yaml:"runsOn,omitempty" json:"runsOn,omitempty"`
	Timeout      string                 `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Retries      int                    `yaml:"retries,omitempty" json:"retries,omitempty"`
	Steps        []Step                 `yaml:"steps,omitempty" json:"steps,omitempty"`
	Inputs       map[string]interface{} `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	Labels       map[string]string      `yaml:"labels,omitempty" json:"labels,omitempty"`
	Capabilities []string               `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
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
	// SecretBindings declares the logical secrets this profile needs, keyed by
	// binding name. It is portable (ships in the Stack) and value-free: the
	// planner maps each binding to a secret:// reference for the resolving
	// (project, env) at plan time (specs/orun-secrets/data-model.md §2.2).
	SecretBindings map[string]SecretBinding `yaml:"secretBindings,omitempty" json:"secretBindings,omitempty"`
	// Materialize declares the deploy-time last mile: after the deploy step
	// succeeds the runner delivers the named (subset of) secrets into the
	// deployed application's native store via a typed adapter, under the same
	// policy decision the job's resolve already made (specs/orun-secrets/
	// data-model.md §2.3, SD-13). Value-free — key names only.
	Materialize *MaterializeSpec `yaml:"materialize,omitempty" json:"materialize,omitempty"`
}

// MaterializeSpec is a profile's runtime-delivery block (data-model.md §2.3).
// Target names a typed adapter (v1: "cloudflare-worker"). Secrets are the
// binding keys to sync — a compile-checked subset of the profile's
// secretBindings (or the component's secretEnv). OnRotate is reserved (e.g.
// "redeploy"): rotation convergence via a system trigger is a follow-up (SEC6
// builds the deploy-time step only — see runner-integration.md §6).
type MaterializeSpec struct {
	Target   string   `yaml:"target" json:"target"`
	Secrets  []string `yaml:"secrets,omitempty" json:"secrets,omitempty"`
	OnRotate string   `yaml:"onRotate,omitempty" json:"onRotate,omitempty"`
}

// SecretBinding is one logical secret a profile requires. Required marks a
// binding whose reference MUST be mappable at plan time (else a compile error).
// AsEnv optionally overrides the environment variable the resolved value is
// injected as; it defaults to the binding key when empty.
type SecretBinding struct {
	Required bool   `yaml:"required,omitempty" json:"required,omitempty"`
	AsEnv    string `yaml:"asEnv,omitempty" json:"asEnv,omitempty"`
}

// ProfilePolicies defines enforcement rules for a profile.
type ProfilePolicies struct {
	RequireCleanGitTree           bool `yaml:"requireCleanGitTree,omitempty" json:"requireCleanGitTree,omitempty"`
	RequirePinnedTerraformVersion bool `yaml:"requirePinnedTerraformVersion,omitempty" json:"requirePinnedTerraformVersion,omitempty"`
	RequireApproval               bool `yaml:"requireApproval,omitempty" json:"requireApproval,omitempty"`
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
	Version      string                  `yaml:"version" json:"version"`
	Orun         CompositionPackageOrun  `yaml:"orun,omitempty" json:"orun,omitempty"`
	Exports      []CompositionExport     `yaml:"exports" json:"exports"`
	Dependencies []CompositionDependency `yaml:"dependencies,omitempty" json:"dependencies,omitempty"`
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
	APIVersion string              `yaml:"apiVersion" json:"apiVersion"`
	Kind       string              `yaml:"kind" json:"kind"`
	Metadata   Metadata            `yaml:"metadata" json:"metadata"`
	Spec       ComponentSchemaSpec `yaml:"spec" json:"spec"`
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
	Description    string                    `yaml:"description,omitempty" json:"description,omitempty"`
	Policies       *ProfilePolicies          `yaml:"policies,omitempty" json:"policies,omitempty"`
	Jobs           map[string]ProfileJobSpec `yaml:"jobs" json:"jobs"`
	SecretBindings map[string]SecretBinding  `yaml:"secretBindings,omitempty" json:"secretBindings,omitempty"`
	Materialize    *MaterializeSpec          `yaml:"materialize,omitempty" json:"materialize,omitempty"`
}
