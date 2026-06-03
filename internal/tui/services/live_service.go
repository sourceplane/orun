package services

import (
	"context"
	"errors"
	"os"
	"path/filepath"

	"github.com/sourceplane/orun/internal/cockpit/bridge"
	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
	"github.com/sourceplane/orun/internal/cockpit/watch"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objread"
	"github.com/sourceplane/orun/internal/statebackend"
)

// LiveServiceConfig holds the dependencies LiveOrunService needs.
type LiveServiceConfig struct {
	IntentFile string
	IntentRoot string
	ConfigDir  string
	// ObjectModelRoot is the absolute .orun directory whose object graph the
	// TUI reads and writes (executions, history, logs). The content-addressed
	// model is the canonical local source; the legacy file store is gone.
	ObjectModelRoot string
	// Backend is the remote state backend, set only under --remote-state. When
	// nil, the local object graph at ObjectModelRoot is the source.
	Backend statebackend.Backend
	Version string
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
	if r, ok := s.objReader(); ok {
		return bridge.FromObjectReader(r)
	}
	return nil
}

// objReader opens an object-model reader over the workspace's .orun graph. It
// returns ok=false when no ObjectModelRoot is configured or the workspace has
// no object graph on disk yet (an empty workspace), mirroring cmd/orun's
// openObjectReader. The remote (Backend) path bypasses this entirely.
func (s *LiveOrunService) objReader() (*objread.Reader, bool) {
	if s.cfg.ObjectModelRoot == "" {
		return nil, false
	}
	root := filepath.Join(s.cfg.ObjectModelRoot, "objectmodel")
	// Only adopt the object model if it actually has content; opening an empty
	// store would otherwise hide an absent-history workspace behind errors.
	if _, err := os.Stat(filepath.Join(root, "objects")); err != nil {
		return nil, false
	}
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		return nil, false
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Writer: "tui"})
	if err != nil {
		return nil, false
	}
	return objread.New(store, refs, root), true
}

// WatchRunView subscribes to live cockpit updates for execID. Both the
// CLI's `status --watch` and TUI panes consume this channel — same poll
// loop, same terminal-state semantics.
func (s *LiveOrunService) WatchRunView(ctx context.Context, execID string, opts watch.Options) (<-chan watch.Update, error) {
	src := s.source()
	if src == nil {
		return nil, errors.New("no state source configured")
	}
	if opts.ExecID == "" {
		opts.ExecID = execID
	}
	return watch.Run(ctx, src, opts), nil
}
