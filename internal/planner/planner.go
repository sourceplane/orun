package planner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"text/template"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/workflowbackend"
)

// JobPlanner binds components to jobs and creates instances
type JobPlanner struct {
	compositions  map[string]*CompositionInfo // Composition -> default job info
	templateCache map[string]*template.Template
	// Workspace and Project are the tenancy scope composition secretBindings map
	// against (specs/orun-secrets/data-model.md §2.2): a binding for KEY becomes
	// secret://<workspace>/<project>/<env>/<KEY>. Empty when no cloud scope is
	// resolvable at plan time — a required binding then fails to compile.
	Workspace string
	Project   string
	// WorkflowBaseDir is the directory `workflow:` step references resolve against
	// when orun pins their content digest at compile time (specs/orun-workflows
	// §5). Set to the intent file's directory by the CLI. When empty, a workflow:
	// step still compiles (reference + with are materialized) but no digest is
	// pinned — the digest folds in once a base dir is available.
	WorkflowBaseDir string
}

// CompositionInfo holds the default job for a composition
type CompositionInfo struct {
	Type              string
	DefaultJob        *model.JobSpec
	ExecutionProfiles map[string]model.ExecutionProfile
	JobMap            map[string]*model.JobSpec
	// SourceDir is the resolved on-disk root of the composition source this
	// composition came from (orun-workflows-v2 §7). A `workflow:` reference that
	// does not resolve in the intent directory falls back here, so a golden path
	// can ship its workflows in the same Stack — and the resolved bytes are
	// materialized into the workspace so the plan pin is machine-portable.
	SourceDir string
}

// NewJobPlanner creates a new job planner from a composition registry
func NewJobPlanner(compositions map[string]*CompositionInfo) *JobPlanner {
	return &JobPlanner{
		compositions:  compositions,
		templateCache: make(map[string]*template.Template),
	}
}

// PlanJobs creates job instances from component instances
func (jp *JobPlanner) PlanJobs(instances map[string][]*model.ComponentInstance) (map[string]*model.JobInstance, error) {
	jobInstances := make(map[string]*model.JobInstance)

	for envName, envInstances := range instances {
		for _, compInst := range envInstances {
			// Get job definition for this component type
			compositionKey := compInst.ResolvedComposition
			if compositionKey == "" {
				compositionKey = compInst.Type
			}

			compositionInfo, exists := jp.compositions[compositionKey]
			if !exists {
				return nil, fmt.Errorf("no job definition for composition: %s", compositionKey)
			}

			// Determine which jobs to plan based on profile
			jobsToRender, err := jp.resolveJobsForProfile(compInst, compositionInfo)
			if err != nil {
				return nil, err
			}

			// The composition secretBindings for this instance's profile are the
			// same for every job the profile selects; resolve them once.
			bindings := jp.resolveProfileBindings(compInst, compositionInfo)

			// The profile's materialize block (deploy-time last mile) is likewise
			// profile-level; resolve + subset-check it once. A non-subset key is a
			// compile error (specs/orun-secrets/data-model.md §2.3).
			materialize, matErr := resolveMaterialize(jp.resolveProfileMaterialize(compInst, compositionInfo), bindings, compInst.SecretEnv)
			if matErr != nil {
				return nil, fmt.Errorf("component %s: %w", compInst.ComponentName, matErr)
			}

			for _, jobEntry := range jobsToRender {
				jobID := fmt.Sprintf("%s.%s.%s", compInst.ComponentName, envName, jobEntry.job.Name)
				secretRefs, mergeErr := mergeBindingRefs(compInst.SecretEnv, bindings, jp.Workspace, jp.Project, envName)
				if mergeErr != nil {
					return nil, fmt.Errorf("component %s (env %s): %w", compInst.ComponentName, envName, mergeErr)
				}
				jobInst := &model.JobInstance{
					ID:                       jobID,
					Name:                     jobEntry.job.Name,
					Component:                compInst.ComponentName,
					Environment:              envName,
					Composition:              compInst.Type,
					Profile:                  compInst.ProfileRef,
					ProfileSource:            compInst.ProfileSource,
					ProfileRuleTriggerRef:    compInst.ProfileRuleTriggerRef,
					DependencyMode:           compInst.DependencyMode,
					DependencySource:         compInst.DependencySource,
					DependencyRuleTriggerRef: compInst.DependencyRuleTriggerRef,
					RunsOn:                   jobEntry.job.RunsOn,
					Path:                     compInst.Path,
					Timeout:                  jobEntry.job.Timeout,
					Retries:                  jobEntry.job.Retries,
					Labels:                   compInst.Labels,
					Parameters:               compInst.Parameters,
					Env:                      compInst.Env,
					SecretRefs:               secretRefs,
					SecretBindings:           bindings,
					Materialize:              materialize,
					DependsOn:                make([]string, 0),
				}

				resolvedSteps := applyStepOverrides(jobEntry.steps, compInst.StepOverrides)

				tctx := &TemplateContext{
					CompInst: compInst,
					JobID:    jobID,
					JobName:  jobEntry.job.Name,
				}
				renderedSteps, err := jp.renderSteps(resolvedSteps, tctx, compositionInfo.SourceDir)
				if err != nil {
					return nil, fmt.Errorf("failed to render steps for job %s: %w", jobID, err)
				}
				jobInst.Steps = renderedSteps

				jobInstances[jobID] = jobInst
			}
		}
	}

	// Resolve job dependencies
	err := jp.resolveDependencies(jobInstances, instances)
	if err != nil {
		return nil, err
	}

	return jobInstances, nil
}

