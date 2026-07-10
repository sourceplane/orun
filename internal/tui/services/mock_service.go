package services

import "context"

// MockOrunService satisfies OrunService with function-field hooks so unit
// tests can configure return values per call.
type MockOrunService struct {
	LoadWorkspaceFn  func(ctx context.Context, req WorkspaceRequest) (*WorkspaceSnapshot, error)
	GeneratePlanFn   func(ctx context.Context, req PlanRequest) (*PlanResult, error)
	RunPlanFn        func(ctx context.Context, req RunRequest) (<-chan RunEvent, error)
	ListRunsFn       func(ctx context.Context, req ListRunsRequest) ([]RunSummary, error)
	GetRunDetailFn   func(ctx context.Context, req RunDetailRequest) (RunDetail, error)
	DescribeFn       func(ctx context.Context, ref ResourceRef) (*ResourceDescription, error)
	TailLogsFn       func(ctx context.Context, req LogRequest) (<-chan LogEvent, error)
	RefreshCatalogFn func(ctx context.Context, force bool) (CatalogRefreshResult, error)
	CatalogStaleFn   func(ctx context.Context) (bool, error)
	LoadCatalogFn    func(ctx context.Context) (*CatalogSnapshot, error)
	LoadAgentTypesFn func(ctx context.Context) ([]AgentTypeRow, error)
	LiveSessionsFn   func() ([]LiveSessionRow, error)
}

func (m *MockOrunService) LiveSessions() ([]LiveSessionRow, error) {
	if m.LiveSessionsFn != nil {
		return m.LiveSessionsFn()
	}
	return nil, nil
}

func (m *MockOrunService) LoadCatalog(ctx context.Context) (*CatalogSnapshot, error) {
	if m.LoadCatalogFn != nil {
		return m.LoadCatalogFn(ctx)
	}
	return nil, nil
}

func (m *MockOrunService) LoadAgentTypes(ctx context.Context) ([]AgentTypeRow, error) {
	if m.LoadAgentTypesFn != nil {
		return m.LoadAgentTypesFn(ctx)
	}
	return nil, nil
}

func (m *MockOrunService) RefreshCatalog(ctx context.Context, force bool) (CatalogRefreshResult, error) {
	if m.RefreshCatalogFn != nil {
		return m.RefreshCatalogFn(ctx, force)
	}
	return CatalogRefreshResult{}, nil
}

func (m *MockOrunService) CatalogStale(ctx context.Context) (bool, error) {
	if m.CatalogStaleFn != nil {
		return m.CatalogStaleFn(ctx)
	}
	return false, nil
}

// Compile-time check.
var _ OrunService = (*MockOrunService)(nil)

func (m *MockOrunService) LoadWorkspace(ctx context.Context, req WorkspaceRequest) (*WorkspaceSnapshot, error) {
	if m.LoadWorkspaceFn != nil {
		return m.LoadWorkspaceFn(ctx, req)
	}
	return &WorkspaceSnapshot{}, nil
}

func (m *MockOrunService) GeneratePlan(ctx context.Context, req PlanRequest) (*PlanResult, error) {
	if m.GeneratePlanFn != nil {
		return m.GeneratePlanFn(ctx, req)
	}
	return &PlanResult{}, nil
}

func (m *MockOrunService) RunPlan(ctx context.Context, req RunRequest) (<-chan RunEvent, error) {
	if m.RunPlanFn != nil {
		return m.RunPlanFn(ctx, req)
	}
	ch := make(chan RunEvent)
	close(ch)
	return ch, nil
}

func (m *MockOrunService) ListRuns(ctx context.Context, req ListRunsRequest) ([]RunSummary, error) {
	if m.ListRunsFn != nil {
		return m.ListRunsFn(ctx, req)
	}
	return nil, nil
}

func (m *MockOrunService) GetRunDetail(ctx context.Context, req RunDetailRequest) (RunDetail, error) {
	if m.GetRunDetailFn != nil {
		return m.GetRunDetailFn(ctx, req)
	}
	return RunDetail{ExecID: req.ExecID}, nil
}

func (m *MockOrunService) Describe(ctx context.Context, ref ResourceRef) (*ResourceDescription, error) {
	if m.DescribeFn != nil {
		return m.DescribeFn(ctx, ref)
	}
	return &ResourceDescription{}, nil
}

func (m *MockOrunService) TailLogs(ctx context.Context, req LogRequest) (<-chan LogEvent, error) {
	if m.TailLogsFn != nil {
		return m.TailLogsFn(ctx, req)
	}
	ch := make(chan LogEvent)
	close(ch)
	return ch, nil
}
