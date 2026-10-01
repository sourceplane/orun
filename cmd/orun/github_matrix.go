package main

import (
	"fmt"
	"strings"

	"github.com/sourceplane/orun/internal/model"
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
