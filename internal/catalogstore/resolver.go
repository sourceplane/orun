package catalogstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/statestore"
)

// resolver.go implements the C4 PR-3 Resolver surface declared in
// store.go. It is the read-side mirror of writer.go / refs.go /
// indexes.go: every method either consults a typed pointer (refs/*,
// indexes/components/*) or falls back to the spec §4 directory walk and
// returns the on-disk persisted form decoded from the canonical
// PrettyEncode body.

// ResolveCurrentSource is shorthand for ResolveSource with the
// "current" ref name and is the entry point catalog-store.md §4 row 1
// names directly. Falls back to the most recent
// `sources/*/source.json` by `createdAt` when refs/sources/current.json
// is absent.
func (s *store) ResolveCurrentSource(ctx context.Context) (catalogmodel.SourceSnapshot, error) {
	return s.ResolveSource(ctx, RefSelector{Kind: catalogmodel.RefNameCurrent})
}

// ResolveSource resolves a SourceSnapshot from a typed selector. When
// `Snapshot` is set on the selector it bypasses the ref layer and reads
// the snapshot doc directly. Otherwise it follows
// refs/sources/<name>.json (or branches/<branch>.json /
// prs/<pr>.json), decodes the SourceRef body, and reads the referenced
// snapshot.
//
// Empty selector kind defaults to "current". For the current selector
// the §4 fallback walk runs when the typed ref is absent.
func (s *store) ResolveSource(ctx context.Context, sel RefSelector) (catalogmodel.SourceSnapshot, error) {
	if sel.Snapshot != "" {
		return s.readSourceByKey(ctx, sel.Snapshot)
	}
	refPath, err := sourceRefPathForSelector(sel)
	if err != nil {
		return catalogmodel.SourceSnapshot{}, fmt.Errorf("ResolveSource: %w", err)
	}
	body, _, rerr := s.state.Read(ctx, refPath)
	if rerr != nil {
		if !errors.Is(rerr, statestore.ErrNotFound) {
			return catalogmodel.SourceSnapshot{}, fmt.Errorf("ResolveSource: Read %s: %w", refPath, rerr)
		}
		// Spec §4 row 1 fallback — only for the "current" selector.
		// Other ref names (latest/main/branches/prs) surface a wrapped
		// statestore.ErrNotFound so callers see the typed pointer miss.
		if isCurrentSelector(sel) {
			src, ferr := s.fallbackMostRecentSource(ctx)
			if ferr == nil {
				return src, nil
			}
			return catalogmodel.SourceSnapshot{}, ferr
		}
		return catalogmodel.SourceSnapshot{}, errNotFoundChain(fmt.Errorf("ResolveSource: source ref %q absent", refPath))
	}
	var ref catalogmodel.SourceRef
	if err := json.Unmarshal(body, &ref); err != nil {
		return catalogmodel.SourceSnapshot{}, fmt.Errorf("ResolveSource: decode ref %s: %w", refPath, err)
	}
	return s.readSourceByKey(ctx, ref.SourceSnapshotKey)
}

// ResolveCatalog resolves a CatalogSnapshot from a typed selector via
// refs/catalogs/<name>.json (or branches/<branch> / prs/<pr>). For the
// "current" selector the §4 row 2 fallback runs when the ref is
// absent: it resolves the current source first, then walks
// `sources/<srcKey>/catalogs/*/catalog.json` and picks the most recent
// by `createdAt`.
//
// Returns ErrCatalogNotFound (chained with statestore.ErrNotFound) when
// neither the ref nor the fallback resolves a catalog.
func (s *store) ResolveCatalog(ctx context.Context, sel RefSelector) (catalogmodel.CatalogSnapshot, error) {
	refPath, err := catalogRefPathForSelector(sel)
	if err != nil {
		return catalogmodel.CatalogSnapshot{}, fmt.Errorf("ResolveCatalog: %w", err)
	}
	body, _, rerr := s.state.Read(ctx, refPath)
	if rerr != nil {
		if !errors.Is(rerr, statestore.ErrNotFound) {
			return catalogmodel.CatalogSnapshot{}, fmt.Errorf("ResolveCatalog: Read %s: %w", refPath, rerr)
		}
		if isCurrentSelector(sel) {
			// Resolve the source side first so the walk is scoped to
			// the right `sources/<srcKey>/catalogs/*` subtree.
			src, serr := s.ResolveCurrentSource(ctx)
			if serr != nil {
				// Source side also missing — surface the catalog-side
				// sentinel so callers don't have to introspect.
				return catalogmodel.CatalogSnapshot{}, errNotFoundChain(ErrCatalogNotFound)
			}
			cat, ferr := s.fallbackMostRecentCatalog(ctx, src.SourceSnapshotKey)
			if ferr == nil {
				return cat, nil
			}
			return catalogmodel.CatalogSnapshot{}, errNotFoundChain(ErrCatalogNotFound)
		}
		return catalogmodel.CatalogSnapshot{}, errNotFoundChain(ErrCatalogNotFound)
	}
	var ref catalogmodel.CatalogRef
	if err := json.Unmarshal(body, &ref); err != nil {
		return catalogmodel.CatalogSnapshot{}, fmt.Errorf("ResolveCatalog: decode ref %s: %w", refPath, err)
	}
	return s.readCatalogByKeys(ctx, ref.SourceSnapshotKey, ref.CatalogSnapshotKey)
}

