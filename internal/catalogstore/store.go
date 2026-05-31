package catalogstore

import (
	"context"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/statestore"
)

// Writer is the persistence-side contract for catalog objects. PR-1
// shipped steps A and B (source body + catalog body fan-out); PR-2
// fills in steps C and D (refs, global indexes, history events).
// All five Writer methods are wired as of PR-2.
type Writer interface {
	// WriteSourceSnapshot persists a SourceSnapshot at SourceDocPath.
	// Idempotent on byte-identical re-write; returns ErrSourceMismatch
	// (wrapping statestore.ErrExists) on body-divergent re-write.
	WriteSourceSnapshot(ctx context.Context, src catalogmodel.SourceSnapshot) error

	// WriteCatalogSnapshot persists, in order:
	//   B.1 each ComponentManifest
	//   B.2 each catalog graph (dependencies, systems, apis, resources, owners)
	//   B.3 the CatalogSnapshot doc
	//   B.4 each catalog-local index
	//
	// Pre-flight: WriteCatalogSnapshot rejects mismatched
	// sourceSnapshotKey / catalogSnapshotKey linkage between src, cat,
	// and manifests with ErrInputsInconsistent BEFORE issuing any write.
	WriteCatalogSnapshot(
		ctx context.Context,
		src catalogmodel.SourceSnapshot,
		cat catalogmodel.CatalogSnapshot,
		manifests []catalogmodel.ComponentManifest,
		graphs CatalogGraphs,
		localIndexes CatalogLocalIndexes,
	) error

	// WriteRefs is implemented by C4 PR-2 in refs.go.
	WriteRefs(ctx context.Context, refs RefUpdate) error

	// WriteGlobalIndexes is implemented by C4 PR-2 in indexes.go.
	WriteGlobalIndexes(ctx context.Context, updates GlobalIndexUpdate) error

	// AppendComponentEvent is implemented by C4 PR-2 in events.go.
	AppendComponentEvent(ctx context.Context, ev catalogmodel.ComponentHistoryEvent) error
}

// Resolver is the read-side contract. All methods are implemented by
// C4 PR-3.
type Resolver interface {
	ResolveCurrentSource(ctx context.Context) (catalogmodel.SourceSnapshot, error)
	ResolveSource(ctx context.Context, selector RefSelector) (catalogmodel.SourceSnapshot, error)
	ResolveCatalog(ctx context.Context, selector RefSelector) (catalogmodel.CatalogSnapshot, error)
	ResolveComponent(ctx context.Context, sel RefSelector, name string) (catalogmodel.ComponentManifest, error)
	ResolveComponentLatest(ctx context.Context, key string) (ComponentLatest, error)

	// RebuildIndexes reconstructs every global index file
	// (indexes/sources/*, indexes/catalogs/*, indexes/components/*) from
	// the authoritative source tree under sources/*. The rebuild is
	// idempotent and byte-identical for the same input tree per
	// catalog-store.md §8 (T-STORE-3). Used by `orun catalog validate
	// --rebuild-indexes` (C8).
	RebuildIndexes(ctx context.Context) error
}

// Store is the union surface implemented by New().
type Store interface {
	Writer
	Resolver
}

// RefSelector picks a catalog/source pointer for the Resolver methods.
// `Snapshot` is reserved for the C8 diff command and unused by PR-3's
// initial resolver bodies.
type RefSelector struct {
	Kind     string // "current" | "main" | "latest" | "branch" | "pr"
	Branch   string
	PR       string
	Snapshot string
}

// CatalogGraphs is the per-kind graph bundle the writer expects in
// step B.2. Each field is optional; nil graphs are skipped silently
// (the resolver always emits all five so this is a forward-compat lever
// only).
type CatalogGraphs struct {
	Dependencies *catalogmodel.CatalogGraph
	Systems      *catalogmodel.CatalogGraph
	APIs         *catalogmodel.CatalogGraph
	Resources    *catalogmodel.CatalogGraph
	Owners       *catalogmodel.CatalogGraph
}

// CatalogLocalIndexes is the catalog-local index bundle for step B.4.
// Each map's key is the per-axis identifier (component name, owner,
// system, domain, type); the value is the encoded index body the writer
// hands to statestore.Write.
//
// Bodies are passed as pre-built `any` so the writer can canonical-encode
// them via catalogmodel.PrettyEncode just like every other catalog-side
// payload. Callers MUST NOT pre-encode the bodies themselves.
type CatalogLocalIndexes struct {
	Components map[string]any
	Owners     map[string]any
	Systems    map[string]any
	Domains    map[string]any
	Types      map[string]any
}

// RefUpdate is the C4 PR-2 ref-write input. Declared here so the
// interface signature is final. Body fields are intentionally minimal
// in PR-1 — PR-2 finalizes the typed shape against catalogmodel.SourceRef
// and catalogmodel.CatalogRef.
type RefUpdate struct {
	Source  *catalogmodel.SourceRef
	Catalog *catalogmodel.CatalogRef
	// Branch / PR scopes are filled in by PR-2.
	Branch string
	PR     string
}

// GlobalIndexUpdate is the C4 PR-2 global-index input. Like RefUpdate,
// declared here so the Writer signature is final.
type GlobalIndexUpdate struct {
	Source     *catalogmodel.SourceSnapshot
	Catalog    *catalogmodel.CatalogSnapshot
	Components []*catalogmodel.ComponentGlobalIndex
}

// ComponentLatest is the response shape for Resolver.ResolveComponentLatest.
// Declared here so the interface signature is final; bodies populated by
// PR-3.
type ComponentLatest struct {
	ComponentKey string
	Latest       catalogmodel.ComponentIndexLocation
	Main         catalogmodel.ComponentIndexLocation
	Previews     []catalogmodel.ComponentIndexPreview
}

// New returns the in-progress Store. Only WriteSourceSnapshot and
// WriteCatalogSnapshot are wired in this PR; all other methods return
// ErrNotImplemented per catalog-store.md PR-1 stub policy.
func New(state statestore.StateStore) Store {
	return &store{state: state}
}

// store is the internal Store implementation. Held behind the public
// Store interface so PR-2 / PR-3 can fill in method bodies without
// touching the public surface.
type store struct {
	state statestore.StateStore
}

// Compile-time interface assertions — make accidental signature drift
// in PR-2 / PR-3 a build break.
var _ Writer = (*store)(nil)
var _ Resolver = (*store)(nil)
var _ Store = (*store)(nil)

// ----- Resolver method bodies live in resolver.go (PR-3) --------------

// ----- Writer stubs (PR-2) --------------------------------------------
//
// PR-2 moved the WriteRefs / WriteGlobalIndexes / AppendComponentEvent
// bodies into refs.go / indexes.go / events.go. No stubs remain.
