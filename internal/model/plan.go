package model

// Plan is the final execution-ready workflow DAG
type Plan struct {
	APIVersion string        `json:"apiVersion" yaml:"apiVersion"`
	Kind       string        `json:"kind" yaml:"kind"`
	Metadata   PlanMetadata  `json:"metadata" yaml:"metadata"`
	Execution  PlanExecution `json:"execution,omitempty" yaml:"execution,omitempty"`
	Spec       PlanSpec      `json:"spec,omitempty" yaml:"spec,omitempty"`
	Jobs       []PlanJob     `json:"jobs" yaml:"jobs"`
	// Lanes partitions Jobs into the units a runner takes. Present only when
	// the intent declares execution.lanes, so a lane-unaware plan stays
	// byte-identical (specs/orun-lanes §2, §4.6).
	Lanes []PlanLane `json:"lanes,omitempty" yaml:"lanes,omitempty"`
}

// PlanLane is one lane: jobs of one environment that one runner executes in
// one `orun run --lane` process (specs/orun-lanes §2).
type PlanLane struct {
	ID          string   `json:"id" yaml:"id"`
	Name        string   `json:"name" yaml:"name"`
	Index       int      `json:"index" yaml:"index"`
	Environment string   `json:"environment" yaml:"environment"`
	Jobs        []string `json:"jobs" yaml:"jobs"`
	Concurrency int      `json:"concurrency" yaml:"concurrency"`
	RunsOn      string   `json:"runsOn,omitempty" yaml:"runsOn,omitempty"`
	// Source is job | component | env | group | isolated | auto.
	Source string `json:"source" yaml:"source"`
	Group  string `json:"group,omitempty" yaml:"group,omitempty"`
	// SplitFrom and Reason record a cycle repair (specs/orun-lanes §7).
	SplitFrom string `json:"splitFrom,omitempty" yaml:"splitFrom,omitempty"`
	Reason    string `json:"reason,omitempty" yaml:"reason,omitempty"`
	// Level is the lane's depth in the lane graph: 0 for a lane no other
	// lane's job feeds, else one more than the deepest lane it waits on. A
	// workflow that runs the lanes level by level never provisions a runner
	// before its upstream lanes are done (specs/orun-runner-efficiency §5.1).
	Level int `json:"level" yaml:"level"`
	// UpstreamLanes are the lanes this lane's jobs wait on, and UpstreamJobs
	// the jobs in them, both sorted; what a pool gates a runner on.
	UpstreamLanes []string `json:"upstreamLanes,omitempty" yaml:"upstreamLanes,omitempty"`
	UpstreamJobs  []string `json:"upstreamJobs,omitempty" yaml:"upstreamJobs,omitempty"`
	// EstimatedSeconds is the sum of the lane's job weights when the plan
	// carries weights; VCPU the most cores one of its jobs declared. What a
	// pool sizes a runner by (specs/orun-runner-efficiency §5.4).
	EstimatedSeconds int `json:"estimatedSeconds,omitempty" yaml:"estimatedSeconds,omitempty"`
	VCPU             int `json:"vcpu,omitempty" yaml:"vcpu,omitempty"`
}

// PlanMetadata captures immutable plan generation details.
type PlanMetadata struct {
	Name        string `json:"name" yaml:"name"`
	Description string `json:"description,omitempty" yaml:"description,omitempty"`
	Namespace   string `json:"namespace,omitempty" yaml:"namespace,omitempty"`
	// GeneratedAt is volatile and not stamped by the renderer (plans are
	// byte-identical for identical inputs). It is kept for reading older
	// plans and is excluded from checksum and planHash.
	GeneratedAt string            `json:"generatedAt,omitempty" yaml:"generatedAt,omitempty"`
	Checksum    string            `json:"checksum,omitempty" yaml:"checksum,omitempty"`
	Scope       *PlanScope        `json:"scope,omitempty" yaml:"scope,omitempty"`
	Trigger     *PlanTrigger      `json:"trigger,omitempty" yaml:"trigger,omitempty"`
	Revision    *PlanRevisionMeta `json:"revision,omitempty" yaml:"revision,omitempty"`
	Source      *PlanSourceMeta   `json:"source,omitempty" yaml:"source,omitempty"`
	Catalog     *PlanCatalogMeta  `json:"catalog,omitempty" yaml:"catalog,omitempty"`
	Selection   *PlanSelection    `json:"selection,omitempty" yaml:"selection,omitempty"`
	// WorkDir is the intent directory path relative to the workspace root
	// (the directory where orun plan was invoked). orun run uses this when
	// intent auto-discovery fails (e.g. in GHA where the intent lives in a
	// subdirectory of GITHUB_WORKSPACE).
	WorkDir string `json:"workDir,omitempty" yaml:"workDir,omitempty"`
	// Lanes records how the plan's lanes were laid out; absent when the
	// intent sets no execution.lanes (the plan then has no lanes either).
	Lanes *PlanLanesMeta `json:"lanes,omitempty" yaml:"lanes,omitempty"`
}

