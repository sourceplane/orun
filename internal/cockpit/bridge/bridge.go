// Package bridge is the single read-path from .orun on-disk state (or a
// remote backend) into cockpit view-models. Both the CLI and TUI call
// here — there is no other route from raw state to rendered UI.
package bridge

import (
	"context"
	"fmt"

	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
	"github.com/sourceplane/orun/internal/state"
	"github.com/sourceplane/orun/internal/statebackend"
)

// Source abstracts the place state lives. The local store and the remote
// statebackend both implement it.
type Source interface {
	LoadRun(ctx context.Context, execID string) (*state.ExecMetadata, *state.ExecState, error)
	ListRuns(ctx context.Context) ([]state.ExecEntry, error)
}

// FromStore wraps a local *state.Store as a Source.
func FromStore(s *state.Store) Source { return &storeSource{s: s} }

// FromBackend wraps a remote statebackend.Backend as a Source.
func FromBackend(b statebackend.Backend) Source { return &backendSource{b: b} }

// LoadRunView fetches metadata + state for execID and builds the cockpit
// view-model in one call.
func LoadRunView(ctx context.Context, src Source, execID string) (viewmodel.RunView, error) {
	if src == nil {
		return viewmodel.RunView{}, fmt.Errorf("nil source")
	}
	meta, st, err := src.LoadRun(ctx, execID)
	if err != nil {
		return viewmodel.RunView{}, err
	}
	return viewmodel.BuildRunView(execID, meta, st), nil
}

// LoadRunListView returns the cockpit list view of every known run.
func LoadRunListView(ctx context.Context, src Source) (viewmodel.RunListView, error) {
	if src == nil {
		return viewmodel.RunListView{}, fmt.Errorf("nil source")
	}
	entries, err := src.ListRuns(ctx)
	if err != nil {
		return viewmodel.RunListView{}, err
	}
	return viewmodel.BuildRunListView(entries), nil
}

// --- adapters --------------------------------------------------------

type storeSource struct{ s *state.Store }

func (a *storeSource) LoadRun(_ context.Context, execID string) (*state.ExecMetadata, *state.ExecState, error) {
	if a.s == nil {
		return nil, nil, fmt.Errorf("nil store")
	}
	meta, _ := a.s.LoadMetadata(execID)
	st, _ := a.s.LoadState(execID)
	return meta, st, nil
}

func (a *storeSource) ListRuns(_ context.Context) ([]state.ExecEntry, error) {
	if a.s == nil {
		return nil, fmt.Errorf("nil store")
	}
	return a.s.ListExecutions()
}

type backendSource struct{ b statebackend.Backend }

func (a *backendSource) LoadRun(ctx context.Context, execID string) (*state.ExecMetadata, *state.ExecState, error) {
	if a.b == nil {
		return nil, nil, fmt.Errorf("nil backend")
	}
	st, meta, err := a.b.LoadRunState(ctx, execID)
	return meta, st, err
}

func (a *backendSource) ListRuns(_ context.Context) ([]state.ExecEntry, error) {
	// Remote backend doesn't list — Phase 2 will add this when we wire
	// `orun status --all --remote-state`. For now, callers fall back to
	// the local store.
	return nil, fmt.Errorf("remote backend does not support listing")
}
