package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/render"
)

// githubMatrixJSON renders the plan's jobs as a GitHub Actions matrix
// (`{"include":[...]}`) for --github-output. Each entry carries the job's
// index so a workflow can give every lane the runner label that pins a
// runner to it: ghr-orun-job:${{ github.run_id }}-${{ matrix.index }}.
func githubMatrixJSON(plan *model.Plan) string {
	entries := make([]string, 0, len(plan.Jobs))
	for _, job := range plan.Jobs {
		entries = append(entries, fmt.Sprintf(
			`{"id":%q,"uid":%q,"index":%d,"component":%q,"env":%q,"composition":%q,"profile":%q}`,
			job.ID, job.UID, job.Index, job.Component, job.Environment, job.Composition, job.Profile))
	}
	return fmt.Sprintf("{\"include\":[%s]}", strings.Join(entries, ","))
}

// laneMatrixEntry is one GitHub job of the lane matrix (specs/orun-lanes §9.1).
// Level and UpstreamLanes are what a pool, or a workflow that runs the lanes
// level by level, gates the runner on (specs/orun-runner-efficiency §5.1).
type laneMatrixEntry struct {
	Lane          string   `json:"lane"`
	Name          string   `json:"name"`
	Index         int      `json:"index"`
	Env           string   `json:"env"`
	Jobs          int      `json:"jobs"`
	RunsOn        string   `json:"runsOn"`
	Level         int      `json:"level"`
	UpstreamLanes []string `json:"upstreamLanes"`
}

type laneMatrix struct {
	Include []laneMatrixEntry `json:"include"`
}

// laneMatrixEntries is one entry per lane, or one per job on a plan without
// lanes (lane = job id, name = its check name, level = the job's depth in
// the plan's dependency graph, upstreamLanes = its dependencies), so a
// workflow can switch to the lane matrix before it adopts any lane
// configuration.
func laneMatrixEntries(plan *model.Plan) []laneMatrixEntry {
	entries := make([]laneMatrixEntry, 0, len(plan.Jobs))
	if len(plan.Lanes) > 0 {
		for _, lane := range plan.Lanes {
			up := lane.UpstreamLanes
			if up == nil {
				up = []string{}
			}
			entries = append(entries, laneMatrixEntry{Lane: lane.ID, Name: lane.Name, Index: lane.Index, Env: lane.Environment, Jobs: len(lane.Jobs), RunsOn: lane.RunsOn, Level: lane.Level, UpstreamLanes: up})
		}
		return entries
	}
	depth := jobDepths(plan.Jobs)
	known := make(map[string]bool, len(plan.Jobs))
	for _, job := range plan.Jobs {
		known[job.ID] = true
	}
	for _, job := range plan.Jobs {
		name := job.CheckName
		if name == "" {
			name = render.PlanJobCheckName(job)
		}
		if name == "" {
			name = job.ID
		}
		up := []string{}
		for _, dep := range job.DependsOn {
			if known[dep] {
				up = append(up, dep)
			}
		}
		sort.Strings(up)
		entries = append(entries, laneMatrixEntry{Lane: job.ID, Name: name, Index: job.Index, Env: job.Environment, Jobs: 1, RunsOn: job.RunsOn, Level: depth[job.ID], UpstreamLanes: up})
	}
	return entries
}

// jobDepths is each job's depth in the plan's dependency graph: 0 with no
// dependencies in the plan, else one more than its deepest dependency.
func jobDepths(jobs []model.PlanJob) map[string]int {
	byID := make(map[string]model.PlanJob, len(jobs))
	for _, job := range jobs {
		byID[job.ID] = job
	}
	memo := make(map[string]int, len(jobs))
	visiting := map[string]bool{}
	var walk func(id string) int
	walk = func(id string) int {
		if d, ok := memo[id]; ok {
			return d
		}
		if visiting[id] { // the planner rejects cycles; never loop on one
			return 0
		}
		visiting[id] = true
		d := 0
		for _, dep := range byID[id].DependsOn {
			if _, ok := byID[dep]; !ok {
				continue
			}
			if v := walk(dep) + 1; v > d {
				d = v
			}
		}
		delete(visiting, id)
		memo[id] = d
		return d
	}
	for _, job := range jobs {
		walk(job.ID)
	}
	return memo
}

// githubLaneMatrixJSON renders the plan's lanes as a matrix for
// `orun run --lane ${{ matrix.lane }}`, and the number of entries.
func githubLaneMatrixJSON(plan *model.Plan) (string, int) {
	entries := laneMatrixEntries(plan)
	b, _ := json.Marshal(laneMatrix{entries})
	return string(b), len(entries)
}

// githubLaneMatrixLevels renders the lane matrix split by level: element k
// is the matrix of the lanes at level k. A workflow runs level k as one
// matrix job that `needs:` level k-1, so no runner is provisioned before the
// lanes it waits on are done. Returns the per-level matrices (each a
// `{"include":[…]}` document) in level order.
func githubLaneMatrixLevels(plan *model.Plan) []string {
	entries := laneMatrixEntries(plan)
	depth := -1
	for _, e := range entries {
		if e.Level > depth {
			depth = e.Level
		}
	}
	levels := make([]laneMatrix, depth+1)
	for k := range levels {
		levels[k].Include = []laneMatrixEntry{}
	}
	for _, e := range entries {
		levels[e.Level].Include = append(levels[e.Level].Include, e)
	}
	out := make([]string, len(levels))
	for k, m := range levels {
		b, _ := json.Marshal(m)
		out[k] = string(b)
	}
	return out
}

// githubLaneMatrixLevelsJSON is githubLaneMatrixLevels as one JSON array, for
// `fromJson(…)[k]`.
func githubLaneMatrixLevelsJSON(plan *model.Plan) string {
	levels := githubLaneMatrixLevels(plan)
	if len(levels) == 0 {
		return "[]"
	}
	return "[" + strings.Join(levels, ",") + "]"
}