type jobRenderEntry struct {
	job   *model.JobSpec
	steps []model.Step
}

func (jp *JobPlanner) resolveJobsForProfile(compInst *model.ComponentInstance, info *CompositionInfo) ([]jobRenderEntry, error) {
	// Legacy: no profile means use default job with all steps
	if compInst.ProfileSource == "" || compInst.ProfileSource == "legacy-none" {
		jobDef := info.DefaultJob
		if jobDef == nil {
			return nil, fmt.Errorf("no default job defined for composition: %s", compInst.Type)
		}
		return []jobRenderEntry{{job: jobDef, steps: jobDef.Steps}}, nil
	}

	// Profile-based: filter jobs and steps
	if len(info.ExecutionProfiles) == 0 {
		jobDef := info.DefaultJob
		if jobDef == nil {
			return nil, fmt.Errorf("no default job defined for composition: %s", compInst.Type)
		}
		return []jobRenderEntry{{job: jobDef, steps: jobDef.Steps}}, nil
	}

	profile, exists := info.ExecutionProfiles[compInst.ProfileName]
	if !exists {
		return nil, fmt.Errorf("profile %q not found in composition %s", compInst.ProfileName, compInst.Type)
	}

	entries := make([]jobRenderEntry, 0, len(profile.Jobs))
	for jobName, profileJob := range profile.Jobs {
		baseJob, exists := info.JobMap[jobName]
		if !exists {
			return nil, fmt.Errorf("profile references unknown job %q in composition %s", jobName, compInst.Type)
		}

		var filteredSteps []model.Step
		if len(profileJob.IncludeCapabilities) > 0 {
			filteredSteps = filterStepsByCapability(baseJob.Steps, profileJob.IncludeCapabilities)
		} else {
			filteredSteps = filterStepsByProfile(baseJob.Steps, profileJob.StepsEnabled)
		}

		if len(profileJob.StepOverrides) > 0 {
			filteredSteps = applyProfileStepOverrides(filteredSteps, profileJob.StepOverrides)
		}

		entries = append(entries, jobRenderEntry{job: baseJob, steps: filteredSteps})
	}

	return entries, nil
}

func filterStepsByProfile(baseSteps []model.Step, stepsEnabled []string) []model.Step {
	enabledSet := make(map[string]struct{}, len(stepsEnabled))
	for _, sid := range stepsEnabled {
		enabledSet[sid] = struct{}{}
	}

	filtered := make([]model.Step, 0, len(stepsEnabled))
	for _, step := range baseSteps {
		sid := step.ID
		if sid == "" {
			sid = step.Name
		}
		if _, enabled := enabledSet[sid]; enabled {
			filtered = append(filtered, step)
		}
	}
	return filtered
}

