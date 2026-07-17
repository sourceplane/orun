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
	Name        string            `json:"name" yaml:"name"`
	Description string            `json:"description,omitempty" yaml:"description,omitempty"`
	Namespace   string            `json:"namespace,omitempty" yaml:"namespace,omitempty"`
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
}

// PlanSpec holds specification about the plan and its bindings
type PlanSpec struct {
	JobBindings        map[string]string           `json:"jobBindings,omitempty" yaml:"jobBindings,omitempty"` // model -> JobRegistry name mapping
	CompositionSources []ResolvedCompositionSource `json:"compositionSources,omitempty" yaml:"compositionSources,omitempty"`
	// WorkflowEngine is the declared engine pin materialized from
	// intent.execution.workflowEngine (orun-workflows-v2 §6). It folds into the
	// plan checksum; at run time the resolved engine's content digest must match
	// or the run fails closed. "Which engine ran this" is plan content.
	WorkflowEngine *PlanWorkflowEngine `json:"workflowEngine,omitempty" yaml:"workflowEngine,omitempty"`
}

// PlanWorkflowEngine is the in-plan workflow-engine pin.
type PlanWorkflowEngine struct {
	Ref    string `json:"ref,omitempty" yaml:"ref,omitempty"`
	Digest string `json:"digest" yaml:"digest"`
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
	ID                       string                 `json:"id" yaml:"id"`
	UID                      string                 `json:"uid,omitempty" yaml:"uid,omitempty"`
	DisplayName              string                 `json:"displayName,omitempty" yaml:"displayName,omitempty"`
	CheckName                string                 `json:"checkName,omitempty" yaml:"checkName,omitempty"`
	Name                     string                 `json:"name" yaml:"name"`
	Component                string                 `json:"component" yaml:"component"`
	Environment              string                 `json:"environment" yaml:"environment"`
	Composition              string                 `json:"composition,omitempty" yaml:"composition,omitempty"`
	Profile                  string                 `json:"profile,omitempty" yaml:"profile,omitempty"`
	ProfileSource            string                 `json:"profileSource,omitempty" yaml:"profileSource,omitempty"`
	ProfileRuleTriggerRef    string                 `json:"profileRuleTriggerRef,omitempty" yaml:"profileRuleTriggerRef,omitempty"`
	JobRegistry              string                 `json:"jobRegistry,omitempty" yaml:"jobRegistry,omitempty"` // Name of the JobRegistry used
	Job                      string                 `json:"job,omitempty" yaml:"job,omitempty"`                 // Specific job from registry
	RunsOn                   string                 `json:"runsOn,omitempty" yaml:"runsOn,omitempty"`
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
	Run   string `json:"run,omitempty" yaml:"run,omitempty"`
	Use   string `json:"use,omitempty" yaml:"use,omitempty"`
	// Workflow is the torkflow workflow file this step runs (the third execution
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
	OnFailure        string                       `json:"onFailure,omitempty" yaml:"onFailure,omitempty"`
}
