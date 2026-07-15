package model

import (
	"fmt"
	"strings"
)

// StepPhase defines execution phase for a job step.
type StepPhase string

const (
	PhasePre  StepPhase = "pre"
	PhaseMain StepPhase = "main"
	PhasePost StepPhase = "post"
)

// JobRegistry holds all job definitions (k8s-style declarative format)
type JobRegistry struct {
	APIVersion string    `yaml:"apiVersion" json:"apiVersion"`
	Kind       string    `yaml:"kind" json:"kind"`
	Metadata   Metadata  `yaml:"metadata" json:"metadata"`
	Jobs       []JobSpec `yaml:"jobs" json:"jobs"`
}

// JobSpec defines a complete job specification with multiple steps
type JobSpec struct {
	Name         string                 `yaml:"name" json:"name"`
	Description  string                 `yaml:"description" json:"description"`
	RunsOn       string                 `yaml:"runsOn,omitempty" json:"runsOn,omitempty"`
	Timeout      string                 `yaml:"timeout" json:"timeout"`
	Retries      int                    `yaml:"retries" json:"retries"`
	Steps        []Step                 `yaml:"steps" json:"steps"`
	Inputs       map[string]interface{} `yaml:"inputs" json:"inputs"`
	Labels       map[string]string      `yaml:"labels" json:"labels"`
	Capabilities []string               `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
}

// Step is a single execution unit within a job
type Step struct {
	ID               string                 `yaml:"id,omitempty" json:"id,omitempty"`
	Name             string                 `yaml:"name" json:"name"`
	Capability       string                 `yaml:"capability,omitempty" json:"capability,omitempty"`
	Phase            string                 `yaml:"phase,omitempty" json:"phase,omitempty"`
	Order            int                    `yaml:"order,omitempty" json:"order,omitempty"`
	Run              string                 `yaml:"run,omitempty" json:"run,omitempty"`
	Use              string                 `yaml:"use,omitempty" json:"use,omitempty"`
	// Workflow names a torkflow workflow file to run as this step — the third
	// execution vocabulary beside run/use (specs/orun-workflows §3). Exactly one
	// of Run/Use/Workflow may be set on a step.
	Workflow         string                 `yaml:"workflow,omitempty" json:"workflow,omitempty"`
	With             map[string]interface{} `yaml:"with,omitempty" json:"with,omitempty"`
	Env              map[string]interface{} `yaml:"env,omitempty" json:"env,omitempty"`
	Shell            string                 `yaml:"shell,omitempty" json:"shell,omitempty"`
	WorkingDirectory string                 `yaml:"working-directory,omitempty" json:"working-directory,omitempty"`
	Timeout          string                 `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Retry            int                    `yaml:"retry,omitempty" json:"retry,omitempty"`
	OnFailure        string                 `yaml:"onFailure,omitempty" json:"onFailure,omitempty"` // stop, continue
}

// ValidateExecForm enforces the step's execution-vocabulary invariant: a step is
// exactly one of run / use / workflow (specs/orun-workflows §3, invariant 8). A
// step with more than one set is a compile error; a step with none is left to the
// existing downstream handling (a bare step is a no-op today). Returns a nil error
// when at most one form is set.
func (s Step) ValidateExecForm() error {
	set := make([]string, 0, 3)
	if strings.TrimSpace(s.Run) != "" {
		set = append(set, "run")
	}
	if strings.TrimSpace(s.Use) != "" {
		set = append(set, "use")
	}
	if strings.TrimSpace(s.Workflow) != "" {
		set = append(set, "workflow")
	}
	if len(set) > 1 {
		return fmt.Errorf("step %q sets %s — a step must use exactly one of run/use/workflow", s.Name, strings.Join(set, " and "))
	}
	return nil
}

// NormalizePhase returns a normalized phase string and defaults empty values to "main".
func NormalizePhase(phase string) string {
	p := strings.ToLower(strings.TrimSpace(phase))
	if p == "" {
		return string(PhaseMain)
	}
	return p
}

// IsValidPhase checks whether a phase is one of pre/main/post.
func IsValidPhase(phase string) bool {
	switch NormalizePhase(phase) {
	case string(PhasePre), string(PhaseMain), string(PhasePost):
		return true
	default:
		return false
	}
}

// JobBinding is a k8s-style declarative binding between a model and its jobs
type JobBinding struct {
	APIVersion string         `yaml:"apiVersion" json:"apiVersion"`
	Kind       string         `yaml:"kind" json:"kind"`
	Metadata   Metadata       `yaml:"metadata" json:"metadata"`
	Spec       JobBindingSpec `yaml:"spec" json:"spec"`
}

// JobBindingSpec specifies which jobs are available for a model
type JobBindingSpec struct {
	Model       string         `yaml:"model" json:"model"`           // Model name (helm, terraform, charts, etc)
	Jobs        []JobRef       `yaml:"jobs" json:"jobs"`             // List of available jobs
	DefaultJob  string         `yaml:"defaultJob" json:"defaultJob"` // Default job to execute
	Constraints JobConstraints `yaml:"constraints,omitempty" json:"constraints,omitempty"`
}

