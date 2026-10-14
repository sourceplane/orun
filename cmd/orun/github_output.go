package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/runbundle"
)

// githubExecID is the execution id a plan made in GitHub Actions carries: the
// workflow run, its attempt and the plan's short checksum (runbundle.ExecID).
// The plan shard --artifact github uploads is named by it and --github-output
// hands it to the run jobs as exec_id, so it is computed once and the two
// agree.
func githubExecID(planID string) string {
	runAttempt := os.Getenv("GITHUB_RUN_ATTEMPT")
	if runAttempt == "" {
		runAttempt = "1"
	}
	if len(planID) > 12 {
		planID = planID[:12]
	}
	return runbundle.ExecID(os.Getenv("GITHUB_RUN_ID"), runAttempt, planID)
}

// githubOutputLines are the step outputs --github-output appends to
// $GITHUB_OUTPUT for a plan: the job matrix, the lane matrix and its count,
// one matrix per level with the array of them and the level count, the plan
// id and the execution id. A nil plan (a CI event that matched no trigger)
// yields empty matrices and empty ids, so a workflow's matrix guard skips its
// run jobs.
func githubOutputLines(plan *model.Plan, planID, execID string) []string {
	if plan == nil {
		return []string{
			githubOutputHeredoc("matrix", `{"include":[]}`),
			githubOutputHeredoc("lane-matrix", `{"include":[]}`),
			"lane-count=0",
			githubOutputHeredoc("lane-matrix-levels", "[]"),
			"lane-level-count=0",
			"plan_id=",
			"exec_id=",
		}
	}
	lines := []string{githubOutputHeredoc("matrix", githubMatrixJSON(plan))}
	laneMatrix, laneCount := githubLaneMatrixJSON(plan)
	lines = append(lines, githubOutputHeredoc("lane-matrix", laneMatrix), fmt.Sprintf("lane-count=%d", laneCount))
	// Level by level: lane-matrix-<k> per level, the array of them, and the
	// level count, so a workflow can chain one matrix job per level with
	// needs: and never provision a runner before its upstream lanes are done
	// (specs/orun-runner-efficiency §5.1).
	levels := githubLaneMatrixLevels(plan)
	for k, m := range levels {
		lines = append(lines, githubOutputHeredoc(fmt.Sprintf("lane-matrix-%d", k), m))
	}
	return append(lines,
		githubOutputHeredoc("lane-matrix-levels", githubLaneMatrixLevelsJSON(plan)),
		fmt.Sprintf("lane-level-count=%d", len(levels)),
		"plan_id="+planID,
		"exec_id="+execID,
	)
}

// githubOutputHeredoc is a multi-line step output in $GITHUB_OUTPUT syntax.
func githubOutputHeredoc(name, value string) string {
	return fmt.Sprintf("%s<<EOF\n%s\nEOF", name, value)
}

// writeGithubOutput appends lines to the step-output file $GITHUB_OUTPUT
// names. It needs nothing else of GitHub Actions and no artifact backend: a
// workflow whose run jobs take the plan from the platform runs
// `orun plan --github-output` with no --artifact.
func writeGithubOutput(lines []string) error {
	path := os.Getenv("GITHUB_OUTPUT")
	if path == "" {
		return fmt.Errorf("$GITHUB_OUTPUT is not set; nothing written")
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(strings.Join(lines, "\n") + "\n")
	return err
}

// emitGithubOutput writes the --github-output outputs for plan when the flag
// is set. Not being able to write them is a warning, not a failure: the plan
// itself succeeded, and the workflow's matrix guard reports the missing
// output.
func emitGithubOutput(plan *model.Plan, planID, execID string) {
	if !githubOutput {
		return
	}
	if err := writeGithubOutput(githubOutputLines(plan, planID, execID)); err != nil {
		fmt.Fprintf(os.Stderr, "⚠ warning: --github-output: %v\n", err)
	}
}
