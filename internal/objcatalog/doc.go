// Package objcatalog is the read view over the object-model component catalog.
//
// It mirrors internal/objread: a Reader rooted at the object-model root reads a
// CatalogSnapshot tree (catalog.json + components/ + graph/ + the optional
// impact/ subtree) back into a presentation-neutral CatalogView. The cockpit and
// the change-detection engine consume this view instead of re-resolving the
// catalog or reading intent.yaml directly (specs/orun-catalog-state, CS2).
//
// Reads are tolerant of older catalogs: a catalog written before the impact/
// subtree existed loads with Ownership == nil and no error.
package objcatalog
