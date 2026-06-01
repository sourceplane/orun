package catalogsync

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// TestNoopSyncer_ReportsNotConfigured proves the Phase 2 default syncer
// accepts nothing, returns the documented warning, and never errors.
func TestNoopSyncer_ReportsNotConfigured(t *testing.T) {
	res, err := NoopSyncer{}.PushCatalogSnapshot(context.Background(), SyncPayload{}, PushOptions{})
	if err != nil {
		t.Fatalf("NoopSyncer returned error: %v", err)
	}
	if res.Accepted {
		t.Errorf("NoopSyncer accepted the push; want Accepted=false")
	}
	if res.RemoteSourceKey != "" || res.RemoteCatalogKey != "" {
		t.Errorf("NoopSyncer minted remote keys: %+v", res)
	}
	if len(res.Warnings) != 1 || res.Warnings[0] != NoopSyncWarning {
		t.Errorf("warnings = %v, want exactly [%q]", res.Warnings, NoopSyncWarning)
	}
	if NoopSyncWarning != "remote sync not configured (Phase 3)" {
		t.Errorf("NoopSyncWarning drifted: %q", NoopSyncWarning)
	}
}

// TestNoopSyncer_IgnoresPayloadAndOptions proves the no-op path is inert: a
// fully-populated payload and options change nothing about the result.
func TestNoopSyncer_IgnoresPayloadAndOptions(t *testing.T) {
	payload := newCatalogModelPayload()
	opts := PushOptions{
		DryRun:        true,
		AllowDirty:    true,
		Reason:        "test",
		ExtraMetadata: map[string]string{"k": "v"},
	}
	res, err := NoopSyncer{}.PushCatalogSnapshot(context.Background(), payload, opts)
	if err != nil {
		t.Fatalf("NoopSyncer returned error: %v", err)
	}
	if res.Accepted || len(res.Warnings) != 1 || res.Warnings[0] != NoopSyncWarning {
		t.Errorf("populated payload/options changed result: %+v", res)
	}
}

// TestSyncPayload_ComposedFromCatalogModel proves SyncPayload is assembled
// entirely from internal/catalogmodel value types — no runner/CLI/store types
// are required to build a payload. If a future refactor introduced a
// non-catalogmodel field, this file would fail to compile.
func TestSyncPayload_ComposedFromCatalogModel(t *testing.T) {
	var (
		_ catalogmodel.SourceSnapshot          = SyncPayload{}.Source
		_ catalogmodel.CatalogSnapshot         = SyncPayload{}.Catalog
		_ []catalogmodel.ComponentManifest     = SyncPayload{}.Manifests
		_ []catalogmodel.CatalogGraph          = SyncPayload{}.Graphs
		_ catalogmodel.SourceRef               = SyncPayload{}.SourceRef
		_ catalogmodel.CatalogRef              = SyncPayload{}.CatalogRef
		_ []catalogmodel.ComponentHistoryEvent = SyncPayload{}.HistoryEvents
	)

	p := newCatalogModelPayload()
	if p.Source.SourceSnapshotKey != "src-test" {
		t.Errorf("payload Source not wired from catalogmodel: %+v", p.Source)
	}
	if p.Catalog.CatalogSnapshotKey != "cat-test" {
		t.Errorf("payload Catalog not wired from catalogmodel: %+v", p.Catalog)
	}
}

// newCatalogModelPayload builds a SyncPayload using only catalogmodel types,
// doubling as the composition proof for TestSyncPayload_ComposedFromCatalogModel.
func newCatalogModelPayload() SyncPayload {
	return SyncPayload{
		Source:        catalogmodel.SourceSnapshot{SourceSnapshotKey: "src-test"},
		Catalog:       catalogmodel.CatalogSnapshot{CatalogSnapshotKey: "cat-test"},
		Manifests:     []catalogmodel.ComponentManifest{{}},
		Graphs:        []catalogmodel.CatalogGraph{{}},
		SourceRef:     catalogmodel.SourceRef{},
		CatalogRef:    catalogmodel.CatalogRef{},
		HistoryEvents: []catalogmodel.ComponentHistoryEvent{},
	}
}
