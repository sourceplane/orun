package planner

import (
	"fmt"
	"sort"
	"strings"
	"text/template"

	"github.com/sourceplane/arx/internal/model"
)

// JobPlanner binds components to jobs and creates instances
type JobPlanner struct {
	compositions  map[string]*CompositionInfo // Composition -> default job info
	templateCache map[string]*template.Template
}

// CompositionInfo holds the default job for a composition
type CompositionInfo struct {
	Type       string
	DefaultJob *model.JobSpec
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
			compositionInfo, exists := jp.compositions[compInst.Type]
			if !exists {
				return nil, fmt.Errorf("no job definition for type: %s", compInst.Type)
			}

			jobDef := compositionInfo.DefaultJob
			if jobDef == nil {
				return nil, fmt.Errorf("no default job defined for type: %s", compInst.Type)
			}

			// Create job instance
			jobID := fmt.Sprintf("%s@%s.%s", compInst.ComponentName, envName, jobDef.Name)
			jobInst := &model.JobInstance{
				ID:          jobID,
				Name:        jobDef.Name,
				Component:   compInst.ComponentName,
				Environment: envName,
				Composition: compInst.Type,
				RunsOn:      jobDef.RunsOn,
				Path:        compInst.Path,
				Timeout:     jobDef.Timeout,
				Retries:     jobDef.Retries,
				Labels:      compInst.Labels,
				Config:      compInst.Inputs,
				DependsOn:   make([]string, 0),
			}

			resolvedSteps := applyStepOverrides(jobDef.Steps, compInst.StepOverrides)

			// Render steps with template variables
			renderedSteps, err := jp.renderSteps(resolvedSteps, compInst)
			if err != nil {
				return nil, fmt.Errorf("failed to render steps for job %s: %w", jobID, err)
			}
			jobInst.Steps = renderedSteps

			jobInstances[jobID] = jobInst
		}
	}

	// Resolve job dependencies
	err := jp.resolveDependencies(jobInstances, instances)
	if err != nil {
		return nil, err
	}

	return jobInstances, nil
}

// Templates are cached to avoid re-parsing identical steps across multiple instances
func (jp *JobPlanner) renderSteps(steps []model.Step, compInst *model.ComponentInstance) ([]model.RenderedStep, error) {
	rendered := make([]model.RenderedStep, 0, len(steps))
	sortedSteps, err := sortStepsByPhaseAndOrder(steps)
	if err != nil {
		return nil, err
	}

	// Build template context once
	context := map[string]interface{}{
		"Component":   compInst.ComponentName,
		"Environment": compInst.Environment,
		"Type":        compInst.Type,
	}

	// Add all inputs to context
	for k, v := range compInst.Inputs {
		context[k] = v
	}

	for _, step := range sortedSteps {
		renderedRun, err := jp.renderTemplateString(compInst.Type, step.Name, "run", step.Run, context)
		if err != nil {
			return nil, err
		}
		renderedUse, err := jp.renderTemplateString(compInst.Type, step.Name, "use", step.Use, context)
		if err != nil {
			return nil, err
		}
		renderedShell, err := jp.renderTemplateString(compInst.Type, step.Name, "shell", step.Shell, context)
		if err != nil {
			return nil, err
		}
		renderedWorkingDirectory, err := jp.renderTemplateString(compInst.Type, step.Name, "working-directory", step.WorkingDirectory, context)
		if err != nil {
			return nil, err
		}
		renderedWith, err := jp.renderTemplateMap(compInst.Type, step.Name, "with", step.With, context)
		if err != nil {
			return nil, err
		}
		renderedEnv, err := jp.renderTemplateMap(compInst.Type, step.Name, "env", step.Env, context)
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
			With:             renderedWith,
			Env:              renderedEnv,
			Shell:            renderedShell,
			WorkingDirectory: renderedWorkingDirectory,
			Timeout:          step.Timeout,
			Retry:            step.Retry,
			OnFailure:        step.OnFailure,
		})
	}

	return rendered, nil
}

func (jp *JobPlanner) renderTemplateString(componentType, stepName, fieldName, value string, context map[string]interface{}) (string, error) {
	if value == "" {
		return "", nil
	}

	cacheKey := fmt.Sprintf("%s:%s:%s", componentType, stepName, fieldName)
	tmpl, exists := jp.templateCache[cacheKey]
	if !exists {
		var err error
		tmpl, err = template.New(cacheKey).Parse(value)
		if err != nil {
			return "", fmt.Errorf("invalid template in step %s %s: %w", stepName, fieldName, err)
		}
		jp.templateCache[cacheKey] = tmpl
	}

	var buf strings.Builder
	if err := tmpl.Execute(&buf, context); err != nil {
		return "", fmt.Errorf("failed to execute template in step %s %s: %w", stepName, fieldName, err)
	}

	return buf.String(), nil
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
		key := fmt.Sprintf("%s@%s", job.Component, job.Environment)
		compToJobs[key] = append(compToJobs[key], jobID)
	}

	// For each component instance, resolve its dependencies
	for envName, envInstances := range compInstances {
		for _, compInst := range envInstances {
			// Get all jobs for this component
			key := fmt.Sprintf("%s@%s", compInst.ComponentName, envName)
			myJobs, exists := compToJobs[key]
			if !exists {
				continue
			}

			// Resolve each dependency
			for _, dep := range compInst.DependsOn {
				depKey := fmt.Sprintf("%s@%s", dep.ComponentName, dep.Environment)
				depJobs, exists := compToJobs[depKey]
				if !exists {
					return fmt.Errorf("dependency not found: %s depends on %s", key, depKey)
				}

				// Link all my jobs to all dependency jobs
				for _, myJob := range myJobs {
					jobInstances[myJob].DependsOn = append(jobInstances[myJob].DependsOn, depJobs...)
				}
			}
		}
	}

	return nil
}