func filterStepsByCapability(baseSteps []model.Step, capabilities []string) []model.Step {
	capSet := make(map[string]struct{}, len(capabilities))
	for _, cap := range capabilities {
		capSet[cap] = struct{}{}
	}

	filtered := make([]model.Step, 0, len(baseSteps))
	for _, step := range baseSteps {
		if step.Capability == "" {
			filtered = append(filtered, step)
			continue
		}
		if _, included := capSet[step.Capability]; included {
			filtered = append(filtered, step)
		}
	}
	return filtered
}

func applyProfileStepOverrides(steps []model.Step, overrides map[string]model.ProfileStepPatch) []model.Step {
	result := make([]model.Step, 0, len(steps))
	for _, step := range steps {
		sid := step.ID
		if sid == "" {
			sid = step.Name
		}
		if patch, exists := overrides[sid]; exists {
			patched := step
			if patch.Run != "" {
				patched.Run = patch.Run
			}
			if len(patch.With) > 0 {
				patched.With = patch.With
			}
			if len(patch.Env) > 0 {
				patched.Env = patch.Env
			}
			result = append(result, patched)
		} else {
			result = append(result, step)
		}
	}
	return result
}

// Templates are cached to avoid re-parsing identical steps across multiple instances
func (jp *JobPlanner) renderSteps(steps []model.Step, tctx *TemplateContext, compositionSourceDir string) ([]model.RenderedStep, error) {
	rendered := make([]model.RenderedStep, 0, len(steps))
	sortedSteps, err := sortStepsByPhaseAndOrder(steps)
	if err != nil {
		return nil, err
	}

	context := tctx.Build()

	compType := tctx.CompInst.Type
	// Declared outputs of this job's workflow steps, keyed by step id and name —
	// the compile-time source of truth for ${{ steps.X.outputs.Y }} references
	// (orun-workflows-v2 §5). Populated only when inspection ran (base dir set).
	declaredOutputs := map[string]map[string]struct{}{}
	inspected := jp.WorkflowBaseDir != ""
	for _, step := range sortedSteps {
		// A step is exactly one of run/use/workflow (orun-workflows §3). Fail
		// compilation before rendering when more than one is set.
		if err := step.ValidateExecForm(); err != nil {
			return nil, err
		}
		renderedRun, err := jp.renderTemplateString(compType, step.Name, "run", step.Run, context)
		if err != nil {
			return nil, err
		}
		renderedUse, err := jp.renderTemplateString(compType, step.Name, "use", step.Use, context)
		if err != nil {
			return nil, err
		}
		renderedWorkflow, err := jp.renderTemplateString(compType, step.Name, "workflow", step.Workflow, context)
		if err != nil {
			return nil, err
		}
		renderedWorkflow, workflowDigest, inspection, err := jp.pinWorkflow(step.Name, renderedWorkflow, compositionSourceDir)
		if err != nil {
			return nil, err
		}
		renderedConnections, err := jp.renderConnections(compType, step.Name, step.Connections, inspection, workflowDigest != "", context)
		if err != nil {
			return nil, err
		}
		if renderedWorkflow != "" && workflowDigest != "" {
			outs := make(map[string]struct{}, len(inspection.Outputs))
			for _, name := range inspection.Outputs {
				outs[name] = struct{}{}
			}
			for _, key := range []string{step.ID, step.Name} {
				if key != "" {
					declaredOutputs[key] = outs
				}
			}
		}
		renderedShell, err := jp.renderTemplateString(compType, step.Name, "shell", step.Shell, context)
		if err != nil {
			return nil, err
		}
		renderedWorkingDirectory, err := jp.renderTemplateString(compType, step.Name, "working-directory", step.WorkingDirectory, context)
		if err != nil {
			return nil, err
		}
		renderedWith, err := jp.renderTemplateMap(compType, step.Name, "with", step.With, context)
		if err != nil {
			return nil, err
		}
		renderedEnv, err := jp.renderTemplateMap(compType, step.Name, "env", step.Env, context)
		if err != nil {
			return nil, err
		}

		rendered = append(rendered, model.RenderedStep{
			ID:               step.ID,
			Name:             step.Name,
			Phase:            model.NormalizePhase(step.Phase),
			Order:            step.Order,
			Run:              renderedRun,
			Use:              renderedUse,
			Workflow:         renderedWorkflow,
			WorkflowDigest:   workflowDigest,
			Connections:      renderedConnections,
			With:             renderedWith,
			Env:              renderedEnv,
			Shell:            renderedShell,
			WorkingDirectory: renderedWorkingDirectory,
			Timeout:          step.Timeout,
			Retry:            step.Retry,
			Resume:           step.Resume,
			OnFailure:        step.OnFailure,
		})
	}

	if inspected {
		if err := validateOutputRefs(rendered, declaredOutputs); err != nil {
			return nil, err
		}
	}

	return rendered, nil
}

