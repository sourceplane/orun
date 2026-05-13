package model

import "strings"

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
	Name        string                 `yaml:"name" json:"name"`
	Description string                 `yaml:"description" json:"description"`
	RunsOn      string                 `yaml:"runsOn,omitempty" json:"runsOn,omitempty"`
	Timeout     string                 `yaml:"timeout" json:"timeout"`
	Retries     int                    `yaml:"retries" json:"retries"`
	Steps       []Step                 `yaml:"steps" json:"steps"`
	Inputs      map[string]interface{} `yaml:"inputs" json:"inputs"`
	Labels      map[string]string      `yaml:"labels" json:"labels"`
}

// Step is a single execution unit within a job
type Step struct {
	ID               string                 `yaml:"id,omitempty" json:"id,omitempty"`
	Name             string                 `yaml:"name" json:"name"`
	Phase            string                 `yaml:"phase,omitempty" json:"phase,omitempty"`
	Order            int                    `yaml:"order,omitempty" json:"order,omitempty"`
	Run              string                 `yaml:"run,omitempty" json:"run,omitempty"`
	Use              string                 `yaml:"use,omitempty" json:"use,omitempty"`
	With             map[string]interface{} `yaml:"with,omitempty" json:"with,omitempty"`
	Env              map[string]interface{} `yaml:"env,omitempty" json:"env,omitempty"`
	Shell            string                 `yaml:"shell,omitempty" json:"shell,omitempty"`
	WorkingDirectory string                 `yaml:"working-directory,omitempty" json:"working-directory,omitempty"`
	Timeout          string                 `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	Retry            int                    `yaml:"retry,omitempty" json:"retry,omitempty"`
	OnFailure        string                 `yaml:"onFailure,omitempty" json:"onFailure,omitempty"` // stop, continue
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
	ID          string
	Name        string
	Component   string
	Environment string
	Composition string
	RunsOn      string
	Path        string
	Steps       []RenderedStep
	DependsOn   []string
	Timeout     string
	Retries     int
	Config      map[string]interface{} // Single source of truth for env vars
	Labels      map[string]string
}

// RenderedStep is a step with all templates resolved
type RenderedStep struct {
	ID               string                 `json:"id,omitempty"`
	Name             string                 `json:"name"`
	Phase            string                 `json:"phase,omitempty"`
	Order            int                    `json:"order,omitempty"`
	Run              string                 `json:"run,omitempty"`
	Use              string                 `json:"use,omitempty"`
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
