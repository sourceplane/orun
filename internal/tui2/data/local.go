package data

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/sourceplane/orun/internal/agent/live"
	"github.com/sourceplane/orun/internal/cockpit/bridge"
	"github.com/sourceplane/orun/internal/cockpit/catalogread"
	"github.com/sourceplane/orun/internal/cockpit/viewmodel"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
	"github.com/sourceplane/orun/internal/objread"
)

// LocalConfig locates the workspace a LocalSource reads.
type LocalConfig struct {
	// OrunRoot is the absolute .orun directory (state store root).
	OrunRoot string
	// WorkspaceRoot is the intent root the change overlay diffs against.
	WorkspaceRoot string
}

// LocalSource reads the content-addressed object graph directly — the
// same seams the CLI uses (bridge, catalogread, live), never the orun
// binary. Change notifications come from the fs watcher over the ref
// directories; polling exists only as the watcher-failure fallback.
type LocalSource struct {
	cfg   LocalConfig
	watch *watcher
}

// NewLocal constructs a LocalSource. Construction is cheap and never
// fails: an empty or missing workspace renders as empty surfaces that
// populate when the store appears (the watcher covers creation).
func NewLocal(cfg LocalConfig) *LocalSource {
	return &LocalSource{cfg: cfg, watch: newWatcher(cfg.OrunRoot)}
}

// Capabilities implements Source: local sources execute locally.
func (s *LocalSource) Capabilities() Caps { return Caps{Execute: true} }

// Scope implements Source.
func (s *LocalSource) Scope() string { return "local" }

// openModel opens the object store + refs under .orun/objectmodel.
func (s *LocalSource) openModel() (*objectstore.LocalStore, *refstore.LocalRefStore, error) {
	root := filepath.Join(s.cfg.OrunRoot, "objectmodel")
	if _, err := os.Stat(filepath.Join(root, "objects")); err != nil {
		return nil, nil, fmt.Errorf("no object model at %s", root)
	}
	store, err := objectstore.NewLocalStore(objectstore.LocalConfig{Root: root})
	if err != nil {
		return nil, nil, err
	}
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: root, Writer: "tui2"})
	if err != nil {
		return nil, nil, err
	}
	return store, refs, nil
}

// Catalog implements Source via the shared catalogread seam (catalog +
// change overlay in one view).
func (s *LocalSource) Catalog(ctx context.Context) (viewmodel.CatalogView, error) {
	store, refs, err := s.openModel()
	if err != nil {
		return viewmodel.CatalogView{}, err
	}
	return catalogread.New(store, refs, s.cfg.WorkspaceRoot).CatalogView(ctx, true)
}

// reader opens the objread seam over the object model.
func (s *LocalSource) reader() (*objread.Reader, error) {
	store, refs, err := s.openModel()
	if err != nil {
		return nil, err
	}
	return objread.New(store, refs, filepath.Join(s.cfg.OrunRoot, "objectmodel")), nil
}

// Runs implements Source.
func (s *LocalSource) Runs(ctx context.Context) (viewmodel.RunListView, error) {
	r, err := s.reader()
	if err != nil {
		return viewmodel.RunListView{}, err
	}
	return bridge.LoadRunListView(ctx, bridge.FromObjectReader(r))
}

// Run implements Source.
func (s *LocalSource) Run(ctx context.Context, execID string) (viewmodel.RunView, error) {
	r, err := s.reader()
	if err != nil {
		return viewmodel.RunView{}, err
	}
	return bridge.LoadRunView(ctx, bridge.FromObjectReader(r), execID)
}

// Sessions implements Source: the pid-swept live registry.
func (s *LocalSource) Sessions(context.Context) ([]live.Entry, error) {
	return live.List(filepath.Join(s.cfg.OrunRoot, "agents", "live"))
}

// Subscribe implements Source.
func (s *LocalSource) Subscribe(ctx context.Context, topics ...Topic) (<-chan Delta, error) {
	return s.watch.subscribe(ctx, topics), nil
}

// Close implements Source.
func (s *LocalSource) Close() error {
	s.watch.close()
	return nil
}
