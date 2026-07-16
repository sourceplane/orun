package data

import (
	"context"
	"sync"
	"time"

	"github.com/sourceplane/orun/internal/agent/live"
	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
)

// MockSource is the deterministic Source behind surface tests, goldens,
// and the demo program through TR7. Snapshots return the seeded fixtures;
// Emit injects deltas as if the workspace changed.
type MockSource struct {
	mu           sync.Mutex
	CatalogView  viewmodel.CatalogView
	RunList      viewmodel.RunListView
	RunViews     map[string]viewmodel.RunView
	LiveSessions []live.Entry
	Err          error // when set, every snapshot read fails with it

	watch *watcher
}

// NewMock returns an empty mock; seed the exported fields directly.
func NewMock() *MockSource {
	return &MockSource{
		RunViews: make(map[string]viewmodel.RunView),
		watch:    newWatcher(""),
	}
}

// Seed replaces the fixtures under lock and emits deltas for the touched
// topics, imitating a real workspace change.
func (m *MockSource) Seed(fn func(*MockSource), topics ...Topic) {
	m.mu.Lock()
	fn(m)
	m.mu.Unlock()
	for _, t := range topics {
		m.watch.emit(t, false)
	}
}

// Emit injects a raw delta.
func (m *MockSource) Emit(topic Topic, degraded bool) { m.watch.emit(topic, degraded) }

// Capabilities implements Source.
func (m *MockSource) Capabilities() Caps { return Caps{Execute: true} }

// Scope implements Source.
func (m *MockSource) Scope() string { return "mock" }

// Catalog implements Source.
func (m *MockSource) Catalog(context.Context) (viewmodel.CatalogView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.CatalogView, m.Err
}

// Runs implements Source.
func (m *MockSource) Runs(context.Context) (viewmodel.RunListView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RunList, m.Err
}

// Run implements Source.
func (m *MockSource) Run(_ context.Context, execID string) (viewmodel.RunView, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.RunViews[execID], m.Err
}

// Sessions implements Source.
func (m *MockSource) Sessions(context.Context) ([]live.Entry, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]live.Entry(nil), m.LiveSessions...), m.Err
}

// Subscribe implements Source.
func (m *MockSource) Subscribe(ctx context.Context, topics ...Topic) (<-chan Delta, error) {
	// The mock never starts the fs watcher: subscribe wires the fan-out
	// only, and deltas come from Seed/Emit.
	return m.watch.subscribe(ctx, topics), nil
}

// Close implements Source.
func (m *MockSource) Close() error { return nil }

// SampleMock returns a mock seeded with the standard fixture set the demo
// program and goldens share.
func SampleMock() *MockSource {
	m := NewMock()
	m.LiveSessions = []live.Entry{
		{SessionID: "as_local_01", PID: 4242, State: "running", AgentType: "implementer", Task: "fix flaky catalog test", Driver: "claude-code", StartedAt: time.Now().Add(-25 * time.Minute)},
		{SessionID: "as_local_02", PID: 4243, State: "running", AgentType: "reviewer", Task: "review PR #482", Driver: "claude-code", StartedAt: time.Now().Add(-5 * time.Minute)},
	}
	m.RunList = viewmodel.RunListView{Runs: []viewmodel.RunSummary{
		{ExecID: "exec_01J8Z3", PlanName: "deploy checkout", Status: "running", StartedAt: time.Now().Add(-2 * time.Minute)},
		{ExecID: "exec_01J8Z2", PlanName: "plan payments", Status: "completed", StartedAt: time.Now().Add(-50 * time.Minute), FinishedAt: time.Now().Add(-48 * time.Minute)},
		{ExecID: "exec_01J8Z1", PlanName: "deploy web", Status: "failed", StartedAt: time.Now().Add(-3 * time.Hour), FinishedAt: time.Now().Add(-3 * time.Hour).Add(4 * time.Minute)},
	}}
	return m
}
