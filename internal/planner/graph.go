package planner

import (
	"fmt"

	"github.com/sourceplane/arx/internal/model"
)

// JobGraph represents the DAG of job instances with cycle detection and topological sorting
type JobGraph struct {
	jobs map[string]*model.JobInstance
}

// NewJobGraph creates a new job graph from job instances
func NewJobGraph(jobs map[string]*model.JobInstance) *JobGraph {
	return &JobGraph{
		jobs: jobs,
	}
}

// DetectCycles performs cycle detection on the job dependency graph using DFS
func (g *JobGraph) DetectCycles() error {
	visited := make(map[string]bool)
	recStack := make(map[string]bool)

	for jobID := range g.jobs {
		if !visited[jobID] {
			if g.hasCycleDFS(jobID, visited, recStack) {
				return fmt.Errorf("cycle detected in job dependencies")
			}
		}
	}

	return nil
}

// hasCycleDFS performs DFS cycle detection from a given node
func (g *JobGraph) hasCycleDFS(node string, visited, recStack map[string]bool) bool {
	visited[node] = true
	recStack[node] = true

	job, exists := g.jobs[node]
	if !exists {
		return false
	}

	for _, dep := range job.DependsOn {
		if !visited[dep] {
			if g.hasCycleDFS(dep, visited, recStack) {
				return true
			}
		} else if recStack[dep] {
			return true
		}
	}

	recStack[node] = false
	return false
}

// TopologicalSort performs topological sorting of jobs using Kahn's algorithm
// Returns sorted job IDs in execution order
func (g *JobGraph) TopologicalSort() ([]string, error) {
	// Build reverse dependency graph (dependents: who depends on me)
	dependents := make(map[string][]string)
	inDegree := make(map[string]int)

	// Initialize all jobs
	for jobID := range g.jobs {
		inDegree[jobID] = 0
		dependents[jobID] = make([]string, 0)
	}

	// Build graph by counting incoming edges
	for jobID, job := range g.jobs {
		for _, dep := range job.DependsOn {
			dependents[dep] = append(dependents[dep], jobID)
			inDegree[jobID]++
		}
	}

	// Kahn's algorithm: process nodes with no dependencies first
	queue := make([]string, 0)
	for jobID, degree := range inDegree {
		if degree == 0 {
			queue = append(queue, jobID)
		}
	}

	sorted := make([]string, 0, len(g.jobs))
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		sorted = append(sorted, current)

		// Process all dependents
		for _, dependent := range dependents[current] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = append(queue, dependent)
			}
		}
	}

	// Check if all jobs were processed (indicates no cycles)
	if len(sorted) != len(g.jobs) {
		return nil, fmt.Errorf("failed to topologically sort: possible cycle detected")
	}

	return sorted, nil
}
