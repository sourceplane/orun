package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/state"
)

const minimalIntent = `apiVersion: orun.dev/v1
kind: Intent
metadata:
  name: tui-fixture
discovery:
  roots: []
environments:
  dev:
    selectors:
      components: ["*"]
components:
  - name: alpha
    type: terraform
    domain: infra
    enabled: true
    path: components/alpha
    subscribe:
      environments:
        - name: dev
          profile: plan-only
`

func TestLiveOrunService_LoadWorkspace_ReadsIntent(t *testing.T) {
	dir := t.TempDir()
	intentPath := filepath.Join(dir, "intent.yaml")
	if err := os.WriteFile(intentPath, []byte(minimalIntent), 0o644); err != nil {
		t.Fatal(err)
	}

	store := state.NewStore(dir)
	svc := NewLiveOrunService(LiveServiceConfig{
		IntentFile: intentPath,
		IntentRoot: dir,
		Store:      store,
	})

	snap, err := svc.LoadWorkspace(context.Background(), WorkspaceRequest{})
	if err != nil {
		t.Fatalf("LoadWorkspace: %v", err)
	}
	if snap == nil {
		t.Fatal("expected snapshot, got nil")
	}
	if snap.IntentName != "tui-fixture" {
		t.Errorf("IntentName = %q, want %q", snap.IntentName, "tui-fixture")
	}
	if got := len(snap.Components); got != 1 {
		t.Fatalf("Components = %d, want 1", got)
	}
	c := snap.Components[0]
	if c.Name != "alpha" || c.Type != "terraform" || c.Domain != "infra" {
		t.Errorf("unexpected component: %+v", c)
	}
	if len(c.Envs) != 1 || c.Envs[0] != "dev" {
		t.Errorf("Envs = %v, want [dev]", c.Envs)
	}
	if c.Profile != "plan-only" {
		t.Errorf("Profile = %q, want %q", c.Profile, "plan-only")
	}
	if len(snap.Environments) != 1 || snap.Environments[0] != "dev" {
		t.Errorf("Environments = %v, want [dev]", snap.Environments)
	}
}

func TestLiveOrunService_LoadWorkspace_RespectsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	svc := NewLiveOrunService(LiveServiceConfig{})
	if _, err := svc.LoadWorkspace(ctx, WorkspaceRequest{}); err == nil {
		t.Fatal("expected context cancellation error")
	}
}

func TestLiveOrunService_ListRuns_EmptyStore(t *testing.T) {
	dir := t.TempDir()
	svc := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: orunDir(dir)})
	runs, err := svc.ListRuns(context.Background(), ListRunsRequest{})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 0 {
		t.Fatalf("want 0 runs, got %d", len(runs))
	}
}

func TestLiveOrunService_ListRuns_RemoteStateNotImplemented(t *testing.T) {
	svc := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: orunDir(t.TempDir())})
	_, err := svc.ListRuns(context.Background(), ListRunsRequest{RemoteState: true})
	if err == nil {
		t.Fatal("expected error for RemoteState=true, got nil")
	}
}

// TestLiveOrunService_ListRuns_FromObjectGraph is the U1 coverage: a sealed
// execution seeded into the object graph appears in history with its plan name,
// components, and counts resolved from the revision's plan.json.
func TestLiveOrunService_ListRuns_FromObjectGraph(t *testing.T) {
	dir := t.TempDir()
	older := time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC)
	seedObjectExecution(t, dir, seedExec{
		ExecID:    "exec_old",
		PlanName:  "nightly",
		StartedAt: older,
		Jobs: []seedJob{
			{ID: "api@deploy", Component: "api", Steps: []seedStep{{ID: "build"}}},
		},
	})
	seedObjectExecution(t, dir, seedExec{
		ExecID:    "exec_new",
		PlanName:  "release",
		StartedAt: older.Add(time.Hour),
		Jobs: []seedJob{
			{ID: "web@deploy", Component: "web", Steps: []seedStep{{ID: "build"}, {ID: "test"}}},
			{ID: "api@deploy", Component: "api", Steps: []seedStep{{ID: "build"}}},
		},
	})

	svc := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: orunDir(dir)})
	runs, err := svc.ListRuns(context.Background(), ListRunsRequest{})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("want 2 runs, got %d: %+v", len(runs), runs)
	}
	// Newest-first.
	if runs[0].ExecID != "exec_new" || runs[1].ExecID != "exec_old" {
		t.Fatalf("order wrong: %s, %s", runs[0].ExecID, runs[1].ExecID)
	}
	newRun := runs[0]
	if newRun.PlanName != "release" {
		t.Errorf("PlanName = %q, want release", newRun.PlanName)
	}
	if newRun.Status != "completed" {
		t.Errorf("Status = %q, want completed", newRun.Status)
	}
	if newRun.JobTotal != 2 || newRun.JobDone != 2 {
		t.Errorf("counts: total=%d done=%d, want 2/2", newRun.JobTotal, newRun.JobDone)
	}
	if got := newRun.Components; len(got) != 2 || got[0] != "web" || got[1] != "api" {
		t.Errorf("Components = %v, want [web api]", got)
	}
	if newRun.PlanID == "" {
		t.Error("PlanID (short revision id) should be populated")
	}
}