// PlanLanesMeta is metadata.lanes: the effective mode and the lane budget
// auto mode was given, with where the budget came from — the intent's
// maxLanes, its default, or an override (`orun plan --max-lanes`,
// ORUN_MAX_LANES) that a plan reader would otherwise not see
// (specs/orun-runner-efficiency §5.2).
type PlanLanesMeta struct {
	Mode     string `json:"mode" yaml:"mode"`
	MaxLanes int    `json:"maxLanes" yaml:"maxLanes"`
	// MaxLanesFrom is intent | default | flag | env.
	MaxLanesFrom string `json:"maxLanesFrom" yaml:"maxLanesFrom"`
	// WeightsFrom says where the packing weights came from — declared (the
	// templates' estimatedSeconds), history, or file — and is absent when
	// every job weighs one.
	WeightsFrom string `json:"weightsFrom,omitempty" yaml:"weightsFrom,omitempty"`
	// UpstreamWait is execution.lanes.upstreamWait when set: how long a lane
	// job waits for an upstream in another lane.
	UpstreamWait string `json:"upstreamWait,omitempty" yaml:"upstreamWait,omitempty"`
}

// PlanTrigger records which trigger bindings activated the plan. Type / Name
// were added in M5 (cli-surface.md §1.3) so the embedded block in plan.json
// matches the canonical TriggerOccurrence shape (data-model.md §2.1) without
// requiring readers to consult revision.json.
type PlanTrigger struct {
	Type               string   `json:"type,omitempty" yaml:"type,omitempty"`
	Name               string   `json:"name,omitempty" yaml:"name,omitempty"`
	Mode               string   `json:"mode" yaml:"mode"`
	Provider           string   `json:"provider,omitempty" yaml:"provider,omitempty"`
	Event              string   `json:"event,omitempty" yaml:"event,omitempty"`
	Action             string   `json:"action,omitempty" yaml:"action,omitempty"`
	MatchedBindings    []string `json:"matchedBindings" yaml:"matchedBindings"`
	ActiveEnvironments []string `json:"activeEnvironments" yaml:"activeEnvironments"`
	Scope              string   `json:"scope" yaml:"scope"`
	Base               string   `json:"base,omitempty" yaml:"base,omitempty"`
	Head               string   `json:"head,omitempty" yaml:"head,omitempty"`
}

// PlanRevisionMeta is the in-plan reference to the canonical PlanRevision the
// compiled plan was persisted under (cli-surface.md §1.3). Populated by the
// CLI after triggerctx + revision.WriteRevision derive the revisionKey.
type PlanRevisionMeta struct {
	Key      string `json:"key" yaml:"key"`
	PlanHash string `json:"planHash" yaml:"planHash"`
}

// PlanSourceMeta records the workspace VCS state the plan was generated from.
// Populated from the SourceSnapshot resolved during catalog integration (C6).
type PlanSourceMeta struct {
	SnapshotKey  string `json:"snapshotKey" yaml:"snapshotKey"`
	Ref          string `json:"ref,omitempty" yaml:"ref,omitempty"`
	HeadRevision string `json:"headRevision,omitempty" yaml:"headRevision,omitempty"`
	TreeHash     string `json:"treeHash,omitempty" yaml:"treeHash,omitempty"`
	WorkingTree  string `json:"workingTree,omitempty" yaml:"workingTree,omitempty"`
	DirtyHash    string `json:"dirtyHash,omitempty" yaml:"dirtyHash,omitempty"`
}

