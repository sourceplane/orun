package tui

import (
	"context"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/tui/services"
	"github.com/sourceplane/orun/internal/tui/views"
)

// refreshCatalogCmd forces a resolve and reports whether it changed.
func TestRefreshCatalogCmd_ForcesAndReports(t *testing.T) {
	var gotForce bool
	svc := &services.MockOrunService{
		RefreshCatalogFn: func(_ context.Context, force bool) (services.CatalogRefreshResult, error) {
			gotForce = force
			return services.CatalogRefreshResult{Refreshed: true}, nil
		},
	}
	msg := refreshCatalogCmd(svc, true)()
	if !gotForce {
		t.Error("refreshCatalogCmd(force=true) should pass force through")
	}
	cr, ok := msg.(catalogRefreshedMsg)
	if !ok || !cr.refreshed {
		t.Errorf("expected catalogRefreshedMsg{refreshed:true}, got %#v", msg)
	}
}

// A failed/no-op refresh reports refreshed=false (never blocks the cockpit).
func TestRefreshCatalogCmd_FailureIsNotRefreshed(t *testing.T) {
	svc := &services.MockOrunService{
		RefreshCatalogFn: func(_ context.Context, _ bool) (services.CatalogRefreshResult, error) {
			return services.CatalogRefreshResult{}, context.DeadlineExceeded
		},
	}
	if cr := refreshCatalogCmd(svc, false)().(catalogRefreshedMsg); cr.refreshed {
		t.Error("a failed refresh must report refreshed=false")
	}
}

// catalogRefreshedMsg{refreshed:true} reloads the workspace; false is a no-op.
func TestModel_CatalogRefreshed_ReloadsOnChangeOnly(t *testing.T) {
	loaded := make(chan struct{}, 1)
	svc := &services.MockOrunService{
		LoadWorkspaceFn: func(_ context.Context, _ services.WorkspaceRequest) (*services.WorkspaceSnapshot, error) {
			select {
			case loaded <- struct{}{}:
			default:
			}
			return &services.WorkspaceSnapshot{}, nil
		},
	}
	m := NewModel(svc)

	_, cmd := m.Update(catalogRefreshedMsg{refreshed: true})
	runCmd(cmd)
	select {
	case <-loaded:
	default:
		t.Fatal("a changed catalog refresh should reload the workspace")
	}

	_, cmd = m.Update(catalogRefreshedMsg{refreshed: false})
	runCmd(cmd)
	select {
	case <-loaded:
		t.Fatal("an unchanged catalog refresh must not reload")
	default:
	}
}

// The palette toggle flips and persists the auto-refresh preference.
func TestModel_PaletteAutoRefreshToggle(t *testing.T) {
	m := NewModel(&services.MockOrunService{})
	if m.prefs.AutoRefresh {
		t.Fatal("auto-refresh should default off")
	}
	next, _ := m.applyPaletteCommand(views.CommandPaletteCommand{ID: "catalog.autorefresh"})
	m = next.(Model)
	if !m.prefs.AutoRefresh {
		t.Fatal("toggle should enable auto-refresh")
	}
	next, _ = m.applyPaletteCommand(views.CommandPaletteCommand{ID: "catalog.autorefresh"})
	m = next.(Model)
	if m.prefs.AutoRefresh {
		t.Fatal("second toggle should disable auto-refresh")
	}
}

// The catalog.refresh palette command resolves the catalog.
func TestModel_PaletteCatalogRefresh(t *testing.T) {
	refreshed := make(chan bool, 1)
	svc := &services.MockOrunService{
		RefreshCatalogFn: func(_ context.Context, force bool) (services.CatalogRefreshResult, error) {
			refreshed <- force
			return services.CatalogRefreshResult{}, nil
		},
	}
	m := NewModel(svc)
	_, cmd := m.applyPaletteCommand(views.CommandPaletteCommand{ID: "catalog.refresh"})
	runCmd(cmd)
	select {
	case force := <-refreshed:
		if !force {
			t.Error("palette catalog.refresh should force a resolve")
		}
	default:
		t.Fatal("palette catalog.refresh should invoke RefreshCatalog")
	}
}

// checkCatalogStaleCmd reflects the service's read-only staleness probe.
func TestCheckCatalogStaleCmd(t *testing.T) {
	svc := &services.MockOrunService{
		CatalogStaleFn: func(_ context.Context) (bool, error) { return true, nil },
	}
	if cr := checkCatalogStaleCmd(svc)().(catalogStaleMsg); !cr.stale {
		t.Error("expected stale=true from the probe")
	}
	svc.CatalogStaleFn = func(_ context.Context) (bool, error) { return false, context.DeadlineExceeded }
	if cr := checkCatalogStaleCmd(svc)().(catalogStaleMsg); cr.stale {
		t.Error("a failed probe must report stale=false")
	}
}

// catalogStaleMsg drives the badge; a refresh clears it.
func TestModel_StaleBadge_SetAndClear(t *testing.T) {
	m := NewModel(&services.MockOrunService{})

	next, _ := m.Update(catalogStaleMsg{stale: true})
	m = next.(Model)
	if !m.catalogStale {
		t.Fatal("catalogStaleMsg{true} should set the badge")
	}
	if h := m.renderHeader(); !strings.Contains(h, "stale") {
		t.Errorf("header should show the stale chip:\n%s", h)
	}

	// A refresh (even an unchanged one) means the catalog is now current.
	next, _ = m.Update(catalogRefreshedMsg{refreshed: false})
	m = next.(Model)
	if m.catalogStale {
		t.Fatal("a refresh should clear the stale badge")
	}
	if h := m.renderHeader(); strings.Contains(h, "stale") {
		t.Errorf("header should not show the stale chip when fresh:\n%s", h)
	}
}
