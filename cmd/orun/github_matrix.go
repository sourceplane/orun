package main

import (
	"encoding/json"
	"fmt"
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
type laneMatrixEntry struct {
	Lane   string `json:"lane"`
	Name   string `json:"name"`
	Index  int    `json:"index"`
	Env    string `json:"env"`
	Jobs   int    `json:"jobs"`
	RunsOn string `json:"runsOn"`
}

// githubLaneMatrixJSON renders the plan's lanes as a matrix for
// `orun run --lane ${{ matrix.lane }}`. A plan without lanes gets one entry
// per job (lane = job id, name = its check name), so a workflow can switch to
// the lane matrix before it adopts any lane configuration.
func githubLaneMatrixJSON(plan *model.Plan) (string, int) {
	entries := make([]laneMatrixEntry, 0, len(plan.Jobs))
	if len(plan.Lanes) > 0 {
		for _, lane := range plan.Lanes {
			entries = append(entries, laneMatrixEntry{Lane: lane.ID, Name: lane.Name, Index: lane.Index, Env: lane.Environment, Jobs: len(lane.Jobs), RunsOn: lane.RunsOn})
		}
	} else {
		for _, job := range plan.Jobs {
			name := job.CheckName
			if name == "" {
				name = render.PlanJobCheckName(job)
			}
			if name == "" {
				name = job.ID
			}
			entries = append(entries, laneMatrixEntry{Lane: job.ID, Name: name, Index: job.Index, Env: job.Environment, Jobs: 1, RunsOn: job.RunsOn})
		}
	}
	b, _ := json.Marshal(struct {
		Include []laneMatrixEntry `json:"include"`
	}{entries})
	return string(b), len(entries)
}