// PlanCatalogMeta records which catalog snapshot the plan was compiled against.
// Populated from the CatalogSnapshot resolved during catalog integration (C6).
// When Skipped is true, the catalog was deliberately bypassed
// (--no-catalog-refresh) and other fields are empty.
type PlanCatalogMeta struct {
	SnapshotKey       string `json:"snapshotKey,omitempty" yaml:"snapshotKey,omitempty"`
	CatalogHash       string `json:"catalogHash,omitempty" yaml:"catalogHash,omitempty"`
	SourceSnapshotKey string `json:"sourceSnapshotKey,omitempty" yaml:"sourceSnapshotKey,omitempty"`
	Skipped           bool   `json:"skipped" yaml:"skipped"`
}

// PlanScope records the component scoping applied when the plan was generated.
type PlanScope struct {
	DetectedComponent string   `json:"detectedComponent" yaml:"detectedComponent"`
	Components        []string `json:"components" yaml:"components"`
}

// PlanSelection records the environment/component selection that scoped this
// plan (env-scoping "Z" model — specs/orun-env-scoping/data-model.md §2).
// It is a deterministic function of the selection inputs and folds into the
// plan hash like the rest of metadata. Mode is "full" when no narrowing was
// applied, else "scoped". AllEnvs is true when an explicit --all-envs was
// passed (vs. the implicit all-environments default), letting `run`
// distinguish a deliberate all-env plan from an unscoped one.
type PlanSelection struct {
	Envs       []string `json:"envs" yaml:"envs"`
	Components []string `json:"components" yaml:"components"`
	Mode       string   `json:"mode" yaml:"mode"`
	AllEnvs    bool     `json:"allEnvs" yaml:"allEnvs"`
	// PrunedEdges records dependency edges dropped because their endpoint was
	// not in the selection (env-scoping ES2). Empty for a full plan.
	PrunedEdges []PrunedEdge `json:"prunedEdges,omitempty" yaml:"prunedEdges,omitempty"`
}

// PrunedEdge is a dependency edge dropped because one endpoint is not in the
// expanded plan (env-scoping data-model.md §3). Kind is "promotion" or
// "component"; From is always in the plan, To is the dropped endpoint.
type PrunedEdge struct {
	Kind   string `json:"kind" yaml:"kind"`
	From   string `json:"from" yaml:"from"`
	To     string `json:"to" yaml:"to"`
	Reason string `json:"reason" yaml:"reason"`
}

// PlanExecution defines runtime behavior for plan execution.
type PlanExecution struct {
	Concurrency int    `json:"concurrency,omitempty" yaml:"concurrency,omitempty"`
	FailFast    bool   `json:"failFast" yaml:"failFast"`
	StateFile   string `json:"stateFile,omitempty" yaml:"stateFile,omitempty"`
	// Timeout is execution.defaults.timeout: the bound on every step whose
	// job and step set none; absent, the runner applies its 60m default.
	Timeout string `json:"timeout,omitempty" yaml:"timeout,omitempty"`
}

// PlanSpec holds specification about the plan and its bindings
type PlanSpec struct {
	JobBindings        map[string]string           `json:"jobBindings,omitempty" yaml:"jobBindings,omitempty"` // model -> JobRegistry name mapping
	CompositionSources []ResolvedCompositionSource `json:"compositionSources,omitempty" yaml:"compositionSources,omitempty"`
}

// ResolvedCompositionSource records which sources were used to compile the plan.
type ResolvedCompositionSource struct {
	Name           string   `json:"name" yaml:"name"`
	Kind           string   `json:"kind" yaml:"kind"`
	Ref            string   `json:"ref,omitempty" yaml:"ref,omitempty"`
	Path           string   `json:"path,omitempty" yaml:"path,omitempty"`
	ResolvedDigest string   `json:"resolvedDigest" yaml:"resolvedDigest"`
	Exports        []string `json:"exports,omitempty" yaml:"exports,omitempty"`
}

