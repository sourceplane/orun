package main

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/sourceplane/orun/internal/objcatalog"
)

// TestCatalogRefresh_WritesObjectModel verifies the §2 repoint: `orun catalog
// refresh` also writes the object-model catalog (loadable at catalogs/current,
// with the change-detection impact index) and reports it in data.objectModel.
func TestCatalogRefresh_WritesObjectModel(t *testing.T) {
	dir := withTempIntentRoot(t)
	seedGitCatalogWorkspace(t, dir)

	resetCatalogFlags(t)
	catalogJSONFlag = true

	out := captureStdout(t, func() error { return runCatalogRefresh(nil) })
	var env catalogEnvelope
	env.Data = &catalogRefreshData{}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("refresh envelope: %v\n%s", err, out)
	}
	d := env.Data.(*catalogRefreshData)

	if d.ObjectModel == nil {
		t.Fatalf("data.objectModel missing; envelope=%s", out)
	}
	if d.ObjectModel.CatalogID == "" || d.ObjectModel.SourceID == "" {
		t.Errorf("objectModel ids empty: %+v", d.ObjectModel)
	}
	if d.ObjectModel.Components != d.Components {
		t.Errorf("objectModel.components=%d, want %d (catalogstore count)", d.ObjectModel.Components, d.Components)
	}

	// The catalog is actually loadable from the object store, with an impact index.
	store, refs, _, err := openObjectModel()
	if err != nil {
		t.Fatalf("openObjectModel: %v", err)
	}
	view, err := objcatalog.New(store, refs).Load(context.Background(), "catalogs/current")
	if err != nil {
		t.Fatalf("load catalogs/current: %v", err)
	}
	if string(view.ObjectID) != d.ObjectModel.CatalogID {
		t.Errorf("loaded catalog id %s != reported %s", view.ObjectID, d.ObjectModel.CatalogID)
	}
	if view.Ownership == nil {
		t.Errorf("refreshed catalog has no impact index (ownership nil)")
	}
	if len(view.Components) == 0 {
		t.Errorf("refreshed catalog has no components")
	}
}