// validateOutputRefs checks every ${{ steps.X.outputs.Y }} reference in the
// job's rendered steps against the declared outputs of its workflow steps
// (orun-workflows-v2 §5): the referenced step must be an EARLIER workflow step
// of the same job (cross-job references are rejected structurally — S-4), and
// the name must be declared in its pinned file. Fail-closed at compile time.
func validateOutputRefs(rendered []model.RenderedStep, declared map[string]map[string]struct{}) error {
	available := map[string]map[string]struct{}{}
	for _, step := range rendered {
		blob, err := json.Marshal(step)
		if err != nil {
			return err
		}
		for _, ref := range workflowbackend.FindOutputRefs(string(blob)) {
			outs, ok := available[ref.StepID]
			if !ok {
				return fmt.Errorf("step %q references ${{ steps.%s.outputs.%s }}, but %q is not an earlier workflow step in this job", step.Name, ref.StepID, ref.Name, ref.StepID)
			}
			if _, ok := outs[ref.Name]; !ok {
				return fmt.Errorf("step %q references ${{ steps.%s.outputs.%s }}, but the workflow declares no output %q (spec.outputs)", step.Name, ref.StepID, ref.Name, ref.Name)
			}
		}
		for _, key := range []string{step.ID, step.Name} {
			if key == "" {
				continue
			}
			if outs, ok := declared[key]; ok {
				available[key] = outs
			}
		}
	}
	return nil
}

// pinWorkflow resolves a rendered `workflow:` reference, returning the
// (possibly rewritten) reference, its content digest, and the compile-time
// inspection of its declared names (orun-workflows §5, v2 §4/§5/§7).
//
// Resolution order: the intent directory (WorkflowBaseDir), then the
// composition's resolved source root — so a golden path can ship its workflows
// in the same Stack (§7). A workflow resolved from a packaged source is
// materialized into the workspace at a content-addressed path
// (.orun/workflows/<digest12>-<name>) and the reference is rewritten to it:
// the digest-derived name keeps the plan byte-identical across machines, and
// the runner finds the file inside the workspace tree. One digest function over
// the resolved bytes — the pin is source-agnostic (S-7).
//
// An empty reference or unset base dir yields zero values; an unresolvable or
// unparseable reference is a compile error, fail-closed.
func (jp *JobPlanner) pinWorkflow(stepName, workflow, sourceDir string) (string, string, workflowbackend.Inspection, error) {
	workflow = strings.TrimSpace(workflow)
	if workflow == "" || jp.WorkflowBaseDir == "" {
		return workflow, "", workflowbackend.Inspection{}, nil
	}
	localPath := workflow
	if !filepath.IsAbs(localPath) {
		localPath = filepath.Join(jp.WorkflowBaseDir, workflow)
	}
	data, err := os.ReadFile(localPath)
	if err != nil && sourceDir != "" && !filepath.IsAbs(workflow) {
		if pdata, perr := os.ReadFile(filepath.Join(sourceDir, workflow)); perr == nil {
			data, err = pdata, nil
			digest := workflowbackend.DigestBytes(data)
			rel := filepath.ToSlash(filepath.Join(".orun", "workflows", digestShort(digest)+"-"+filepath.Base(workflow)))
			dest := filepath.Join(jp.WorkflowBaseDir, filepath.FromSlash(rel))
			if merr := os.MkdirAll(filepath.Dir(dest), 0o755); merr != nil {
				return "", "", workflowbackend.Inspection{}, fmt.Errorf("step %q: materialize packaged workflow: %w", stepName, merr)
			}
			if werr := os.WriteFile(dest, data, 0o644); werr != nil {
				return "", "", workflowbackend.Inspection{}, fmt.Errorf("step %q: materialize packaged workflow: %w", stepName, werr)
			}
			workflow = rel
		}
	}
	if err != nil {
		return "", "", workflowbackend.Inspection{}, fmt.Errorf("step %q: workflow %q: %w", stepName, workflow, err)
	}
	insp, ierr := workflowbackend.InspectWorkflow(data)
	if ierr != nil {
		return "", "", workflowbackend.Inspection{}, fmt.Errorf("step %q: workflow %q: %w", stepName, workflow, ierr)
	}
	return workflow, workflowbackend.DigestBytes(data), insp, nil
}