// PlanJob is the execution unit in the final plan
type PlanJob struct {
	ID                    string `json:"id" yaml:"id"`
	UID                   string `json:"uid,omitempty" yaml:"uid,omitempty"`
	DisplayName           string `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	CheckName             string `json:"checkName,omitempty" yaml:"checkName,omitempty"`
	Name                  string `json:"name" yaml:"name"`
	Component             string `json:"component" yaml:"component"`
	Environment           string `json:"environment" yaml:"environment"`
	Composition           string `json:"composition,omitempty" yaml:"composition,omitempty"`
	Profile               string `json:"profile,omitempty" yaml:"profile,omitempty"`
	ProfileSource         string `json:"profileSource,omitempty" yaml:"profileSource,omitempty"`
	ProfileRuleTriggerRef string `json:"profileRuleTriggerRef,omitempty" yaml:"profileRuleTriggerRef,omitempty"`
	JobRegistry           string `json:"jobRegistry,omitempty" yaml:"jobRegistry,omitempty"` // Name of the JobRegistry used
	Job                   string `json:"job,omitempty" yaml:"job,omitempty"`                 // Specific job from registry
	RunsOn                string `json:"runsOn,omitempty" yaml:"runsOn,omitempty"`
	// Resources is what the job's template declared it needs; EstimatedSeconds
	// is the job's effective packing weight — the declared estimate, or the
	// one `orun plan --weights` supplied (specs/orun-runner-efficiency §5.4).
	Resources        *JobResources `json:"resources,omitempty" yaml:"resources,omitempty"`
	EstimatedSeconds int           `json:"estimatedSeconds,omitempty" yaml:"estimatedSeconds,omitempty"`
	// Index is the job's position in Jobs: a stable, dense 0-based number for
	// one plan. CI composes a per-job runner label from it
	// (ghr-orun-job:<run>-<index>), so a runner launched for a lane can take
	// only that lane; the platform maps the label back through the plan.
	Index int `json:"index" yaml:"index"`
	// Lane is the id of the lane this job runs in; empty on a lane-unaware
	// plan (specs/orun-lanes §2).
	Lane                     string                 `json:"lane,omitempty" yaml:"lane,omitempty"`
	Path                     string                 `json:"path,omitempty" yaml:"path,omitempty"` // Working directory for job execution
	Steps                    []PlanStep             `json:"steps" yaml:"steps"`
	DependsOn                []string               `json:"dependsOn,omitempty" yaml:"dependsOn,omitempty"`
	AdvisoryDependsOn        []string               `json:"advisoryDependsOn,omitempty" yaml:"advisoryDependsOn,omitempty"`
	DependencyMode           string                 `json:"dependencyMode,omitempty" yaml:"dependencyMode,omitempty"`
	DependencySource         string                 `json:"dependencySource,omitempty" yaml:"dependencySource,omitempty"`
	DependencyRuleTriggerRef string                 `json:"dependencyRuleTriggerRef,omitempty" yaml:"dependencyRuleTriggerRef,omitempty"`
	Gates                    []PlanPromotionGate    `json:"gates,omitempty" yaml:"gates,omitempty"`
	Timeout                  string                 `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Retries                  int                    `json:"retries,omitempty" yaml:"retries,omitempty"`
	Env                      map[string]interface{} `json:"env,omitempty" yaml:"env,omitempty"`
	// SecretRefs lists the secret:// references this job will resolve at run
	// time — references only, sorted by AsEnv. No value field exists,
	// structurally (specs/orun-secrets/data-model.md §5).
	SecretRefs []PlanSecretRef `json:"secretRefs,omitempty" yaml:"secretRefs,omitempty"`
	// Materialize renders the profile's runtime-delivery block as an explicit
	// plan step (specs/orun-secrets/data-model.md §2.3, §5). Value-free — the
	// target adapter id and the key names only; it folds into the plan checksum.
	Materialize *PlanMaterialize       `json:"materialize,omitempty" yaml:"materialize,omitempty"`
	Labels      map[string]string      `json:"labels,omitempty" yaml:"labels,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty" yaml:"parameters,omitempty"`
}

