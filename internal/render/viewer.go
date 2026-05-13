package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/ui"
)

// PlanViewer provides human-readable visualization of a plan DAG
type PlanViewer struct {
	plan  *model.Plan
	color bool
	long  bool
}

// NewPlanViewer creates a new plan viewer
func NewPlanViewer(plan *model.Plan) *PlanViewer {
	return &PlanViewer{plan: plan}
}

func (pv *PlanViewer) SetColor(enabled bool) *PlanViewer {
	pv.color = enabled
	return pv
}

func (pv *PlanViewer) SetLong(enabled bool) *PlanViewer {
	pv.long = enabled
	return pv
}

// profileDisplayName returns the display name for a job based on profile.
// For "validate-terraform" with profile "terraform.validate", returns "validate".
func profileDisplayName(job *model.PlanJob) string {
	if job.Profile != "" {
		parts := strings.SplitN(job.Profile, ".", 2)
		if len(parts) == 2 {
			return parts[1]
		}
	}
	name := job.Name
	if job.Composition != "" {
		name = strings.TrimSuffix(name, "-"+job.Composition)
	}
	return name
}

// ViewDAG returns a human-readable tree view of the DAG
func (pv *PlanViewer) ViewDAG() string {
	if len(pv.plan.Jobs) == 0 {
		return emptyPanel("plan view: dag", "no jobs in plan", pv.color)
	}

	// Group jobs by component and environment
	componentMap := make(map[string]map[string][]*model.PlanJob)
	for i := range pv.plan.Jobs {
		job := &pv.plan.Jobs[i]
		if componentMap[job.Component] == nil {
			componentMap[job.Component] = make(map[string][]*model.PlanJob)
		}
		componentMap[job.Component][job.Environment] = append(componentMap[job.Component][job.Environment], job)
	}

	// Sort components for consistent output
	components := make([]string, 0, len(componentMap))
	for comp := range componentMap {
		components = append(components, comp)
	}
	sort.Strings(components)

	var sb strings.Builder
	sb.WriteString("\n" + ui.BoldCyan(pv.color, "Component graph") + "\n")

	for i, component := range components {
		isLastComponent := i == len(components)-1

		componentPrefix := "├─ "
		if isLastComponent {
			componentPrefix = "└─ "
		}
		sb.WriteString(fmt.Sprintf("%s%s\n", componentPrefix, component))

		// Get sorted environments for this component
		envMap := componentMap[component]
		environments := make([]string, 0, len(envMap))
		for env := range envMap {
			environments = append(environments, env)
		}
		sort.Strings(environments)

		for j, env := range environments {
			isLastEnv := j == len(environments)-1
			jobs := envMap[env]

			sort.Slice(jobs, func(a, b int) bool {
				return jobs[a].Name < jobs[b].Name
			})

			// Environment line with profile: staging [terraform.validate]
			envPrefix := "│  └─ "
			envConnector := "│     "
			if !isLastEnv {
				envPrefix = "│  ├─ "
				envConnector = "│  │  "
			}
			if isLastComponent {
				envPrefix = strings.Replace(envPrefix, "│", " ", 1)
				envConnector = strings.Replace(envConnector, "│", " ", 1)
			}

			profileTag := ""
			if len(jobs) > 0 && jobs[0].Profile != "" {
				profileTag = fmt.Sprintf(" [%s]", jobs[0].Profile)
			}
			sb.WriteString(fmt.Sprintf("%s%s%s\n", envPrefix, env, profileTag))

			for k, job := range jobs {
				isLastJob := k == len(jobs)-1

				jobPrefix := envConnector + "└─ "
				jobConnector := envConnector + "   "
				if !isLastJob {
					jobPrefix = envConnector + "├─ "
					jobConnector = envConnector + "│  "
				}

				displayName := profileDisplayName(job)
				jobLine := fmt.Sprintf("%s%s", jobPrefix, displayName)
				if job.Timeout != "" {
					jobLine += fmt.Sprintf(" · %s", job.Timeout)
				}
				if job.Retries > 0 {
					jobLine += fmt.Sprintf(" (retry:%dx)", job.Retries)
				}
				sb.WriteString(jobLine + "\n")

				// Show dependencies if any
				if len(job.DependsOn) > 0 {
					sort.Strings(job.DependsOn)
					for l, dep := range job.DependsOn {
						isLastDep := l == len(job.DependsOn)-1 && len(job.Steps) == 0
						depPrefix := jobConnector + "├─ "
						if isLastDep {
							depPrefix = jobConnector + "└─ "
						}
						sb.WriteString(fmt.Sprintf("%s(depends on) %s\n", depPrefix, dep))
					}
				}

				// Show steps
				if len(job.Steps) > 0 {
					for l, step := range job.Steps {
						isLastStep := l == len(job.Steps)-1
						stepPrefix := jobConnector + "├─ "
						if isLastStep {
							stepPrefix = jobConnector + "└─ "
						}

						sb.WriteString(fmt.Sprintf("%s%s\n", stepPrefix, step.Name))

						if pv.long && step.Run != "" {
							stepConnector := jobConnector + "│  "
							if isLastStep {
								stepConnector = jobConnector + "   "
							}
							sb.WriteString(fmt.Sprintf("%srun: %s\n", stepConnector, step.Run))
						}
					}
				}
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString(summaryPanel("", map[string]int{
		"components": len(components),
		"jobs":       len(pv.plan.Jobs),
	}, pv.color))

	return sb.String()
}

// ViewByComponent shows a component-focused view with all its jobs
func (pv *PlanViewer) ViewByComponent(componentName string) string {
	var matchingJobs []*model.PlanJob
	for i := range pv.plan.Jobs {
		if pv.plan.Jobs[i].Component == componentName {
			matchingJobs = append(matchingJobs, &pv.plan.Jobs[i])
		}
	}

	if len(matchingJobs) == 0 {
		return emptyPanel("plan view: component", fmt.Sprintf("component: %s\nno jobs found", componentName), pv.color)
	}

	var sb strings.Builder
	sb.WriteString(panelHeader(
		"plan view: component",
		fmt.Sprintf("%s [%s]", componentName, matchingJobs[0].Composition),
		len(matchingJobs),
		pv.color,
	))
	sb.WriteString("\n" + ui.BoldCyan(pv.color, "Environment breakdown") + "\n")

	// Group by environment
	envMap := make(map[string][]*model.PlanJob)
	for _, job := range matchingJobs {
		envMap[job.Environment] = append(envMap[job.Environment], job)
	}

	// Sort environments
	environments := make([]string, 0, len(envMap))
	for env := range envMap {
		environments = append(environments, env)
	}
	sort.Strings(environments)

	// Show each environment
	for _, env := range environments {
		jobs := envMap[env]
		sb.WriteString(fmt.Sprintf("├─ %s (%d jobs)\n", env, len(jobs)))

		sort.Slice(jobs, func(a, b int) bool {
			return jobs[a].Name < jobs[b].Name
		})

		for i, job := range jobs {
			prefix := "│  ├─ "
			if i == len(jobs)-1 {
				prefix = "│  └─ "
			}
			connector := "│  │"
			if i == len(jobs)-1 {
				connector = "│   "
			}

			sb.WriteString(fmt.Sprintf("%s%s\n", prefix, job.Name))
			if job.JobRegistry != "" {
				sb.WriteString(fmt.Sprintf("%s  registry: %s\n", connector, job.JobRegistry))
			}
			if job.Job != "" {
				sb.WriteString(fmt.Sprintf("%s  job: %s\n", connector, job.Job))
			}
			if job.Timeout != "" {
				sb.WriteString(fmt.Sprintf("%s  timeout: %s\n", connector, job.Timeout))
			}
			if job.Retries > 0 {
				sb.WriteString(fmt.Sprintf("%s  retries: %d\n", connector, job.Retries))
			}

			if len(job.DependsOn) > 0 {
				sb.WriteString(fmt.Sprintf("%s  dependencies:\n", connector))
				for _, dep := range job.DependsOn {
					sb.WriteString(fmt.Sprintf("%s    - %s\n", connector, dep))
				}
			}

			if len(job.Steps) > 0 {
				sb.WriteString(fmt.Sprintf("%s  steps:\n", connector))
				for j, step := range job.Steps {
					stepPrefix := "├─ "
					if j == len(job.Steps)-1 {
						stepPrefix = "└─ "
					}
					sb.WriteString(fmt.Sprintf("%s    %s%s\n", connector, stepPrefix, step.Name))
					if step.Run != "" {
						runCmd := step.Run
						if len(runCmd) > 70 {
							runCmd = runCmd[:67] + "..."
						}
						sb.WriteString(fmt.Sprintf("%s    %s   run: %s\n", connector, "   ", runCmd))
					} else if step.Use != "" {
						useRef := step.Use
						if len(useRef) > 70 {
							useRef = useRef[:67] + "..."
						}
						sb.WriteString(fmt.Sprintf("%s    %s   use: %s\n", connector, "   ", useRef))
					}
				}
			}
			sb.WriteString("\n")
		}
	}

	sb.WriteString(summaryPanel("component summary", map[string]int{
		"jobs": len(matchingJobs),
	}, pv.color))

	return sb.String()
}

// ViewDependencies shows job dependencies in a focused way
func (pv *PlanViewer) ViewDependencies() string {
	if len(pv.plan.Jobs) == 0 {
		return emptyPanel("plan view: dependencies", "no jobs in plan", pv.color)
	}

	var sb strings.Builder
	sb.WriteString(panelHeader("plan view: dependencies", pv.plan.Metadata.Name, len(pv.plan.Jobs), pv.color))
	sb.WriteString("\n" + ui.BoldCyan(pv.color, "Dependency graph") + "\n")

	// Sort jobs by name
	jobs := make([]*model.PlanJob, len(pv.plan.Jobs))
	for i := range pv.plan.Jobs {
		jobs[i] = &pv.plan.Jobs[i]
	}
	sort.Slice(jobs, func(a, b int) bool {
		return jobs[a].ID < jobs[b].ID
	})

	for i, job := range jobs {
		prefix := "├─ "
		if i == len(jobs)-1 {
			prefix = "└─ "
		}

		sb.WriteString(fmt.Sprintf("%s%s (%s/%s)\n", prefix, job.Name, job.Component, job.Environment))

		if len(job.DependsOn) == 0 {
			sb.WriteString("   └─ no dependencies\n")
		} else {
			for j, dep := range job.DependsOn {
				depPrefix := "  ├─ "
				if j == len(job.DependsOn)-1 {
					depPrefix = "  └─ "
				}
				sb.WriteString(fmt.Sprintf("%sdepends-on: %s\n", depPrefix, dep))
			}
		}
		sb.WriteString("\n")
	}

	sb.WriteString(summaryPanel("dependency summary", map[string]int{
		"jobs": len(jobs),
	}, pv.color))

	return sb.String()
}

func panelHeader(view, planName string, jobs int, color bool) string {
	var sb strings.Builder
	sb.WriteString(ui.Cyan(color, "┌──────────────────────────────────────────────────────────┐") + "\n")
	sb.WriteString(ui.BoldCyan(color, fmt.Sprintf("│ %-56s │", view)) + "\n")
	sb.WriteString(ui.Cyan(color, "├──────────────────────────────────────────────────────────┤") + "\n")
	sb.WriteString(fmt.Sprintf("│ plan:  %s\n", planName))
	sb.WriteString(fmt.Sprintf("│ jobs:  %d\n", jobs))
	sb.WriteString(ui.Cyan(color, "└──────────────────────────────────────────────────────────┘") + "\n")
	return sb.String()
}

func summaryPanel(title string, values map[string]int, color bool) string {
	keys := make([]string, 0, len(values))
	for k := range values {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var sb strings.Builder
	sb.WriteString("\n")
	for _, key := range keys {
		sb.WriteString(fmt.Sprintf("  %-12s %d\n", key, values[key]))
	}
	return sb.String()
}

func emptyPanel(view, body string, color bool) string {
	var sb strings.Builder
	sb.WriteString(ui.Cyan(color, "┌──────────────────────────────────────────────────────────┐") + "\n")
	sb.WriteString(ui.BoldCyan(color, fmt.Sprintf("│ %-56s │", view)) + "\n")
	sb.WriteString(ui.Cyan(color, "├──────────────────────────────────────────────────────────┤") + "\n")
	for _, line := range strings.Split(body, "\n") {
		sb.WriteString(fmt.Sprintf("│ %s\n", line))
	}
	sb.WriteString(ui.Cyan(color, "└──────────────────────────────────────────────────────────┘"))
	return sb.String()
}
