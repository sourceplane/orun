package model

// Plan is the final execution-ready workflow DAG
type Plan struct {
	APIVersion string        `json:"apiVersion" yaml:"apiVersion"`
	Kind       string        `json:"kind" yaml:"kind"`
	Metadata   PlanMetadata  `json:"metadata" yaml:"metadata"`
	Execution  PlanExecution `json:"execution,omitempty" yaml:"execution,omitempty"`
	Spec       PlanSpec      `json:"spec,omitempty" yaml:"spec,omitempty"`
	Jobs       []PlanJob     `json:"jobs" yaml:"jobs"`
}

// PlanMetadata captures immutable plan generation details.
type PlanMetadata struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Namespace   string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	GeneratedAt string `json:"generatedAt,omitempty" yaml:"generatedAt,omitempty"`
	Checksum    string `json:"checksum,omitempty" yaml:"checksum,omitempty"`
}

// PlanExecution defines runtime behavior for plan execution.
type PlanExecution struct {
	Concurrency int    `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
	FailFast    bool   `json:"failFast" yaml:"failFast"`
	StateFile   string `json:"stateFile,omitempty" yaml:"stateFile,omitempty"`
}

// PlanSpec holds specification about the plan and its bindings
type PlanSpec struct {
	JobBindings map[string]string `json:"jobBindings,omitempty" yaml:"jobBindings,omitempty"` // model -> JobRegistry name mapping
}

// PlanJob is the execution unit in the final plan
type PlanJob struct {
	ID          string                 `json:"id" yaml:"id"`
	Name        string                 `json:"name" yaml:"name"`
	Component   string                 `json:"component" yaml:"component"`
	Environment string                 `json:"environment" yaml:"environment"`
	Composition string                 `json:"composition,omitempty" yaml:"composition,omitempty"`
	JobRegistry string                 `json:"jobRegistry,omitempty" yaml:"jobRegistry,omitempty"` // Name of the JobRegistry used
	Job         string                 `json:"job,omitempty" yaml:"job,omitempty"`                 // Specific job from registry
	RunsOn      string                 `json:"runsOn,omitempty" yaml:"runsOn,omitempty"`
	Path        string                 `json:"path,omitempty" yaml:"path,omitempty"` // Working directory for job execution
	Steps       []PlanStep             `json:"steps" yaml:"steps"`
	DependsOn   []string               `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	Timeout     string                 `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Retries     int                    `json:"retries,omitempty" yaml:"retries,omitempty"`
	Env         map[string]interface{} `json:"env,omitempty" yaml:"env,omitempty"`
	Labels      map[string]string      `json:"labels,omitempty" yaml:"labels,omitempty"`
	Config      map[string]interface{} `json:"config,omitempty" yaml:"config,omitempty"`
}

// PlanStep is a step in the final plan
type PlanStep struct {
	ID        string `json:"id,omitempty" yaml:"id,omitempty"`
	Name      string `json:"name,omitempty" yaml:"name,omitempty"`
	Phase     string `json:"phase,omitempty" yaml:"phase,omitempty"`
	Order     int    `json:"order,omitempty" yaml:"order,omitempty"`
	Run       string `json:"run" yaml:"run"`
	Timeout   string `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Retry     int    `json:"retry,omitempty" yaml:"retry,omitempty"`
	OnFailure string `json:"onFailure,omitempty" yaml:"onFailure,omitempty"`
}
