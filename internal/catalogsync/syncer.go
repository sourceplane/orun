// Package catalogsync defines the Phase 2 catalog sync seam: a local-only
// contract for pushing a resolved catalog snapshot to a future remote store.
//
// Phase 2 ships no networking. NoopSyncer is the default implementation; it
// accepts nothing and reports that remote sync is not configured. The package
// exists so the local model is already shaped for the Phase 3 remote driver
// without forcing a breaking redesign, and so the CLI depends only on the
// Syncer interface rather than on any concrete (eventually remote) driver.
//
// Purity constraints (enforced by import_boundary_test.go): this package must
// not import net/http, internal/runner, or cmd/orun. SyncPayload is composed
// from internal/catalogmodel types only — no translation layer — so a future
// remote driver can stream the local file shapes without rebuilding them. A
// translation layer here would be a signal that the local model is the wrong
// shape; its absence keeps the local/remote contract honest.
package catalogsync

import (
	"context"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// NoopSyncWarning is the warning a non-configured syncer reports. It is a
// stable string so the CLI and tests can assert on it verbatim.
const NoopSyncWarning = "remote sync not configured (Phase 3)"

// PushOptions carries local control metadata for a push. Every field is
// advisory in Phase 2 — NoopSyncer ignores all of them — but the shape is
// fixed now so a Phase 3 remote driver can drive dry-run, dirty-preview, and
// audit behavior off the same options the CLI already populates.
type PushOptions struct {
	// DryRun asks a remote driver to validate without committing. Ignored by
	// NoopSyncer.
	DryRun bool

	// AllowDirty permits pushing a snapshot resolved from a dirty worktree.
	// A remote driver gates dirty previews behind this plus repo policy.
	AllowDirty bool

	// Reason is free-form text included in a remote audit log.
	Reason string

	// ExtraMetadata is opaque key/value metadata forwarded to a remote
	// driver's audit record. Never part of catalog identity.
	ExtraMetadata map[string]string
}

// PushResult reports the outcome of a push. NoopSyncer returns Accepted:false
// with the NoopSyncWarning and empty remote keys.
type PushResult struct {
	// Accepted reports whether the remote store durably accepted the
	// snapshot. Always false for NoopSyncer.
	Accepted bool

	// RemoteSourceKey / RemoteCatalogKey echo the keys the remote store
	// assigned. Empty when not accepted.
	RemoteSourceKey  string
	RemoteCatalogKey string

	// Warnings carries human-readable notes (e.g. "remote sync not
	// configured (Phase 3)"). The CLI surfaces these in text and JSON output.
	Warnings []string
}

// SyncPayload is the union of everything needed to reconstruct a catalog
// snapshot remotely. It mirrors the local on-disk shapes — built from
// internal/catalogmodel types only — so a future remote driver streams the
// local files without any translation step.
type SyncPayload struct {
	Source        catalogmodel.SourceSnapshot
	Catalog       catalogmodel.CatalogSnapshot
	Manifests     []catalogmodel.ComponentManifest
	Graphs        []catalogmodel.CatalogGraph
	SourceRef     catalogmodel.SourceRef
	CatalogRef    catalogmodel.CatalogRef
	HistoryEvents []catalogmodel.ComponentHistoryEvent
}

// Syncer is the seam a Phase 3 remote driver implements. Phase 2 wires
// NoopSyncer as the only implementation; the CLI depends on this interface so
// the driver can be swapped without touching command code.
type Syncer interface {
	// PushCatalogSnapshot pushes a resolved, locally-persisted catalog
	// snapshot to the configured remote store. It must not mutate the
	// payload. Implementations are expected to be idempotent on
	// (source, catalog) keys.
	PushCatalogSnapshot(ctx context.Context, payload SyncPayload, opts PushOptions) (PushResult, error)
}

// NoopSyncer is the default no-network syncer. It performs no I/O, accepts
// nothing, and reports that remote sync is not configured. It ignores the
// payload and options entirely.
type NoopSyncer struct{}

// PushCatalogSnapshot reports the not-configured warning and returns no
// error. The local refresh has already succeeded by the time the CLI calls
// this; a non-configured remote is an expected Phase 2 state, not a failure.
func (NoopSyncer) PushCatalogSnapshot(_ context.Context, _ SyncPayload, _ PushOptions) (PushResult, error) {
	return PushResult{
		Accepted: false,
		Warnings: []string{NoopSyncWarning},
	}, nil
}

// Compile-time assertion that NoopSyncer satisfies Syncer.
var _ Syncer = NoopSyncer{}
