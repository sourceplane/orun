package runbundle

// ShardRole identifies the type of a shard within an execution.
type ShardRole string

const (
	ShardRolePlan ShardRole = "plan"
	ShardRoleJob  ShardRole = "job"
)

// RunBundleShardManifest is embedded in every shard artifact.
// Portable across storage backends (GitHub, R2, S3, Orun Cloud).
type RunBundleShardManifest struct {
	APIVersion    string     `json:"apiVersion"`    // "orun.io/v1alpha1"
	Kind          string     `json:"kind"`           // "RunBundleShard"
	SchemaVersion string     `json:"schemaVersion"`  // "1.0.0"
	Role          ShardRole  `json:"role"`
	ExecID        string     `json:"execId"`
	PlanID        string     `json:"planId"`
	JobUID        string     `json:"jobUid,omitempty"`
	JobID         string     `json:"jobId,omitempty"`
	Component     string     `json:"component,omitempty"`
	Environment   string     `json:"environment,omitempty"`
	Composition   string     `json:"composition,omitempty"`
	Profile       string     `json:"profile,omitempty"`
	Status        string     `json:"status,omitempty"`   // job terminal status
	StartedAt     string     `json:"startedAt,omitempty"`
	FinishedAt    string     `json:"finishedAt,omitempty"`
	Source        ShardSource `json:"source,omitempty"`   // provenance
	Files         map[string]string `json:"files"`         // logical name -> relative path
}

// ShardSource describes where a shard originated.
type ShardSource struct {
	Type       string `json:"type"`       // "github-actions", "local", "r2", "s3"
	Repository string `json:"repository,omitempty"`
	RunID      string `json:"runId,omitempty"`
	RunAttempt string `json:"runAttempt,omitempty"`
	Workflow   string `json:"workflow,omitempty"`
	SHA        string `json:"sha,omitempty"`
	Ref        string `json:"ref,omitempty"`
	EventName  string `json:"eventName,omitempty"`
}

// Checksums maps file paths to hex digests.
type Checksums struct {
	Algorithm string            `json:"algorithm"` // "sha256"
	Files     map[string]string `json:"files"`      // relative path -> hex digest
}

// SynthesizedExecution is produced by hydrate for local .orun/ state.
type SynthesizedExecution struct {
	ExecID        string                 `json:"execId"`
	PlanID        string                 `json:"planId"`
	Status        string                 `json:"status"`  // "completed" | "failed" | "partial" | "cancelled"
	Partial       bool                   `json:"partial,omitempty"`
	PartialReason string                 `json:"partialReason,omitempty"`
	Counts        JobCounts              `json:"counts"`
	Jobs          map[string]JobShardRef `json:"jobs"`
	PlanShard     ShardRef               `json:"planShard"`
	Source        ShardSource            `json:"source,omitempty"`
	CreatedAt     string                 `json:"createdAt"`
}

// JobCounts summarizes job outcomes in a synthesized execution.
type JobCounts struct {
	Total     int `json:"total"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
	Cancelled int `json:"cancelled"`
	Skipped   int `json:"skipped"`
	Pending   int `json:"pending"`
}

// JobShardRef references a single job shard within a synthesized execution.
type JobShardRef struct {
	JobUid     string `json:"jobUid"`
	JobID      string `json:"jobId"`
	Status     string `json:"status"`
	ShardName  string `json:"shardName"`
	StartedAt  string `json:"startedAt,omitempty"`
	FinishedAt string `json:"finishedAt,omitempty"`
}

// ShardRef references a single shard (plan or job).
type ShardRef struct {
	Name   string `json:"name"`
	Role   string `json:"role"`
	PlanID string `json:"planId,omitempty"`
}