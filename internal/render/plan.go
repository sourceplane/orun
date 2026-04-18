package render

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/sourceplane/arx/internal/model"
	"gopkg.in/yaml.v3"
)

const (
	planAPIVersion   = "arx.io/v1"
	defaultStateFile = ".arx-state.json"
)

// Renderer materializes job instances into a Plan
type Renderer struct{}

// NewRenderer creates a new renderer
func NewRenderer() *Renderer {
	return &Renderer{}
}

// RenderPlan creates a plan from job instances with JobRegistry bindings
func (r *Renderer) RenderPlan(metadata model.Metadata, jobInstances map[string]*model.JobInstance, jobBindings map[string]string) *model.Plan {
	jobIDs := make([]string, 0, len(jobInstances))
	for jobID := range jobInstances {
		jobIDs = append(jobIDs, jobID)
	}
	sort.Strings(jobIDs)
	return r.RenderPlanWithOrder(metadata, jobInstances, jobBindings, jobIDs)
}

// RenderPlanWithOrder creates a plan from job instances using a caller-specified order.
func (r *Renderer) RenderPlanWithOrder(metadata model.Metadata, jobInstances map[string]*model.JobInstance, jobBindings map[string]string, jobOrder []string) *model.Plan {
	plan := &model.Plan{
		APIVersion: planAPIVersion,
		Kind:       "Plan",
		Metadata: model.PlanMetadata{
			Name:        metadata.Name,
			Description: metadata.Description,
			Namespace:   metadata.Namespace,
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		},
		Execution: model.PlanExecution{
			Concurrency: 4,
			FailFast:    true,
			StateFile:   defaultStateFile,
		},
		Spec: model.PlanSpec{
			JobBindings: jobBindings, // Map of model -> JobRegistry name
		},
		Jobs: make([]model.PlanJob, 0),
	}

	// Convert job instances to plan jobs
	for _, jobID := range jobOrder {
		job, exists := jobInstances[jobID]
		if !exists {
			continue
		}

		// Look up JobRegistry name from bindings
		registryName := ""
		if bindings, ok := jobBindings[job.Composition]; ok {
			registryName = bindings
		}

		planJob := model.PlanJob{
			ID:          job.ID,
			Name:        job.Name,
			Component:   job.Component,
			Environment: job.Environment,
			Composition: job.Composition,
			JobRegistry: registryName,
			Job:         job.Name, // The specific job name from the registry
			RunsOn:      job.RunsOn,
			Path:        job.Path,
			Steps:       r.convertSteps(job.Steps),
			DependsOn:   job.DependsOn,
			Timeout:     job.Timeout,
			Retries:     job.Retries,
			Env:         job.Config, // Single source: Config
			Labels:      job.Labels,
			Config:      job.Config,
		}

		plan.Jobs = append(plan.Jobs, planJob)
	}

	r.attachChecksum(plan)

	return plan
}

// convertSteps converts rendered steps to plan steps
func (r *Renderer) convertSteps(steps []model.RenderedStep) []model.PlanStep {
	planSteps := make([]model.PlanStep, len(steps))
	for i, step := range steps {
		stepID := step.Name
		if stepID == "" {
			stepID = fmt.Sprintf("step-%d", i+1)
		}
		planSteps[i] = model.PlanStep{
			ID:               stepID,
			Name:             step.Name,
			Phase:            step.Phase,
			Order:            step.Order,
			Run:              step.Run,
			Use:              step.Use,
			With:             step.With,
			Env:              step.Env,
			Shell:            step.Shell,
			WorkingDirectory: step.WorkingDirectory,
			Timeout:          step.Timeout,
			Retry:            step.Retry,
			OnFailure:        step.OnFailure,
		}
	}
	return planSteps
}

func (r *Renderer) attachChecksum(plan *model.Plan) {
	if plan == nil {
		return
	}

	clone := *plan
	clone.Metadata.Checksum = ""
	payload, err := json.Marshal(clone)
	if err != nil {
		return
	}
	sum := sha256.Sum256(payload)
	plan.Metadata.Checksum = fmt.Sprintf("sha256-%x", sum)
}

// RenderJSON renders plan as JSON
func (r *Renderer) RenderJSON(plan *model.Plan) ([]byte, error) {
	return json.MarshalIndent(plan, "", "  ")
}

// RenderYAML renders plan as YAML
func (r *Renderer) RenderYAML(plan *model.Plan) ([]byte, error) {
	return yaml.Marshal(plan)
}

// WritePlan writes plan to file (JSON or YAML based on extension)
func (r *Renderer) WritePlan(plan *model.Plan, path string) error {
	var data []byte
	var err error

	// Ensure directory exists
	dir := filepath.Dir(path)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create directory: %w", err)
		}
	}

	// Determine format from extension
	ext := filepath.Ext(path)
	switch ext {
	case ".json":
		data, err = r.RenderJSON(plan)
	case ".yaml", ".yml":
		data, err = r.RenderYAML(plan)
	default:
		// Default to JSON if no extension
		data, err = r.RenderJSON(plan)
	}

	if err != nil {
		return fmt.Errorf("failed to render plan: %w", err)
	}

	// Write to file
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("failed to write plan to %s: %w", path, err)
	}

	return nil
}

// DebugDump outputs debug information about the plan
func (r *Renderer) DebugDump(plan *model.Plan) string {
	output := fmt.Sprintf("Plan: %s (%s)\n", plan.Metadata.Name, plan.Metadata.Description)
	output += fmt.Sprintf("Jobs: %d\n\n", len(plan.Jobs))

	for _, job := range plan.Jobs {
		output += fmt.Sprintf("Job: %s\n", job.ID)
		output += fmt.Sprintf("  Component: %s\n", job.Component)
		output += fmt.Sprintf("  Environment: %s\n", job.Environment)
		output += fmt.Sprintf("  Composition: %s\n", job.Composition)
		output += fmt.Sprintf("  Steps: %d\n", len(job.Steps))
		output += fmt.Sprintf("  DependsOn: %v\n", job.DependsOn)
		output += "\n"
	}

	return output
}
