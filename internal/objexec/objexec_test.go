package objexec

import (
	"context"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/clock"
	"github.com/sourceplane/orun/internal/execseal"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/nodewriter"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/state"
)

func revID() objectstore.ObjectID { return objectstore.ObjectID("sha256:" + strings.Repeat("a", 64)) }

func sampleState() (*state.ExecState, *state.ExecMetadata) {
	st := &state.ExecState{
		ExecID: "exec-legacy-1",
		Jobs: map[string]*state.JobState{
			"api@deploy": {Status: "success", StartedAt: "2026-06-02T10:00:00Z", FinishedAt: "2026-06-02T10:01:00Z",
				Steps: map[string]string{"build": "success", "test": "success"}},
			"db@migrate": {Status: "failed", LastError: "boom", Steps: map[string]string{"apply": "failed"}},
		},
	}
	meta := &state.ExecMetadata{
		ExecID: "exec-legacy-1", Status: "failed", DryRun: false, JobFailed: 1,
		StartedAt: "2026-06-02T10:00:00Z", FinishedAt: "2026-06-02T10:01:00Z",
		Links: []state.ExecutionLink{{Label: "ci", URL: "http://ci/run/1"}},
	}
	return st, meta
}

func TestFromLegacyStateShape(t *testing.T) {
	t.Parallel()
	st, meta := sampleState()
	in := FromLegacyState(revID(), "trg_1", "", "run-001", st, meta)
	if in.Status != nodes.StatusFailed {
		t.Fatalf("status = %q, want failed", in.Status)
	}
	if in.ExecutionID != "exec-legacy-1" || in.ExecutionKey != "run-001" || in.TriggerID != "trg_1" {
		t.Fatalf("ids = %+v", in)
	}
	if in.DryRun || len(in.Links) != 1 || in.Links[0].Label != "ci" {
		t.Fatalf("meta fields = %+v", in)
	}
	if len(in.Jobs) != 2 {
		t.Fatalf("jobs = %d, want 2", len(in.Jobs))
	}
	// Sorted by job id: api@deploy first.
	api := in.Jobs[0]
	if api.Record.JobID != "api@deploy" || api.Record.Status != nodes.StatusSucceeded {
		t.Fatalf("api job = %+v", api.Record)
	}
	if !strings.HasPrefix(api.Record.Folder, "j-") || len(api.Record.Folder) != 10 {
		t.Fatalf("api folder = %q", api.Record.Folder)
	}
	if api.Record.StartedAt == nil || api.Record.FinishedAt == nil {
		t.Fatalf("api times not parsed")
	}
	if len(api.Attempts) != 1 || len(api.Attempts[0].Steps) != 2 {
		t.Fatalf("api attempts/steps = %+v", api.Attempts)
	}
	db := in.Jobs[1]
	if db.Record.Status != nodes.StatusFailed || db.Record.LastError != "boom" {
		t.Fatalf("db job = %+v", db.Record)
	}
}

func TestFromLegacyStateSealsEndToEnd(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	store := objectstore.NewMemStore("")
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: t.TempDir(), Clock: clock.Fixed{}})
	if err != nil {
		t.Fatalf("refs: %v", err)
	}
	sealer := execseal.New(nodewriter.New(store, refs))

	st, meta := sampleState()
	in := FromLegacyState(revID(), "trg_1", "", "run-001", st, meta)
	id, err := sealer.Seal(ctx, in)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	// Read execution.json back and confirm the rolled-up summary.
	entries, _ := store.GetTree(ctx, id)
	var execBlob objectstore.ObjectID
	for _, e := range entries {
		if e.Name == "execution.json" {
			execBlob = e.ID
		}
	}
	_, body, _ := store.Get(ctx, execBlob)
	for _, want := range []string{`"jobsTotal":2`, `"jobsSucceeded":1`, `"jobsFailed":1`, `"stepsTotal":3`, `"status":"failed"`} {
		if !strings.Contains(string(body), want) {
			t.Fatalf("execution.json missing %q:\n%s", want, body)
		}
	}
}

func TestMapStatus(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"success": nodes.StatusSucceeded, "succeeded": nodes.StatusSucceeded, "OK": nodes.StatusSucceeded,
		"failed": nodes.StatusFailed, "ERROR": nodes.StatusFailed,
		"cancelled": nodes.StatusCancelled, "skipped": nodes.StatusCancelled,
		"running": nodes.StatusRunning, "in-progress": nodes.StatusRunning,
		"": nodes.StatusPending, "weird": nodes.StatusPending,
	}
	for in, want := range cases {
		if got := mapStatus(in); got != want {
			t.Fatalf("mapStatus(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestTerminalStatus(t *testing.T) {
	t.Parallel()
	// Terminal meta status passes through.
	if got := terminalStatus(nil, &state.ExecMetadata{Status: "success"}); got != nodes.StatusSucceeded {
		t.Fatalf("terminal meta = %q", got)
	}
	// Non-terminal meta with a failed tally → failed.
	if got := terminalStatus(nil, &state.ExecMetadata{Status: "running", JobFailed: 2}); got != nodes.StatusFailed {
		t.Fatalf("non-terminal+tally = %q", got)
	}
	// Nil meta, a failed job → failed.
	st := &state.ExecState{Jobs: map[string]*state.JobState{"a": {Status: "failed"}}}
	if got := terminalStatus(st, nil); got != nodes.StatusFailed {
		t.Fatalf("failed job = %q", got)
	}
	// Everything clean → succeeded.
	if got := terminalStatus(&state.ExecState{Jobs: map[string]*state.JobState{"a": {Status: "success"}}}, &state.ExecMetadata{Status: "running"}); got != nodes.StatusSucceeded {
		t.Fatalf("clean = %q", got)
	}
}

func TestParseTimeAndHelpers(t *testing.T) {
	t.Parallel()
	if !parseTime("").IsZero() || !parseTime("garbage").IsZero() {
		t.Fatalf("bad times should be zero")
	}
	if parseTime("2026-06-02T10:00:00Z").IsZero() {
		t.Fatalf("valid time parsed as zero")
	}
	if parseTimePtr("") != nil {
		t.Fatalf("empty time ptr should be nil")
	}
	if parseTimePtr("2026-06-02T10:00:00Z") == nil {
		t.Fatalf("valid time ptr should be non-nil")
	}
	if firstNonEmpty("", "", "x") != "x" || firstNonEmpty("", "") != "" {
		t.Fatalf("firstNonEmpty wrong")
	}
}

func TestFromLegacyStateNilInputs(t *testing.T) {
	t.Parallel()
	in := FromLegacyState(revID(), "", "exec-x", "", nil, nil)
	if in.Status != nodes.StatusSucceeded {
		t.Fatalf("nil inputs status = %q, want succeeded", in.Status)
	}
	if in.ExecutionID != "exec-x" {
		t.Fatalf("execID fallback = %q", in.ExecutionID)
	}
	if in.Jobs != nil {
		t.Fatalf("nil state should yield no jobs")
	}
	// Job folders are deterministic.
	if jobFolder("api@deploy") != jobFolder("api@deploy") {
		t.Fatalf("jobFolder not deterministic")
	}
}
