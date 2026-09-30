package planner

import (
	"fmt"
	"sort"

	"github.com/sourceplane/orun/internal/model"
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
// Returns sorted job IDs in execution order.
//
// The order is deterministic: among the jobs whose dependencies are all
// satisfied, the lexicographically smallest id is emitted first. Plan
// checksums and revision keys hash the rendered job order, so it must not
// depend on map iteration order.
func (g *JobGraph) TopologicalSort() ([]string, error) {
	jobIDs := make([]string, 0, len(g.jobs))
	for jobID := range g.jobs {
		jobIDs = append(jobIDs, jobID)
	}
	sort.Strings(jobIDs)

	// Build reverse dependency graph (dependents: who depends on me)
	dependents := make(map[string][]string)
	inDegree := make(map[string]int, len(jobIDs))

	// Initialize all jobs
	for _, jobID := range jobIDs {
		inDegree[jobID] = 0
	}

	// Build graph by counting incoming edges
	for _, jobID := range jobIDs {
		for _, dep := range g.jobs[jobID].DependsOn {
			dependents[dep] = append(dependents[dep], jobID)
			inDegree[jobID]++
		}
	}

	// Kahn's algorithm: process nodes with no dependencies first. ready is
	// kept sorted so the next job is always the smallest ready id.
	ready := make([]string, 0)
	for _, jobID := range jobIDs {
		if inDegree[jobID] == 0 {
			ready = append(ready, jobID)
		}
	}

	sorted := make([]string, 0, len(g.jobs))
	for len(ready) > 0 {
		current := ready[0]
		ready = ready[1:]
		sorted = append(sorted, current)

		// Process all dependents
		for _, dependent := range dependents[current] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				i := sort.SearchStrings(ready, dependent)
				ready = append(ready, "")
				copy(ready[i+1:], ready[i:])
				ready[i] = dependent
			}
		}
	}

	// Check if all jobs were processed (indicates no cycles)
	if len(sorted) != len(g.jobs) {
		return nil, fmt.Errorf("failed to topologically sort: possible cycle detected")
	}

	return sorted, nil
}