// digestShort returns the first 12 hex chars of a "sha256:<hex>" digest.
func digestShort(digest string) string {
	hex := strings.TrimPrefix(digest, "sha256:")
	if len(hex) > 12 {
		hex = hex[:12]
	}
	return hex
}

// renderConnections templates the grant's secret references and validates the
// grant against the workflow's declared connections (orun-workflows-v2 §4).
func (jp *JobPlanner) renderConnections(compType, stepName string, grant map[string]map[string]string, insp workflowbackend.Inspection, hasInspection bool, context map[string]interface{}) (map[string]map[string]string, error) {
	var rendered map[string]map[string]string
	if len(grant) > 0 {
		rendered = make(map[string]map[string]string, len(grant))
		for conn, fields := range grant {
			renderedFields := make(map[string]string, len(fields))
			for field, ref := range fields {
				value, err := jp.renderTemplateString(compType, stepName, "connections."+conn+"."+field, ref, context)
				if err != nil {
					return nil, err
				}
				renderedFields[field] = value
			}
			rendered[conn] = renderedFields
		}
	}
	if hasInspection {
		if err := workflowbackend.ValidateGrant("step "+stepName, insp.Connections, rendered); err != nil {
			return nil, err
		}
	}
	return rendered, nil
}

func (jp *JobPlanner) renderTemplateString(componentType, stepName, fieldName, value string, context map[string]interface{}) (string, error) {
	if value == "" {
		return "", nil
	}

	// Run-time output references (${{ steps.X.outputs.Y }}, orun-workflows-v2
	// §5) are masked through the compile-time template pass and restored after:
	// their values exist only at run time, so the compiler validates the names
	// and leaves the spans for the runner to substitute.
	masked, spans := workflowbackend.MaskOutputRefs(value)

	cacheKey := fmt.Sprintf("%s:%s:%s", componentType, stepName, fieldName)
	tmpl, exists := jp.templateCache[cacheKey]
	if !exists {
		var err error
		tmpl, err = template.New(cacheKey).Parse(masked)
		if err != nil {
			return "", fmt.Errorf("invalid template in step %s %s: %w", stepName, fieldName, err)
		}
		jp.templateCache[cacheKey] = tmpl
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, context); err != nil {
		return "", fmt.Errorf("failed to execute template in step %s %s: %w", stepName, fieldName, err)
	}

	return workflowbackend.UnmaskOutputRefs(buf.String(), spans), nil
}

func (jp *JobPlanner) renderTemplateMap(componentType, stepName, fieldName string, values map[string]interface{}, context map[string]interface{}) (map[string]interface{}, error) {
	if len(values) == 0 {
		return nil, nil
	}

	rendered := make(map[string]interface{}, len(values))
	for key, value := range values {
		strValue, ok := value.(string)
		if !ok {
			rendered[key] = value
			continue
		}
		resolved, err := jp.renderTemplateString(componentType, stepName, fieldName+":"+key, strValue, context)
		if err != nil {
			return nil, err
		}
		rendered[key] = resolved
	}

	return rendered, nil
}

