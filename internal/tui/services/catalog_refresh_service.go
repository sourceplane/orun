package services

// catalog_refresh_service.go is the cockpit-side catalog resolve (the
// "cockpit-side resolve" follow-up noted in catalog_source.go): it keeps the
// object-model catalog the cockpit reads fresh for the current tree, instead of
// relying on an out-of-band `orun plan`/`run`/`catalog refresh` having run. It
// delegates to internal/catalogrefresh — the same engine the CLI uses — so the
// cockpit and CLI converge on the same content-addressed catalog id.

import (
	"context"
	"path/filepath"

	"github.com/sourceplane/orun/internal/catalogrefresh"
)

// CatalogRefreshResult reports the outcome of a cockpit-triggered RefreshCatalog.
type CatalogRefreshResult struct {
	Refreshed bool   // a resolve + object-model write ran
	Fresh     bool   // already up to date for the current tree — no resolve
	Skipped   bool   // another refresh held the lock — this call did nothing
	CatalogID string // the object-model catalog id (when known)
}

// RefreshCatalog implements OrunService.RefreshCatalog.
func (s *LiveOrunService) RefreshCatalog(ctx context.Context, force bool) (CatalogRefreshResult, error) {
	if s.cfg.ObjectModelRoot == "" || s.cfg.IntentRoot == "" {
		return CatalogRefreshResult{}, nil
	}
	objRoot := filepath.Join(s.cfg.ObjectModelRoot, "objectmodel")
	res, err := catalogrefresh.EnsureFresh(ctx, objRoot, s.cfg.IntentRoot, force, catalogrefresh.Config{
		OrunVersion: s.cfg.Version,
	})
	if err != nil {
		return CatalogRefreshResult{}, err
	}
	return CatalogRefreshResult{
		Refreshed: res.Refreshed,
		Fresh:     res.Fresh,
		Skipped:   res.Skipped,
		CatalogID: res.CatalogID,
	}, nil
}

// CatalogStale implements OrunService.CatalogStale (read-only).
func (s *LiveOrunService) CatalogStale(ctx context.Context) (bool, error) {
	if s.cfg.ObjectModelRoot == "" || s.cfg.IntentRoot == "" {
		return false, nil
	}
	objRoot := filepath.Join(s.cfg.ObjectModelRoot, "objectmodel")
	return catalogrefresh.IsStale(ctx, objRoot, s.cfg.IntentRoot)
}
