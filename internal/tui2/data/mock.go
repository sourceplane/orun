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
	StepLogs     map[string]string // key: execID/jobID/stepID
	Components   map[string]viewmodel.ComponentView
	LiveSessions []live.Entry
	Err          error // when set, every snapshot read fails with it

	watch *watcher
}

// NewMock returns an empty mock; seed the exported fields directly.
func NewMock() *MockSource {
	return &MockSource{
		RunViews:   make(map[string]viewmodel.RunView),
		StepLogs:   make(map[string]string),
		Components: make(map[string]viewmodel.ComponentView),
		watch:      newWatcher(""),
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

// Component implements Source.
func (m *MockSource) Component(_ context.Context, key string) (viewmodel.ComponentView, bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return viewmodel.ComponentView{}, false, m.Err
	}
	v, ok := m.Components[key]
	return v, ok, nil
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

// StepLog implements Source.
func (m *MockSource) StepLog(_ context.Context, execID, jobID, stepID string) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Err != nil {
		return nil, m.Err
	}
	return []byte(m.StepLogs[execID+"/"+jobID+"/"+stepID]), nil
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
	m.CatalogView = viewmodel.CatalogView{
		SourceID: "src_abc", CatalogID: "cat_abc", Overlay: true,
		Components: []viewmodel.ComponentRow{
			{Key: "checkout", Name: "checkout", Type: "service", Domain: "payments", Envs: []string{"production", "staging"}, DirectlyChanged: true},
			{Key: "payments", Name: "payments", Type: "service", Domain: "payments", Envs: []string{"production"}, Dependent: true, DependsOn: []string{"checkout"}},
			{Key: "web", Name: "web", Type: "frontend", Domain: "storefront", Envs: []string{"production"}},
		},
	}
	m.Components["checkout"] = viewmodel.ComponentView{
		Key: "checkout", Name: "checkout", Type: "service", Domain: "payments", Path: "services/checkout",
		Envs:      []viewmodel.EnvBinding{{Name: "production", Active: true}, {Name: "staging", Active: true}},
		DependsOn: []string{"payments-db"}, Watches: []string{"services/checkout/**"},
	}
	m.RunViews["exec_01J8Z3"] = viewmodel.RunView{
		ExecID: "exec_01J8Z3", PlanName: "deploy checkout", Status: "running",
		Trigger: "deploy", StartedAt: time.Now().Add(-2 * time.Minute),
		Counts: viewmodel.Counts{Total: 3, Completed: 1, Running: 1, Pending: 1},
		Jobs: []viewmodel.Job{
			{ID: "checkout@deploy", Component: "checkout", Environment: "production", Short: "deploy", Status: "completed",
				StartedAt: time.Now().Add(-2 * time.Minute), FinishedAt: time.Now().Add(-1 * time.Minute),
				Steps: []viewmodel.Step{{ID: "build", Status: "completed"}, {ID: "push", Status: "completed"}}},
			{ID: "payments@deploy", Component: "payments", Environment: "production", Short: "deploy", Status: "running",
				StartedAt: time.Now().Add(-1 * time.Minute),
				Steps:     []viewmodel.Step{{ID: "build", Status: "completed"}, {ID: "migrate", Status: "running"}, {ID: "push", Status: "pending"}}},
			{ID: "web@deploy", Component: "web", Environment: "production", Short: "deploy", Status: "pending",
				Steps: []viewmodel.Step{{ID: "build", Status: "pending"}}},
		},
	}
	m.StepLogs["exec_01J8Z3/checkout@deploy/build"] = "compiling checkout\n42 packages built\nimage sha256:abcd pushed"
	m.StepLogs["exec_01J8Z3/payments@deploy/migrate"] = "applying migration 0042_add_ledger\nERROR: retrying lock acquisition\nlock acquired"
	m.RunList = viewmodel.RunListView{Runs: []viewmodel.RunSummary{
		{ExecID: "exec_01J8Z3", PlanName: "deploy checkout", Status: "running", StartedAt: time.Now().Add(-2 * time.Minute)},
		{ExecID: "exec_01J8Z2", PlanName: "plan payments", Status: "completed", StartedAt: time.Now().Add(-50 * time.Minute), FinishedAt: time.Now().Add(-48 * time.Minute)},
		{ExecID: "exec_01J8Z1", PlanName: "deploy web", Status: "failed", StartedAt: time.Now().Add(-3 * time.Hour), FinishedAt: time.Now().Add(-3 * time.Hour).Add(4 * time.Minute)},
	}}
	return m
}
