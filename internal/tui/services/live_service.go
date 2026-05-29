package services

import (
	"context"
	"errors"

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