// PlanMaterialize is the value-free materialize step in the plan: the typed
// adapter Target and the secret key names to deliver after the deploy step.
// No value field exists — structurally (Invariant 1).
type PlanMaterialize struct {
	Target  string   `json:"target" yaml:"target"`
	Secrets []string `json:"secrets,omitempty" yaml:"secrets,omitempty"`
}

// PlanSecretRef is one secret reference on a plan job: the env var name the
// resolved value will be injected as, and the reference that names it. Later
// milestones add compile-time annotations (grant, servesFrom, personalShadow)
// — never values.
type PlanSecretRef struct {
	AsEnv string `json:"asEnv" yaml:"asEnv"`
	Ref   string `json:"ref" yaml:"ref"`
	// Optional marks a best-effort reference: a key absent from the backend
	// is skipped at resolve (no env var) instead of failing the job. Hard
	// references (the default) stay fail-closed.
	Optional bool `json:"optional,omitempty" yaml:"optional,omitempty"`
}

// PlanPromotionGate is a cross-plan evidence gate in the plan output.
type PlanPromotionGate struct {
	Type        string            `json:"type" yaml:"type"`
	Environment string            `json:"environment" yaml:"environment"`
	Component   string            `json:"component" yaml:"component"`
	Condition   string            `json:"condition" yaml:"condition"`
	Match       map[string]string `json:"match" yaml:"match"`
}

// PlanStep is a step in the final plan
type PlanStep struct {
	ID    string `json:"id,omitempty" yaml:"id,omitempty"`
	Name  string `json:"name,omitempty" yaml:"name,omitempty"`
	Phase string `json:"phase,omitempty" yaml:"phase,omitempty"`
	Order int    `json:"order,omitempty" yaml:"order,omitempty"`
	// Scope is lane for a run step the first job of a lane runs once in the
	// shared tree for every job that shares it (specs/orun-runner-efficiency
	// §5.3); absent is job.
	Scope string `json:"scope,omitempty" yaml:"scope,omitempty"`
	Run   string `json:"run,omitempty" yaml:"run,omitempty"`
	Use   string `json:"use,omitempty" yaml:"use,omitempty"`
	// Workflow is the workflow file this step runs (the third execution
	// vocabulary beside run/use); WorkflowDigest is the content digest pinned at
	// compile time so the reference — never the outcome — is durable plan state
	// (specs/orun-workflows §5/§7). Both fold into the plan checksum.
	Workflow       string `json:"workflow,omitempty" yaml:"workflow,omitempty"`
	WorkflowDigest string `json:"workflowDigest,omitempty" yaml:"workflowDigest,omitempty"`
	// Connections is the compile-checked credential grant for a workflow step
	// (orun-workflows-v2 §4): connection name → field → secret:// reference.
	// Names and references only, never values — the plan IS the reviewable
	// grant, and only mapped references cross to the engine at run time.
	Connections      map[string]map[string]string `json:"connections,omitempty" yaml:"connections,omitempty"`
	With             map[string]interface{}       `json:"with,omitempty" yaml:"with,omitempty"`
	Env              map[string]interface{}       `json:"env,omitempty" yaml:"env,omitempty"`
	Shell            string                       `json:"shell,omitempty" yaml:"shell,omitempty"`
	WorkingDirectory string                       `json:"working-directory,omitempty" yaml:"working-directory,omitempty"`
	Timeout          string                       `json:"timeout,omitempty" yaml:"timeout,omitempty"`
	Retry            int                          `json:"retry,omitempty" yaml:"retry,omitempty"`
	// Resume opts a workflow step into resume-from-failed-step on retry
	// (orun-workflows-v2 §8).
	Resume bool `json:"resume,omitempty" yaml:"resume,omitempty"`
	// Approval is the declared human gate (orun-workflows-v2 §9): the
	// declaration is plan content; the pause and verdict are sealed run facts.
	Approval  *StepApproval `json:"approval,omitempty" yaml:"approval,omitempty"`
	OnFailure string        `json:"onFailure,omitempty" yaml:"onFailure,omitempty"`
}