// ResolveComponent resolves a single ComponentManifest under the
// catalog selected by `sel`. The lookup is a single Read against
// `sources/<srcKey>/catalogs/<catKey>/components/<name>/manifest.json`
// after ResolveCatalog has resolved the (srcKey, catKey) pair.
//
// Returns ErrComponentNotFound (chained with statestore.ErrNotFound) on
// manifest absence; ErrCatalogNotFound when the catalog itself does not
// resolve.
func (s *store) ResolveComponent(ctx context.Context, sel RefSelector, name string) (catalogmodel.ComponentManifest, error) {
	if err := ValidateComponentName(name); err != nil {
		return catalogmodel.ComponentManifest{}, fmt.Errorf("ResolveComponent: %w", err)
	}
	cat, err := s.ResolveCatalog(ctx, sel)
	if err != nil {
		return catalogmodel.ComponentManifest{}, err
	}
	p, err := ComponentManifestPath(cat.SourceSnapshotKey, cat.CatalogSnapshotKey, name)
	if err != nil {
		return catalogmodel.ComponentManifest{}, fmt.Errorf("ResolveComponent: %w", err)
	}
	body, _, rerr := s.state.Read(ctx, p)
	if rerr != nil {
		if errors.Is(rerr, statestore.ErrNotFound) {
			return catalogmodel.ComponentManifest{}, errNotFoundChain(ErrComponentNotFound)
		}
		return catalogmodel.ComponentManifest{}, fmt.Errorf("ResolveComponent: Read %s: %w", p, rerr)
	}
	var m catalogmodel.ComponentManifest
	if err := json.Unmarshal(body, &m); err != nil {
		return catalogmodel.ComponentManifest{}, fmt.Errorf("ResolveComponent: decode %s: %w", p, err)
	}
	return m, nil
}

// ResolveComponentLatest reads the cross-source component global index
// at `indexes/components/<sanitizedComponentKey>.json` and returns its
// Latest / Main / Previews locations as a ComponentLatest. The index
// itself is the canonical denormalized view; ResolveComponentLatest does
// not chase ManifestPath into a manifest body — that's the caller's
// responsibility (and they typically use ResolveComponent next).
//
// Returns ErrComponentNotFound (chained with statestore.ErrNotFound) when
// the index is absent. The §4 row 3 directory-walk fallback is reserved
// for ResolveComponent (single-catalog scope); cross-source latest
// without the global index is a Phase 3 / C8 RebuildIndexes concern.
func (s *store) ResolveComponentLatest(ctx context.Context, componentKey string) (ComponentLatest, error) {
	p, err := ComponentGlobalIndexPath(componentKey)
	if err != nil {
		return ComponentLatest{}, fmt.Errorf("ResolveComponentLatest: %w", err)
	}
	body, _, rerr := s.state.Read(ctx, p)
	if rerr != nil {
		if errors.Is(rerr, statestore.ErrNotFound) {
			return ComponentLatest{}, errNotFoundChain(ErrComponentNotFound)
		}
		return ComponentLatest{}, fmt.Errorf("ResolveComponentLatest: Read %s: %w", p, rerr)
	}
	var idx catalogmodel.ComponentGlobalIndex
	if err := json.Unmarshal(body, &idx); err != nil {
		return ComponentLatest{}, fmt.Errorf("ResolveComponentLatest: decode %s: %w", p, err)
	}
	return ComponentLatest{
		ComponentKey: idx.ComponentKey,
		Latest:       idx.Latest,
		Main:         idx.Main,
		Previews:     idx.Previews,
	}, nil
}

