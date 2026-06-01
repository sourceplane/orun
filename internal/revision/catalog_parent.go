package revision

import (
	"context"
	"fmt"

	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// catalog_parent.go mirrors the canonical revision body under the
// catalog-parent layout introduced in Phase 2 (design.md §7,
// implementation-plan.md C6):
//
//	sources/<srcKey>/catalogs/<catKey>/revisions/<revKey>/
//	  ├─ trigger.json
//	  ├─ revision.json
//	  ├─ plan.json
//	  └─ manifest.json
//
// The mirror is strictly additive: WriteRevision/WriteManifest always emit
// the Phase 1 global layout (revisions/<revKey>/...) first, then — only when
// a (source, catalog) snapshot pair was resolved — copy the *exact same
// bytes* under the catalog parent. Reusing the already-marshalled bytes (not
// re-encoding) guarantees the two layouts are byte-identical and that the
// planHash the caller computed remains valid against both plan.json copies.
//
// Path-segment validation happens inside the catalogstore helpers; an
// invalid srcKey/catKey/revKey therefore surfaces as a wrapped
// statestore.ErrInvalid rather than a panic.

// writeCatalogParentRevision mirrors trigger.json, revision.json, and
// plan.json under the catalog parent. It is invoked by WriteRevision after
// the Phase 1 layout and compat mirror have been written, and only when
// CatalogParent.Active() is true.
//
// The writes follow the same body-before-ref discipline as the global
// layout: bodies are written here; the catalog-parent index/ref bookkeeping
// (indexes/catalogs/..., refs/catalogs/...) is owned by the catalog refresh
// pipeline (C5) and the run integration (C7), not by the plan writer.
func writeCatalogParentRevision(
	ctx context.Context,
	store statestore.StateStore,
	parent CatalogParentRef,
	rev PlanRevision,
	trig triggerctx.TriggerOccurrence,
	planBytes []byte,
) error {
	triggerPath, err := catalogstore.CatalogRevisionTriggerPath(parent.SourceKey, parent.CatalogKey, rev.RevisionKey)
	if err != nil {
		return fmt.Errorf("catalog-parent mirror trigger path: %w", err)
	}
	revPath, err := catalogstore.CatalogRevisionDocPath(parent.SourceKey, parent.CatalogKey, rev.RevisionKey)
	if err != nil {
		return fmt.Errorf("catalog-parent mirror revision path: %w", err)
	}
	planPath, err := catalogstore.CatalogRevisionPlanPath(parent.SourceKey, parent.CatalogKey, rev.RevisionKey)
	if err != nil {
		return fmt.Errorf("catalog-parent mirror plan path: %w", err)
	}

	// trigger.json — re-marshal from the same TriggerOccurrence the global
	// layout persisted; marshalCanonicalJSON is deterministic so the bytes
	// match revisions/<revKey>/trigger.json exactly.
	if _, err := store.Write(ctx, triggerPath, marshalCanonicalJSON(trig), statestore.WriteOptions{}); err != nil {
		return fmt.Errorf("write catalog-parent trigger.json: %w", err)
	}
	// revision.json — same deterministic re-marshal of the persisted record.
	if _, err := store.Write(ctx, revPath, marshalCanonicalJSON(rev), statestore.WriteOptions{}); err != nil {
		return fmt.Errorf("write catalog-parent revision.json: %w", err)
	}
	// plan.json — verbatim caller bytes (the canonical plan the planHash was
	// computed over). Forwarded unchanged so the parent copy is byte-identical.
	if _, err := store.Write(ctx, planPath, planBytes, statestore.WriteOptions{}); err != nil {
		return fmt.Errorf("write catalog-parent plan.json: %w", err)
	}
	return nil
}

// writeCatalogParentManifest mirrors manifest.json under the catalog parent.
// Invoked by WriteManifest after the Phase 1 manifest is written, and only
// when CatalogParent.Active() is true. The manifest is a derived projection,
// so an unconditional overwrite preserves the consistent-snapshot guarantee
// documented on WriteManifest.
func writeCatalogParentManifest(
	ctx context.Context,
	store statestore.StateStore,
	parent CatalogParentRef,
	manifest RevisionManifest,
	revKey string,
) error {
	manifestPath, err := catalogstore.CatalogRevisionManifestPath(parent.SourceKey, parent.CatalogKey, revKey)
	if err != nil {
		return fmt.Errorf("catalog-parent mirror manifest path: %w", err)
	}
	if _, err := store.Write(ctx, manifestPath, marshalCanonicalJSON(manifest), statestore.WriteOptions{}); err != nil {
		return fmt.Errorf("write catalog-parent manifest.json: %w", err)
	}
	return nil
}