// JobRef is a reference to a job by name
type JobRef struct {
	Name     string `yaml:"name" json:"name"`
	Required bool   `yaml:"required,omitempty" json:"required,omitempty"` // Must be included in plan
}

// JobConstraints defines constraints for job execution
type JobConstraints struct {
	Platforms  []string `yaml:"platforms,omitempty" json:"platforms,omitempty"`   // kubernetes, docker, etc
	MinVersion string   `yaml:"minVersion,omitempty" json:"minVersion,omitempty"` // Minimum tool version
}

// JobInstance is a materialized job for a component in an environment
type JobInstance struct {
	ID                    string
	Name                  string
	Component             string
	Environment           string
	Composition           string
	Profile               string
	ProfileSource         string
	ProfileRuleTriggerRef string
	RunsOn                string
	Path                  string
	Steps                 []RenderedStep
	DependsOn             []string
	// AdvisoryDependsOn lists dependency job IDs that exist semantically
	// but are NOT enforced by the executor — used when DependencyMode is
	// "advisory". Kept for plan auditability ("api normally depends on
	// database, but for this trigger it ran in parallel").
	AdvisoryDependsOn []string
	// DependencyMode is the resolved enforcement policy for this job.
	DependencyMode string
	// DependencySource: "default", "environment", "subscription", or
	// "subscription-rule".
	DependencySource string
	// DependencyRuleTriggerRef: the matching triggerRef when source is
	// "subscription-rule".
	DependencyRuleTriggerRef string
	Gates                    []PromotionGate
	Timeout                  string
	Retries                  int
	Parameters               map[string]interface{}
	Env                      map[string]string
	// SecretRefs maps env var names to secret:// references (never values).
	// Resolved to plaintext only in the runner, at step launch.
	SecretRefs map[string]string
	// SecretBindings carries the resolved composition secretBindings for this
	// job's profile (specs/orun-secrets/data-model.md §2.2). The planner maps
	// each to a secret:// reference merged into SecretRefs; the list is kept for
	// plan auditability (which logical bindings a profile declared).
	SecretBindings []ResolvedSecretBinding
	// Materialize carries the profile's resolved runtime-delivery block for this
	// job (specs/orun-secrets/data-model.md §2.3). The planner subset-checks
	// materialize.secrets against the profile's bindings/secretEnv and translates
	// each to the env var name (AsEnv) the resolved value is keyed under, so the
	// runner delivers by that name after the deploy step. nil when the profile
	// declares no materialize block.
	Materialize *ResolvedMaterialize
	Labels      map[string]string
}

// ResolvedMaterialize is a profile's materialize block resolved onto a job:
// the typed adapter Target and the env var names (Secrets) to sync — a subset
// of the job's SecretRefs, value-free. The runner reads the already-resolved
// value for each name from its per-job secret cache (no second resolve).
type ResolvedMaterialize struct {
	Target  string
	Secrets []string
}

// ResolvedSecretBinding is a composition secretBinding resolved onto a job: the
// logical Key, the env var it injects as (AsEnv, defaulting to Key), and whether
// it is Required (a required binding that cannot be mapped is a compile error).
type ResolvedSecretBinding struct {
	Key      string
	AsEnv    string
	Required bool
}

// PromotionGate is an evidence check for cross-plan environment promotion.
type PromotionGate struct {
	Type        string
	Environment string
	Component   string
	Condition   string
	Match       map[string]string
}

// RenderedStep is a step with all templates resolved
type RenderedStep struct {
	ID               string                 `json:"id,omitempty"`
	Name             string                 `json:"name"`
	Phase            string                 `json:"phase,omitempty"`
	Order            int                    `json:"order,omitempty"`
	Run              string                 `json:"run,omitempty"`
	Use              string                 `json:"use,omitempty"`
	// Workflow is the rendered torkflow workflow reference; WorkflowDigest is the
	// content digest orun pinned for it at compile time (specs/orun-workflows §5).
	Workflow         string                 `json:"workflow,omitempty"`
	WorkflowDigest   string                 `json:"workflowDigest,omitempty"`
	With             map[string]interface{} `json:"with,omitempty"`
	Env              map[string]interface{} `json:"env,omitempty"`
	Shell            string                 `json:"shell,omitempty"`
	WorkingDirectory string                 `json:"workingDirectory,omitempty"`
	Timeout          string                 `json:"timeout"`
	Retry            int                    `json:"retry"`
	OnFailure        string                 `json:"onFailure"`
}

// JobGraph represents the logical DAG of all job instances
type JobGraph struct {
	Jobs  map[string]*JobInstance
	Edges map[string][]string // jobID -> [dependentJobIDs]
}
