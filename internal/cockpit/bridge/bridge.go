// Package bridge is the single read-path from .orun on-disk state (or a
// remote backend) into cockpit view-models. Both the CLI and TUI call
// here — there is no other route from raw state to rendered UI.
package bridge

import (
	"context"
	"fmt"

	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/objread"
	"github.com/sourceplane/orun/internal/objview"
	"github.com/sourceplane/orun/internal/statebackend"
)

// Source abstracts the place state lives. The remote statebackend and the
// content-addressed object model both implement it.
type Source interface {
	LoadRun(ctx context.Context, execID string) (*execmodel.ExecMetadata, *execmodel.ExecState, error)
	ListRuns(ctx context.Context) ([]execmodel.ExecEntry, error)
}

// FromBackend wraps a remote statebackend.Backend as a Source.
func FromBackend(b statebackend.Backend) Source { return &backendSource{b: b} }

// FromObjectReader wraps the object-model read layer as a Source, adapting its
// native views into the execmodel shapes the view-models render.
func FromObjectReader(r *objread.Reader) Source { return &objReaderSource{r: r} }

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

type objReaderSource struct{ r *objread.Reader }

func (a *objReaderSource) LoadRun(ctx context.Context, execID string) (*execmodel.ExecMetadata, *execmodel.ExecState, error) {
	if a.r == nil {
		return nil, nil, fmt.Errorf("nil object reader")
	}
	ref := execID
	if ref == "" {
		ref = "executions/latest"
	}
	v, err := a.r.Get(ctx, ref)
	if err != nil {
		return nil, nil, err
	}
	return objview.ToMeta(v), objview.ToState(v), nil
}

func (a *objReaderSource) ListRuns(ctx context.Context) ([]execmodel.ExecEntry, error) {
	if a.r == nil {
		return nil, fmt.Errorf("nil object reader")
	}
	views, err := a.r.List(ctx)
	if err != nil {
		return nil, err
	}
	return objview.ToEntries(views), nil
}

type backendSource struct{ b statebackend.Backend }

func (a *backendSource) LoadRun(ctx context.Context, execID string) (*execmodel.ExecMetadata, *execmodel.ExecState, error) {
	if a.b == nil {
		return nil, nil, fmt.Errorf("nil backend")
	}
	st, meta, err := a.b.LoadRunState(ctx, execID)
	return meta, st, err
}

func (a *backendSource) ListRuns(_ context.Context) ([]execmodel.ExecEntry, error) {
	// Remote backend doesn't list — Phase 2 will add this when we wire
	// `orun status --all --remote-state`. For now, callers fall back to
	// the local store.
	return nil, fmt.Errorf("remote backend does not support listing")
}