func sortStepsByPhaseAndOrder(steps []model.Step) ([]model.Step, error) {
	type indexedStep struct {
		step  model.Step
		index int
	}

	indexed := make([]indexedStep, 0, len(steps))
	for i, step := range steps {
		phase := model.NormalizePhase(step.Phase)
		if !model.IsValidPhase(phase) {
			return nil, fmt.Errorf("invalid step phase %q in step %s", step.Phase, step.Name)
		}
		step.Phase = phase
		indexed = append(indexed, indexedStep{step: step, index: i})
	}

	sort.SliceStable(indexed, func(i, j int) bool {
		pi := phaseRank(indexed[i].step.Phase)
		pj := phaseRank(indexed[j].step.Phase)
		if pi != pj {
			return pi < pj
		}
		if indexed[i].step.Order != indexed[j].step.Order {
			return indexed[i].step.Order < indexed[j].step.Order
		}
		return indexed[i].index < indexed[j].index
	})

	result := make([]model.Step, 0, len(indexed))
	for _, item := range indexed {
		result = append(result, item.step)
	}

	return result, nil
}

func phaseRank(phase string) int {
	switch model.NormalizePhase(phase) {
	case string(model.PhasePre):
		return 0
	case string(model.PhaseMain):
		return 1
	case string(model.PhasePost):
		return 2
	default:
		return 99
	}
}

func applyStepOverrides(base []model.Step, overrides []model.Step) []model.Step {
	if len(overrides) == 0 {
		return base
	}

	result := make([]model.Step, 0, len(base)+len(overrides))
	indexByName := make(map[string]int, len(base))

	for _, step := range base {
		indexByName[step.Name] = len(result)
		result = append(result, step)
	}

	for _, override := range overrides {
		if idx, exists := indexByName[override.Name]; exists {
			// Replace entirely by step name.
			result[idx] = override
			continue
		}

		// Allow additive override steps when name does not exist in base job.
		indexByName[override.Name] = len(result)
		result = append(result, override)
	}

	return result
}

// resolveDependencies sets up dependency edges between job instances
func (jp *JobPlanner) resolveDependencies(jobInstances map[string]*model.JobInstance, compInstances map[string][]*model.ComponentInstance) error {
	// Build a map for fast lookup: (component, environment) -> job IDs
	compToJobs := make(map[string][]string) // key: "comp@env", value: [jobIDs]

	for jobID, job := range jobInstances {
		key := fmt.Sprintf("%s.%s", job.Component, job.Environment)
		compToJobs[key] = append(compToJobs[key], jobID)
	}

	// For each component instance, resolve its dependencies
	for envName, envInstances := range compInstances {
		for _, compInst := range envInstances {
			// Get all jobs for this component
			key := fmt.Sprintf("%s.%s", compInst.ComponentName, envName)
			myJobs, exists := compToJobs[key]
			if !exists {
				continue
			}

			// Resolve each dependency
			for _, dep := range compInst.DependsOn {
				depKey := fmt.Sprintf("%s.%s", dep.ComponentName, dep.Environment)
				depJobs, exists := compToJobs[depKey]
				if !exists {
					// Include policy decides whether a missing dep is an
					// error or just a no-op order edge. "if-selected"
					// (the default) means the edge is order-only: if the
					// other end isn't in this plan, silently drop it.
					// "always" means the author asserted the dep MUST run,
					// so a missing target is a real misconfiguration.
					include := dep.Include
					if include == "" {
						include = model.IncludeIfSelected
					}
					if include == model.IncludeAlways {
						return fmt.Errorf("dependency not found: %s depends on %s (include: always)", key, depKey)
					}
					continue
				}

				// Branch by the dependent job's resolved DependencyMode.
				// Falls back to enforced for legacy paths that did not
				// populate the field.
				for _, myJob := range myJobs {
					mode := jobInstances[myJob].DependencyMode
					if mode == "" {
						mode = model.DependencyModeEnforced
					}
					switch mode {
					case model.DependencyModeAdvisory:
						jobInstances[myJob].AdvisoryDependsOn = append(
							jobInstances[myJob].AdvisoryDependsOn, depJobs...)
					case model.DependencyModeDisabled:
						// drop dependency entirely
					default: // enforced
						jobInstances[myJob].DependsOn = append(
							jobInstances[myJob].DependsOn, depJobs...)
					}
				}
			}
		}
	}

	return nil
}
