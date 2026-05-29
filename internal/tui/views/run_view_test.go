package views

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/tui/services"
)

func TestRunViewModel_HeaderShowsDryRun(t *testing.T) {
	m := NewRunViewModel()
	ch := make(chan services.RunEvent)
	close(ch)
	m, _ = m.StartStream(ch, true)
	view := m.View()
	if !strings.Contains(view, "Activity") || !strings.Contains(view, "dry-run") {
		t.Fatalf("expected dry-run header, got:\n%s", view)
	}
}

func TestRunViewModel_HeaderHidesDryRunWhenFalse(t *testing.T) {
	m := NewRunViewModel()
	if strings.Contains(m.View(), "(dry-run)") {
		t.Fatal("idle view should not show (dry-run) header marker")
	}
}

func TestRunViewModel_JobStartedAddsRow(t *testing.T) {
	m := NewRunViewModel()
	ch := make(chan services.RunEvent, 4)
	m, _ = m.StartStream(ch, true)

	m, _ = m.Update(services.RunEventMsg{Event: services.RunEvent{
		Kind:      services.RunEventJobStarted,
		JobID:     "alpha-dev",
		Component: "alpha",
		Env:       "dev",
	}})
	rows := m.Rows()
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	if rows[0].Status != "running" || rows[0].Component != "alpha" || rows[0].Env != "dev" {
		t.Errorf("unexpected row: %+v", rows[0])
	}
}

func TestRunViewModel_JobCompletedUpdatesStatus(t *testing.T) {
	m := NewRunViewModel()
	ch := make(chan services.RunEvent, 4)
	m, _ = m.StartStream(ch, true)
	m, _ = m.Update(services.RunEventMsg{Event: services.RunEvent{Kind: services.RunEventJobStarted, JobID: "j1"}})
	m, _ = m.Update(services.RunEventMsg{Event: services.RunEvent{Kind: services.RunEventJobCompleted, JobID: "j1"}})
	if got := m.Rows()[0].Status; got != "completed" {
		t.Errorf("status = %q, want completed", got)
	}
}

func TestRunViewModel_JobFailedCarriesError(t *testing.T) {
	m := NewRunViewModel()
	ch := make(chan services.RunEvent, 4)
	m, _ = m.StartStream(ch, true)
	m, _ = m.Update(services.RunEventMsg{Event: services.RunEvent{Kind: services.RunEventJobStarted, JobID: "j1"}})
	m, _ = m.Update(services.RunEventMsg{Event: services.RunEvent{
		Kind:  services.RunEventJobFailed,
		JobID: "j1",
		Error: "boom",
	}})
	row := m.Rows()[0]
	if row.Status != "failed" || row.Err != "boom" {
		t.Errorf("unexpected row: %+v", row)
	}
	if !strings.Contains(m.View(), "boom") {
		t.Errorf("View should surface error text")
	}
}

func TestRunViewModel_RunDoneSetsDoneAndStopsRearm(t *testing.T) {
	m := NewRunViewModel()
	ch := make(chan services.RunEvent, 4)
	m, _ = m.StartStream(ch, true)
	if m.Done() {
		t.Fatal("expected not done at start")
	}
	m, cmd := m.Update(services.RunEventMsg{Event: services.RunEvent{Kind: services.RunEventRunDone}})
	if !m.Done() {
		t.Fatal("expected done after RunEventRunDone")
	}
	if cmd != nil {
		t.Fatal("expected nil cmd after RunEventRunDone (no re-arm)")
	}
}

func TestRunViewModel_InitNoEventsReturnsNil(t *testing.T) {
	m := NewRunViewModel()
	if cmd := m.Init(); cmd != nil {
		t.Fatal("Init with nil Events should return nil cmd")
	}
}

func TestRunViewModel_StartStreamReturnsWaitCmd(t *testing.T) {
	m := NewRunViewModel()
	ch := make(chan services.RunEvent, 1)
	ch <- services.RunEvent{Kind: services.RunEventJobStarted, JobID: "j1"}
	m, cmd := m.StartStream(ch, true)
	if cmd == nil {
		t.Fatal("expected non-nil wait cmd")
	}
	// Drive it once to confirm it produces a RunEventMsg.
	msg := cmd()
	if _, ok := msg.(services.RunEventMsg); !ok {
		t.Fatalf("msg = %T, want RunEventMsg", msg)
	}
	_ = m
}