// TestLiveOrunService_ListRuns_Limit verifies the Limit knob truncates the
// newest-first list.
func TestLiveOrunService_ListRuns_Limit(t *testing.T) {
	dir := t.TempDir()
	base := time.Date(2026, 6, 3, 9, 0, 0, 0, time.UTC)
	for i := 0; i < 3; i++ {
		seedObjectExecution(t, dir, seedExec{
			ExecID:    "exec_" + string(rune('a'+i)),
			StartedAt: base.Add(time.Duration(i) * time.Hour),
			Jobs:      []seedJob{{ID: "j", Component: "c"}},
		})
	}
	svc := NewLiveOrunService(LiveServiceConfig{ObjectModelRoot: orunDir(dir)})
	runs, err := svc.ListRuns(context.Background(), ListRunsRequest{Limit: 2})
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	if len(runs) != 2 {
		t.Fatalf("want 2 runs (limit), got %d", len(runs))
	}
}

func TestLiveOrunService_TailLogs_GuardsAndFollowSupported(t *testing.T) {
	svc := NewLiveOrunService(LiveServiceConfig{Store: state.NewStore(t.TempDir())})
	if _, err := svc.TailLogs(context.Background(), LogRequest{ExecID: "e", JobID: "j", RemoteState: true}); err == nil {
		t.Error("expected error for RemoteState=true")
	}
	if _, err := svc.TailLogs(context.Background(), LogRequest{}); err == nil {
		t.Error("expected error for missing ExecID")
	}
	// Follow is now supported even when the execution dir does not exist yet
	// (live run not yet flushed): it returns a channel and waits for files.
	ctx, cancel := context.WithCancel(context.Background())
	ch, err := svc.TailLogs(ctx, LogRequest{ExecID: "e", JobID: "j", Follow: true})
	if err != nil {
		t.Fatalf("Follow=true should be supported, got %v", err)
	}
	cancel()
	// Draining must terminate once the context is cancelled.
	for range ch {
	}
}

func TestLiveOrunService_TailLogs_StreamsLocalFile(t *testing.T) {
	dir := t.TempDir()
	store := state.NewStore(dir)
	if err := store.EnsureDirs(); err != nil {
		t.Fatal(err)
	}
	execID := "exec-test"
	jobID := "job-1"
	stepID := "step-1"
	logDir := store.LogDir(execID, jobID)
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-create the exec dir so ResolveExecID accepts the literal ID.
	if err := os.MkdirAll(store.ExecPath(execID), 0o755); err != nil {
		t.Fatal(err)
	}
	logPath := store.LogPath(execID, jobID, stepID)
	if err := os.WriteFile(logPath, []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	svc := NewLiveOrunService(LiveServiceConfig{Store: store})
	ch, err := svc.TailLogs(context.Background(), LogRequest{
		ExecID: execID,
		JobID:  jobID,
		StepID: stepID,
	})
	if err != nil {
		t.Fatalf("TailLogs: %v", err)
	}
	var lines []string
	for ev := range ch {
		lines = append(lines, ev.Line)
	}
	if len(lines) != 2 || lines[0] != "line one" || lines[1] != "line two" {
		t.Fatalf("got lines %v", lines)
	}
}