// ----- internal helpers -----------------------------------------------

// isCurrentSelector reports whether the selector targets the "current"
// source/catalog ref (either explicit Kind="current" or the zero-value
// selector which defaults to current).
func isCurrentSelector(sel RefSelector) bool {
	return sel.Kind == "" || sel.Kind == catalogmodel.RefNameCurrent
}

// sourceRefPathForSelector maps a RefSelector to the on-disk source ref
// path. Returns ErrInvalidPathInput on missing required fields per kind.
func sourceRefPathForSelector(sel RefSelector) (string, error) {
	switch sel.Kind {
	case "", catalogmodel.RefNameCurrent:
		return SourceRefPath(catalogmodel.RefNameCurrent)
	case catalogmodel.RefNameMain, catalogmodel.RefNameLatest:
		return SourceRefPath(sel.Kind)
	case "branch":
		if sel.Branch == "" {
			return "", fmt.Errorf("%w: branch selector requires Branch", ErrInvalidPathInput)
		}
		b := catalogmodel.SanitizeBranch(sel.Branch)
		if b == "" {
			return "", fmt.Errorf("%w: branch %q sanitized to empty", ErrInvalidPathInput, sel.Branch)
		}
		return SourceBranchRefPath(b)
	case "pr":
		if sel.PR == "" {
			return "", fmt.Errorf("%w: pr selector requires PR", ErrInvalidPathInput)
		}
		return SourcePRRefPath(sel.PR)
	default:
		return "", fmt.Errorf("%w: unknown selector kind %q", ErrInvalidPathInput, sel.Kind)
	}
}

// catalogRefPathForSelector mirrors sourceRefPathForSelector against
// the refs/catalogs/* tree.
func catalogRefPathForSelector(sel RefSelector) (string, error) {
	switch sel.Kind {
	case "", catalogmodel.RefNameCurrent:
		return CatalogRefPath(catalogmodel.RefNameCurrent)
	case catalogmodel.RefNameMain, catalogmodel.RefNameLatest:
		return CatalogRefPath(sel.Kind)
	case "branch":
		if sel.Branch == "" {
			return "", fmt.Errorf("%w: branch selector requires Branch", ErrInvalidPathInput)
		}
		b := catalogmodel.SanitizeBranch(sel.Branch)
		if b == "" {
			return "", fmt.Errorf("%w: branch %q sanitized to empty", ErrInvalidPathInput, sel.Branch)
		}
		return CatalogBranchRefPath(b)
	case "pr":
		if sel.PR == "" {
			return "", fmt.Errorf("%w: pr selector requires PR", ErrInvalidPathInput)
		}
		return CatalogPRRefPath(sel.PR)
	default:
		return "", fmt.Errorf("%w: unknown selector kind %q", ErrInvalidPathInput, sel.Kind)
	}
}

// readSourceByKey reads sources/<srcKey>/source.json and decodes it
// into a SourceSnapshot. Wraps statestore.ErrNotFound unchanged so
// callers can errors.Is against the underlying sentinel.
func (s *store) readSourceByKey(ctx context.Context, srcKey string) (catalogmodel.SourceSnapshot, error) {
	p, err := SourceDocPath(srcKey)
	if err != nil {
		return catalogmodel.SourceSnapshot{}, fmt.Errorf("ResolveSource: %w", err)
	}
	body, _, rerr := s.state.Read(ctx, p)
	if rerr != nil {
		return catalogmodel.SourceSnapshot{}, fmt.Errorf("ResolveSource: Read %s: %w", p, rerr)
	}
	var src catalogmodel.SourceSnapshot
	if err := json.Unmarshal(body, &src); err != nil {
		return catalogmodel.SourceSnapshot{}, fmt.Errorf("ResolveSource: decode %s: %w", p, err)
	}
	return src, nil
}

