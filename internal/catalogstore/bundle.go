package catalogstore

import (
	"fmt"
	"sort"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// bundle.go is the C5 PR-1 persistence-bundle assembly seam. It is the
// single pure helper that turns a resolved catalog view (the
// catalogresolve.CatalogView shapes — SourceSnapshot + *CatalogSnapshot +
// []*ComponentManifest + []*CatalogGraph) into the four writer-input
// bundles the catalogstore.Writer consumes:
//
//   - CatalogGraphs       (step B.2 — per-kind graph fan-out)
//   - CatalogLocalIndexes (step B.4 — catalog-local index bodies)
//   - GlobalIndexUpdate   (step C   — source/catalog/component global indexes)
//   - RefUpdate           (step D   — source/catalog ref pointers)
//
// Home rationale: this logic must return catalogstore types
// (CatalogGraphs / CatalogLocalIndexes / GlobalIndexUpdate / RefUpdate),
// and catalogresolve MUST NOT import catalogstore (doc.go architecture
// rule — catalogstore depends on catalogresolve, never the reverse).
// Therefore the assembly lives here, in catalogstore, reusing the
// canonical single-source derivation rule already shipped by
// buildComponentGlobalIndexShard (rebuild.go).
//
// Purity: AssembleBundle performs no I/O. Given identical inputs it
// produces byte-identical bundles (every slice/map is built in a
// deterministic order), so the refresh path stays reproducible and
// golden-testable.

// BundleInputs is the full input set AssembleBundle needs. It mirrors the
// fields of catalogresolve.CatalogView (Snapshot / Manifests / Graphs)
// plus the source-side SourceSnapshot and the ref-scope facts the
// resolver cannot invent (Branch / PR scope labels and the ref
// UpdatedAt timestamp). The CLI fills Branch / PR / UpdatedAt from the
// sourcectx.WorkspaceState + clock at refresh time.
type BundleInputs struct {
	// Source is the persisted SourceSnapshot the catalog was resolved
	// against. Required.
	Source catalogmodel.SourceSnapshot

	// Snapshot is the resolved *CatalogSnapshot (CatalogView.Snapshot).
	// Required.
	Snapshot *catalogmodel.CatalogSnapshot

	// Manifests are the resolved component manifests
	// (CatalogView.ResolvedCatalog.Manifests). May be empty.
	Manifests []*catalogmodel.ComponentManifest

	// Graphs are the per-kind catalog graphs (CatalogView.Graphs), in the
	// resolver's canonical order [dependencies, systems, apis, resources,
	// owners]. May be empty; nil graphs map to nil CatalogGraphs slots.
	Graphs []*catalogmodel.CatalogGraph

	// Branch, when non-empty, requests a refs/{sources,catalogs}/branches/<branch>
	// ref write. The CLI supplies the (unsanitized) branch name from the
	// WorkspaceState for feature/protected branch scopes; WriteRefs
	// sanitizes it. Leave empty to skip branch refs (the canonical main
	// ref is written separately when the ref is authoritative).
	Branch string

	// PR, when non-empty, requests a refs/{sources,catalogs}/prs/<pr> ref
	// write. The CLI supplies the decimal PR number from the
	// WorkspaceState for pr scope. Leave empty to skip PR refs.
	PR string

	// UpdatedAt is the RFC 3339 / Z timestamp stamped on the SourceRef /
	// CatalogRef pointer bodies. Caller-supplied so ref writes stay
	// deterministic (pinned in golden tests). Required.
	UpdatedAt string
}

// CatalogBundle is the assembled writer-input set. The CLI hands these to
// the catalogstore.Writer in order: WriteSourceSnapshot(Source) →
// WriteCatalogSnapshot(Source, Catalog, Manifests, Graphs, LocalIndexes)
// → WriteGlobalIndexes(GlobalIndexes) → WriteRefs(Refs).
type CatalogBundle struct {
	Source        catalogmodel.SourceSnapshot
	Catalog       catalogmodel.CatalogSnapshot
	Manifests     []catalogmodel.ComponentManifest
	Graphs        CatalogGraphs
	LocalIndexes  CatalogLocalIndexes
	GlobalIndexes GlobalIndexUpdate
	Refs          RefUpdate
}

// AssembleBundle derives the full CatalogBundle from a resolved catalog
// view plus source-side facts. Pure and deterministic.
//
// Bundle derivation rules:
//
//   - Graphs: the input []*CatalogGraph is mapped positionally into the
//     named CatalogGraphs slots in the resolver's canonical order
//     [dependencies, systems, apis, resources, owners]. A short or nil
//     slice leaves the trailing slots nil (the Writer skips nil graphs).
//
//   - LocalIndexes: one catalog-local ComponentExecutionIndex is built
//     per manifest, keyed by component name, with an empty executions[]
//     (history events are populated by C7, not the refresh path — see
//     task-0036 Non-Goals). The owner/system/domain/type axes are left
//     empty in this PR; their catalog-local body schemas are
//     under-specified by data-model.md §9 (only §9.2's component index is
//     fully specified). See ai/proposals/task-0036-spec-update.md.
//
//   - GlobalIndexes: Source + Catalog denormalized pointers plus one
//     per-component ComponentGlobalIndex shard built via the canonical
//     buildComponentGlobalIndexShard rule (shared with RebuildIndexes).
//     Components are sorted by ComponentKey for a deterministic write
//     trace; the Writer re-sorts but the input order is pinned here too.
//
//   - Refs: a SourceRef + CatalogRef pointer pair derived from the
//     Source + Snapshot, with Branch / PR scope labels copied from the
//     inputs. Authoritative/Preview are carried from the snapshot.
func AssembleBundle(in BundleInputs) (CatalogBundle, error) {
	if in.Snapshot == nil {
		return CatalogBundle{}, fmt.Errorf("catalogstore: AssembleBundle: Snapshot is required")
	}
	if in.UpdatedAt == "" {
		return CatalogBundle{}, fmt.Errorf("catalogstore: AssembleBundle: UpdatedAt is required")
	}

	manifests := derefManifests(in.Manifests)
	catKey := in.Snapshot.CatalogSnapshotKey

	graphs := mapGraphs(in.Graphs)

	localIndexes, err := buildLocalIndexes(in.Source, *in.Snapshot, manifests)
	if err != nil {
		return CatalogBundle{}, err
	}

	globalIndexes := buildGlobalIndexUpdate(in.Source, *in.Snapshot, manifests, catKey)

	refs := buildRefUpdate(in.Source, *in.Snapshot, in.Branch, in.PR, in.UpdatedAt)

	return CatalogBundle{
		Source:        in.Source,
		Catalog:       *in.Snapshot,
		Manifests:     manifests,
		Graphs:        graphs,
		LocalIndexes:  localIndexes,
		GlobalIndexes: globalIndexes,
		Refs:          refs,
	}, nil
}

// derefManifests copies the []*ComponentManifest view into a []ComponentManifest
// value slice (the WriteCatalogSnapshot signature), skipping nil pointers.
// Order is preserved (the resolver already sorts by componentKey).
func derefManifests(in []*catalogmodel.ComponentManifest) []catalogmodel.ComponentManifest {
	if len(in) == 0 {
		return nil
	}
	out := make([]catalogmodel.ComponentManifest, 0, len(in))
	for _, m := range in {
		if m == nil {
			continue
		}
		out = append(out, *m)
	}
	return out
}

// mapGraphs maps the resolver's canonical-ordered []*CatalogGraph slice
// into the named CatalogGraphs slots. The resolver always emits exactly
// five graphs in [dependencies, systems, apis, resources, owners] order
// (catalogresolve/graph.go buildGraphs); a short slice leaves trailing
// slots nil and the Writer skips nil graphs silently.
func mapGraphs(g []*catalogmodel.CatalogGraph) CatalogGraphs {
	var out CatalogGraphs
	if len(g) > 0 {
		out.Dependencies = g[0]
	}
	if len(g) > 1 {
		out.Systems = g[1]
	}
	if len(g) > 2 {
		out.APIs = g[2]
	}
	if len(g) > 3 {
		out.Resources = g[3]
	}
	if len(g) > 4 {
		out.Owners = g[4]
	}
	return out
}

// buildLocalIndexes derives the catalog-local index bundle (step B.4).
// PR-1 populates only the component axis: one ComponentExecutionIndex per
// manifest, keyed by component name, with an empty executions[] (history
// events are C7). owner/system/domain/type bodies are intentionally left
// empty — their catalog-local schemas are under-specified by data-model.md
// §9 and are deferred to a later C5 PR (see proposal). Returns an error if
// a component name is invalid for the on-disk path.
func buildLocalIndexes(
	src catalogmodel.SourceSnapshot,
	cat catalogmodel.CatalogSnapshot,
	manifests []catalogmodel.ComponentManifest,
) (CatalogLocalIndexes, error) {
	if len(manifests) == 0 {
		return CatalogLocalIndexes{}, nil
	}
	components := make(map[string]any, len(manifests))
	for _, m := range manifests {
		name := m.Identity.Name
		if err := ValidateComponentName(name); err != nil {
			return CatalogLocalIndexes{}, fmt.Errorf("catalogstore: AssembleBundle: component %q: %w", name, err)
		}
		components[name] = catalogmodel.ComponentExecutionIndex{
			APIVersion:         catalogmodel.APIVersionV1Alpha1,
			Kind:               catalogmodel.KindComponentExecIndex,
			ComponentKey:       m.Identity.ComponentKey,
			SourceSnapshotKey:  src.SourceSnapshotKey,
			CatalogSnapshotKey: cat.CatalogSnapshotKey,
			Executions:         []catalogmodel.ComponentExecutionRow{},
		}
	}
	return CatalogLocalIndexes{Components: components}, nil
}

// buildGlobalIndexUpdate derives the global-index bundle (step C):
// denormalized Source + Catalog pointers plus one ComponentGlobalIndex
// shard per manifest built via the canonical buildComponentGlobalIndexShard
// rule (shared with RebuildIndexes so refresh and rebuild agree byte-for-
// byte). Component shards are sorted by ComponentKey for determinism.
func buildGlobalIndexUpdate(
	src catalogmodel.SourceSnapshot,
	cat catalogmodel.CatalogSnapshot,
	manifests []catalogmodel.ComponentManifest,
	catKey string,
) GlobalIndexUpdate {
	srcCopy := src
	catCopy := cat
	out := GlobalIndexUpdate{
		Source:  &srcCopy,
		Catalog: &catCopy,
	}
	if len(manifests) == 0 {
		return out
	}
	comps := make([]*catalogmodel.ComponentGlobalIndex, 0, len(manifests))
	for i := range manifests {
		comps = append(comps, buildComponentGlobalIndexShard(manifests[i], src, catKey))
	}
	sort.SliceStable(comps, func(a, b int) bool {
		return comps[a].ComponentKey < comps[b].ComponentKey
	})
	out.Components = comps
	return out
}

// buildRefUpdate derives the SourceRef + CatalogRef pointer pair (step D)
// from the source + catalog snapshots. Authoritative / Preview are carried
// from the snapshot (the resolver computed them per data-model.md §2). The
// pointer Name is set to the canonical "current" label — WriteRefs writes
// the same body to current / main / latest / branch / pr paths, so Name is
// the primary ref identity. Branch / PR scope labels are copied onto the
// RefUpdate envelope (not the ref body, which carries no branch field).
func buildRefUpdate(
	src catalogmodel.SourceSnapshot,
	cat catalogmodel.CatalogSnapshot,
	branch, pr, updatedAt string,
) RefUpdate {
	sourceRef := &catalogmodel.SourceRef{
		APIVersion:        catalogmodel.APIVersionV1Alpha1,
		Kind:              catalogmodel.KindSourceRef,
		Name:              catalogmodel.RefNameCurrent,
		SourceScope:       src.SourceScope,
		SourceSnapshotKey: src.SourceSnapshotKey,
		HeadRevision:      src.HeadRevision,
		TreeHash:          src.TreeHash,
		WorkingTree:       src.WorkingTree,
		Authoritative:     cat.Authoritative,
		UpdatedAt:         updatedAt,
	}
	catalogRef := &catalogmodel.CatalogRef{
		APIVersion:         catalogmodel.APIVersionV1Alpha1,
		Kind:               catalogmodel.KindCatalogRef,
		Name:               catalogmodel.RefNameCurrent,
		SourceScope:        cat.SourceScope,
		SourceSnapshotKey:  cat.SourceSnapshotKey,
		CatalogSnapshotKey: cat.CatalogSnapshotKey,
		CatalogHash:        cat.CatalogHash,
		HeadRevision:       cat.HeadRevision,
		TreeHash:           cat.TreeHash,
		Authoritative:      cat.Authoritative,
		Preview:            cat.Preview,
		UpdatedAt:          updatedAt,
	}
	return RefUpdate{
		Source:  sourceRef,
		Catalog: catalogRef,
		Branch:  branch,
		PR:      pr,
	}
}
