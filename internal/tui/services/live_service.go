package services

import (
	"context"
	"errors"

	"github.com/sourceplane/orun/internal/cockpit/bridge"
	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
	"github.com/sourceplane/orun/internal/state"
	"github.com/sourceplane/orun/internal/statebackend"
)

// LiveServiceConfig holds the dependencies LiveOrunService needs.
type LiveServiceConfig struct {
	IntentFile string
	IntentRoot string
	ConfigDir  string
	Store      *state.Store
	Backend    statebackend.Backend
	Version    string
}

// LiveOrunService is the concrete OrunService implementation that calls
// Orun internal packages directly. It never shells out to the orun binary.
type LiveOrunService struct {
	cfg LiveServiceConfig
}

// NewLiveOrunService constructs a LiveOrunService.
func NewLiveOrunService(cfg LiveServiceConfig) *LiveOrunService {
	return &LiveOrunService{cfg: cfg}
}

// Compile-time interface check.
var _ OrunService = (*LiveOrunService)(nil)

// errNotImplemented is returned by the stub methods that arrive in later
// Phase 1 / Phase 2 tasks.
var errNotImplemented = errors.New("not implemented")

// Describe: Phase 3 (Task 21) will replace this stub.
func (s *LiveOrunService) Describe(ctx context.Context, ref ResourceRef) (*ResourceDescription, error) {
	return nil, errNotImplemented
}

// RunView returns the cockpit view-model for a single execution. This is
// the unified read path shared with `orun status`; both surfaces draw the
// same struct.
func (s *LiveOrunService) RunView(ctx context.Context, execID string) (viewmodel.RunView, error) {
	src := s.source()
	if src == nil {
		return viewmodel.RunView{}, errors.New("no state source configured")
	}
	return bridge.LoadRunView(ctx, src, execID)
}

// RunListView returns the cockpit view-model for the execution history.
func (s *LiveOrunService) RunListView(ctx context.Context) (viewmodel.RunListView, error) {
	src := s.source()
	if src == nil {
		return viewmodel.RunListView{}, errors.New("no state source configured")
	}
	return bridge.LoadRunListView(ctx, src)
}

func (s *LiveOrunService) source() bridge.Source {
	if s.cfg.Backend != nil {
		return bridge.FromBackend(s.cfg.Backend)
	}
	if s.cfg.Store != nil {
		return bridge.FromStore(s.cfg.Store)
	}
	return nil
}