// readCatalogByKeys reads sources/<srcKey>/catalogs/<catKey>/catalog.json
// and decodes it. NotFound is mapped to ErrCatalogNotFound (chained with
// statestore.ErrNotFound) so the surface error is uniform across the
// ref-hit and fallback paths.
func (s *store) readCatalogByKeys(ctx context.Context, srcKey, catKey string) (catalogmodel.CatalogSnapshot, error) {
	p, err := CatalogDocPath(srcKey, catKey)
	if err != nil {
		return catalogmodel.CatalogSnapshot{}, fmt.Errorf("ResolveCatalog: %w", err)
	}
	body, _, rerr := s.state.Read(ctx, p)
	if rerr != nil {
		if errors.Is(rerr, statestore.ErrNotFound) {
			return catalogmodel.CatalogSnapshot{}, errNotFoundChain(ErrCatalogNotFound)
		}
		return catalogmodel.CatalogSnapshot{}, fmt.Errorf("ResolveCatalog: Read %s: %w", p, rerr)
	}
	var cat catalogmodel.CatalogSnapshot
	if err := json.Unmarshal(body, &cat); err != nil {
		return catalogmodel.CatalogSnapshot{}, fmt.Errorf("ResolveCatalog: decode %s: %w", p, err)
	}
	return cat, nil
}

// fallbackMostRecentSource implements catalog-store.md §4 row 1: list
// every `sources/*/source.json` and pick the most recent by createdAt.
// Subdirectory traversal is filtered in-memory so the List backend stays
// generic — Phase 1 scale ≤ 1k snapshots, ≤ 50 ms warm-SSD budget.
//
// String compare on `createdAt` is correct here because the field is a
// canonical RFC3339 UTC timestamp ("2026-05-31T00:00:00Z" form) — lexical
// order matches chronological order.
func (s *store) fallbackMostRecentSource(ctx context.Context) (catalogmodel.SourceSnapshot, error) {
	const prefix = "sources/"
	infos, err := s.state.List(ctx, prefix)
	if err != nil {
		return catalogmodel.SourceSnapshot{}, fmt.Errorf("ResolveSource: List %s: %w", prefix, err)
	}
	var best catalogmodel.SourceSnapshot
	found := false
	for _, info := range infos {
		rel := strings.TrimPrefix(info.Path, prefix)
		// Match exactly `<srcKey>/source.json` — one slash, ends in
		// "/source.json". Skips nested `catalogs/...` paths.
		if !strings.HasSuffix(rel, "/source.json") {
			continue
		}
		if strings.Count(rel, "/") != 1 {
			continue
		}
		body, _, rerr := s.state.Read(ctx, info.Path)
		if rerr != nil {
			// Skip transient read failures — fallback is best-effort
			// over whatever List enumerates. A read failure on one
			// snapshot must not blacklist the whole walk.
			continue
		}
		var src catalogmodel.SourceSnapshot
		if err := json.Unmarshal(body, &src); err != nil {
			continue
		}
		if !found || src.CreatedAt > best.CreatedAt {
			best = src
			found = true
		}
	}
	if !found {
		return catalogmodel.SourceSnapshot{}, errNotFoundChain(fmt.Errorf("ResolveSource: no source snapshots under %s", prefix))
	}
	return best, nil
}

// fallbackMostRecentCatalog implements catalog-store.md §4 row 2: list
// every `sources/<srcKey>/catalogs/*/catalog.json` for the resolved
// source and pick the most recent by createdAt.
func (s *store) fallbackMostRecentCatalog(ctx context.Context, srcKey string) (catalogmodel.CatalogSnapshot, error) {
	if err := ValidateSourceKey(srcKey); err != nil {
		return catalogmodel.CatalogSnapshot{}, fmt.Errorf("ResolveCatalog: %w", err)
	}
	prefix := "sources/" + srcKey + "/catalogs/"
	infos, err := s.state.List(ctx, prefix)
	if err != nil {
		return catalogmodel.CatalogSnapshot{}, fmt.Errorf("ResolveCatalog: List %s: %w", prefix, err)
	}
	var best catalogmodel.CatalogSnapshot
	found := false
	for _, info := range infos {
		rel := strings.TrimPrefix(info.Path, prefix)
		if !strings.HasSuffix(rel, "/catalog.json") {
			continue
		}
		if strings.Count(rel, "/") != 1 {
			continue
		}
		body, _, rerr := s.state.Read(ctx, info.Path)
		if rerr != nil {
			continue
		}
		var cat catalogmodel.CatalogSnapshot
		if err := json.Unmarshal(body, &cat); err != nil {
			continue
		}
		if !found || cat.CreatedAt > best.CreatedAt {
			best = cat
			found = true
		}
	}
	if !found {
		return catalogmodel.CatalogSnapshot{}, errNotFoundChain(ErrCatalogNotFound)
	}
	return best, nil
}
