package main

import (
	"testing"

	"github.com/sourceplane/gluon/internal/state"
)

func TestExecutionCountsFromStatePrefersStateValues(t *testing.T) {
	t.Parallel()

	counts := executionCountsFromState(&state.ExecMetadata{
		JobTotal:  9,
		JobDone:   0,
		JobFailed: 0,
	}, &state.ExecState{
		Jobs: map[string]*state.JobState{
			"web-build": {Status: "completed"},
			"web-lint":  {Status: "completed"},
			"api-test":  {Status: "failed"},
		},
	})

	if counts.total != 3 {
		t.Fatalf("counts.total = %d, want 3", counts.total)
	}
	if counts.completed != 2 {
		t.Fatalf("counts.completed = %d, want 2", counts.completed)
	}
	if counts.failed != 1 {
		t.Fatalf("counts.failed = %d, want 1", counts.failed)
	}
}

func TestSelectRelevantLogEntriesPrefersFailedAndURLLogs(t *testing.T) {
	t.Parallel()

	entries := []logEntry{
		{jobID: "api-test", stepID: "test", status: "failed", content: "1 failing test"},
		{jobID: "web-build", stepID: "build", status: "completed", content: "Built web app\nPreview URL: https://preview.example.dev"},
		{jobID: "web-build", stepID: "restore-cache", status: "completed", content: "restore warm cache"},
		{jobID: "web-lint", stepID: "lint", status: "completed", content: "Lint passed"},
	}

	previousRaw := logsRaw
	previousFailed := logsFailed
	previousJob := logsJob
	previousStep := logsStep
	logsRaw = false
	logsFailed = false
	logsJob = ""
	logsStep = ""
	defer func() {
		logsRaw = previousRaw
		logsFailed = previousFailed
		logsJob = previousJob
		logsStep = previousStep
	}()

	filtered := selectRelevantLogEntries(entries)
	if len(filtered) != 2 {
		t.Fatalf("len(filtered) = %d, want 2", len(filtered))
	}
	if filtered[0].jobID != "api-test" {
		t.Fatalf("filtered[0].jobID = %q, want api-test", filtered[0].jobID)
	}
	if filtered[1].stepID != "build" {
		t.Fatalf("filtered[1].stepID = %q, want build", filtered[1].stepID)
	}
}
