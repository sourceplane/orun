package planner

import (
	"fmt"

	"github.com/sourceplane/orun/internal/model"
)

// ResolvePromotionDependencies resolves environment promotion dependencies
// declared in intent (env.promotion.dependsOn) onto the plan's job instances.
//
// Under the env-scoping "Z" model (specs/orun-env-scoping/design.md §4):
//   - When the prerequisite environment IS in this plan, in-plan DAG edges are
//     added (addPromotionEdges). This is the only ENFORCED promotion mechanism:
//     a failed prerequisite job blocks its dependents within the run.
//   - When the prerequisite environment is NOT in this plan:
//   - a previous-success / same-plan-or-previous-success dep records a
//     cross-plan gate (addPromotionGates). Gates are advisory metadata —
//     they are NOT enforced at run time; cross-pipeline (cross-invocation)
//     gating is deferred to Option C, and the CLI emits a one-line notice.
//   - a pure same-plan dep is PRUNED (the CLI records a PrunedEdge and warns).
func ResolvePromotionDependencies(
	jobInstances map[string]*model.JobInstance,
	compInstances map[string][]*model.ComponentInstance,
	environments map[string]model.Environment,
) error {
	activeEnvs := make(map[string]struct{}, len(compInstances))
	for envName := range compInstances {
		activeEnvs[envName] = struct{}{}
	}

	// Build lookup: "component.environment" -> list of job IDs
	compEnvToJobs := make(map[string][]string)
	for jobID, job := range jobInstances {
		key := fmt.Sprintf("%s.%s", job.Component, job.Environment)
		compEnvToJobs[key] = append(compEnvToJobs[key], jobID)
	}

	// Build lookup: environment -> list of job IDs
	envToJobs := make(map[string][]string)
	for jobID, job := range jobInstances {
		envToJobs[job.Environment] = append(envToJobs[job.Environment], jobID)
	}

	for envName, env := range environments {
		if _, active := activeEnvs[envName]; !active {
			continue
		}

		for _, dep := range env.Promotion.DependsOn {
			_, depActive := activeEnvs[dep.Environment]

			if depActive && (dep.Satisfy == "same-plan" || dep.Satisfy == "same-plan-or-previous-success") {
				if err := addPromotionEdges(jobInstances, compEnvToJobs, envToJobs, envName, dep); err != nil {
					return err
				}
			} else if !depActive && (dep.Satisfy == "previous-success" || dep.Satisfy == "same-plan-or-previous-success") {
				addPromotionGates(jobInstances, envToJobs, envName, dep)
			} else if !depActive && dep.Satisfy == "same-plan" {
				// env-scoping (Z model): a same-plan promotion edge whose
				// prerequisite environment is not in this (scoped) plan is
				// pruned rather than fatal. The CLI records it as a PrunedEdge
				// and warns (specs/orun-env-scoping/design.md §3–§4); the
				// selected environment runs standalone.
				continue
			}
		}
	}

	return nil
}

func addPromotionEdges(
	jobInstances map[string]*model.JobInstance,
	compEnvToJobs map[string][]string,
	envToJobs map[string][]string,
	envName string,
	dep model.PromotionDependency,
) error {
	switch dep.Strategy {
	case "same-component":
		myJobs := envToJobs[envName]
		for _, jobID := range myJobs {
			job := jobInstances[jobID]
			depKey := fmt.Sprintf("%s.%s", job.Component, dep.Environment)
			depJobs := compEnvToJobs[depKey]
			if len(depJobs) == 0 {
				continue
			}
			job.DependsOn = append(job.DependsOn, depJobs...)
		}

	case "environment-barrier":
		myJobs := envToJobs[envName]
		depJobs := envToJobs[dep.Environment]
		if len(depJobs) == 0 {
			return nil
		}
		for _, jobID := range myJobs {
			jobInstances[jobID].DependsOn = append(jobInstances[jobID].DependsOn, depJobs...)
		}
	}

	return nil
}

func addPromotionGates(
	jobInstances map[string]*model.JobInstance,
	envToJobs map[string][]string,
	envName string,
	dep model.PromotionDependency,
) {
	myJobs := envToJobs[envName]
	for _, jobID := range myJobs {
		job := jobInstances[jobID]
		gate := model.PromotionGate{
			Type:        "environment-promotion",
			Environment: dep.Environment,
			Component:   job.Component,
			Condition:   dep.Condition,
			Match:       map[string]string{"revision": dep.Match.Revision},
		}
		job.Gates = append(job.Gates, gate)
	}
}
