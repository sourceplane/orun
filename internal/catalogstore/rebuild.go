package catalogstore

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/statestore"
)

// rebuild.go implements catalog-store.md §8 — `Resolver.RebuildIndexes`.
// Walks the authoritative `sources/*` tree and rewrites every global
// index file (indexes/sources/*, indexes/catalogs/*,
// indexes/components/*) byte-identically per T-STORE-3.
//
// The rebuild trusts the on-disk source/catalog/manifest documents as the
// canonical truth. Because the Writer surface (writer.go / indexes.go)
// uses the same `catalogmodel.PrettyEncode` encoder and the same
// `mergeComponentGlobalIndex` merge ordering as this file, the bytes
// rebuilt here are identical to what the Writer would have written for
// the same (source, catalog, manifests) trio.
//
// Cost note: the walk issues O(N) `StateStore.List` calls (one per
// source, one per source's catalogs subtree, one per catalog's
// components subtree) plus O(M) `Read` calls where M is the total file
// count. Phase 1 budget is ≤50 ms for 1k revisions on warm SSD; this is
// the same budget that drives the resolver fallback walks.

// RebuildIndexes reconstructs every global index from the source tree.
// Implements catalog-store.md §8.
func (s *store) RebuildIndexes(ctx context.Context) error {
	// 1. Discover every source.
	sources, err := s.listAllSources(ctx)
	if err != nil {
		return fmt.Errorf("RebuildIndexes: %w", err)
	}

	// 2. Per-source: rewrite indexes/sources/<srcKey>.json AND walk
	//    catalogs underneath.
	//
	// Catalogs are accumulated across sources because the global catalog
	// index path is `indexes/catalogs/<catKey>.json` — there is one
	// canonical body per catalogKey. In practice catalogKeys are unique
	// per (sourceKey, catalogHash) so we only ever see one body, but if
	// two sources happen to share a catalogKey (byte-identical catalog)
	// the bodies are equal by construction so a deterministic last-write
	// wins is harmless.
	sortSourcesByKey(sources)
	for i := range sources {
		src := sources[i]
		if err := s.rebuildSourceGlobalIndex(ctx, src); err != nil {
			return err
		}
	}

	catalogs, err := s.collectAllCatalogs(ctx, sources)
	if err != nil {
		return fmt.Errorf("RebuildIndexes: %w", err)
	}
	sortCatalogsByKey(catalogs)
	for i := range catalogs {
		if err := s.rebuildCatalogGlobalIndex(ctx, catalogs[i]); err != nil {
			return err
		}
	}

	// 3. Component global indexes — the subtle case. Walk every
	//    manifest, derive a per-source CGI shard, accumulate by
	//    componentKey, then merge using the same logic as the Writer
	//    (mergeComponentGlobalIndex). Sort the merge order
	//    deterministically: by source CreatedAt ascending, then srcKey
	//    ascending — so the most-recent source's Latest/Main pointers
	//    win the final merge (matching the Writer's "caller is freshest
	//    writer" semantics).
	type sourcedManifest struct {
		src     catalogmodel.SourceSnapshot
		catKey  string
		manifest catalogmodel.ComponentManifest
	}
	var rows []sourcedManifest
	for i := range sources {
		src := sources[i]
		mfs, err := s.listManifestsForSource(ctx, src.SourceSnapshotKey)
		if err != nil {
			return fmt.Errorf("RebuildIndexes: %w", err)
		}
		for j := range mfs {
			rows = append(rows, sourcedManifest{
				src:      src,
				catKey:   mfs[j].Source.CatalogSnapshotKey,
				manifest: mfs[j],
			})
		}
	}
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].src.CreatedAt != rows[j].src.CreatedAt {
			return rows[i].src.CreatedAt < rows[j].src.CreatedAt
		}
		if rows[i].src.SourceSnapshotKey != rows[j].src.SourceSnapshotKey {
			return rows[i].src.SourceSnapshotKey < rows[j].src.SourceSnapshotKey
		}
		if rows[i].catKey != rows[j].catKey {
			return rows[i].catKey < rows[j].catKey
		}
		return rows[i].manifest.Identity.ComponentKey < rows[j].manifest.Identity.ComponentKey
	})

	merged := make(map[string]catalogmodel.ComponentGlobalIndex)
	componentKeys := make([]string, 0)
	for i := range rows {
		shard := buildComponentGlobalIndexShard(rows[i].manifest, rows[i].src, rows[i].catKey)
		key := shard.ComponentKey
		if _, ok := merged[key]; !ok {
			componentKeys = append(componentKeys, key)
		}
		merged[key] = mergeComponentGlobalIndex(merged[key], *shard)
	}
	sort.Strings(componentKeys)
	for _, key := range componentKeys {
		cgi := merged[key]
		if err := s.writeComponentGlobalIndexPlain(ctx, &cgi); err != nil {
			return err
		}
	}

	return nil
}

// listDir converts a directory prefix (which the rest of the code carries in
// the trailing-slash form used for path matching/trimming) into the bare form
// StateStore.List accepts. The real LocalStore validates its argument as a
// logical path and rejects a trailing slash; the in-memory test spy matches on
// raw string prefix and tolerates either. Stripping the slash is correct for
// both: `List("sources")` stats the sources directory and walks it, and every
// returned object path still begins with the trailing-slash form the callers
// TrimPrefix against.
func listDir(prefix string) string {
	return strings.TrimSuffix(prefix, "/")
}

// listAllSources lists every `sources/<srcKey>/source.json` and decodes
// each body. Skips entries that fail to read or decode — a corrupt
// source.json must not blacklist the whole rebuild.
func (s *store) listAllSources(ctx context.Context) ([]catalogmodel.SourceSnapshot, error) {
	const prefix = "sources/"
	// List with the directory prefix (no trailing slash — the StateStore
	// path validator rejects one) but match/trim against the slash form.
	infos, err := s.state.List(ctx, listDir(prefix))
	if err != nil {
		return nil, fmt.Errorf("List %s: %w", prefix, err)
	}
	var out []catalogmodel.SourceSnapshot
	for _, info := range infos {
		rel := strings.TrimPrefix(info.Path, prefix)
		if !strings.HasSuffix(rel, "/source.json") {
			continue
		}
		if strings.Count(rel, "/") != 1 {
			continue
		}
		body, _, rerr := s.state.Read(ctx, info.Path)
		if rerr != nil {
			continue
		}
		var src catalogmodel.SourceSnapshot
		if err := json.Unmarshal(body, &src); err != nil {
			continue
		}
		out = append(out, src)
	}
	return out, nil
}

// collectAllCatalogs lists every `sources/<srcKey>/catalogs/<catKey>/catalog.json`
// across the supplied sources and decodes each body.
func (s *store) collectAllCatalogs(ctx context.Context, sources []catalogmodel.SourceSnapshot) ([]catalogmodel.CatalogSnapshot, error) {
	var out []catalogmodel.CatalogSnapshot
	seen := make(map[string]bool)
	for i := range sources {
		srcKey := sources[i].SourceSnapshotKey
		if err := ValidateSourceKey(srcKey); err != nil {
			continue
		}
		prefix := "sources/" + srcKey + "/catalogs/"
		infos, err := s.state.List(ctx, listDir(prefix))
		if err != nil {
			return nil, fmt.Errorf("List %s: %w", prefix, err)
		}
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
			if seen[cat.CatalogSnapshotKey] {
				continue
			}
			seen[cat.CatalogSnapshotKey] = true
			out = append(out, cat)
		}
	}
	return out, nil
}

// listManifestsForSource lists every
// `sources/<srcKey>/catalogs/*/components/*/manifest.json` under one
// source and decodes each body.
func (s *store) listManifestsForSource(ctx context.Context, srcKey string) ([]catalogmodel.ComponentManifest, error) {
	if err := ValidateSourceKey(srcKey); err != nil {
		return nil, err
	}
	prefix := "sources/" + srcKey + "/catalogs/"
	infos, err := s.state.List(ctx, listDir(prefix))
	if err != nil {
		return nil, fmt.Errorf("List %s: %w", prefix, err)
	}
	var out []catalogmodel.ComponentManifest
	for _, info := range infos {
		rel := strings.TrimPrefix(info.Path, prefix)
		// Match `<catKey>/components/<name>/manifest.json` — exactly
		// 3 slashes, ends in `/manifest.json`.
		if !strings.HasSuffix(rel, "/manifest.json") {
			continue
		}
		parts := strings.Split(rel, "/")
		if len(parts) != 4 || parts[1] != "components" {
			continue
		}
		body, _, rerr := s.state.Read(ctx, info.Path)
		if rerr != nil {
			continue
		}
		var m catalogmodel.ComponentManifest
		if err := json.Unmarshal(body, &m); err != nil {
			continue
		}
		out = append(out, m)
	}
	return out, nil
}

// rebuildSourceGlobalIndex re-encodes the SourceSnapshot body and writes
// it to indexes/sources/<srcKey>.json via plain Write. Plain overwrite
// per spec §8 — no CAS, no retry budget.
func (s *store) rebuildSourceGlobalIndex(ctx context.Context, src catalogmodel.SourceSnapshot) error {
	if err := ValidateSourceKey(src.SourceSnapshotKey); err != nil {
		return fmt.Errorf("RebuildIndexes: %w", err)
	}
	p, err := SourceGlobalIndexPath(src.SourceSnapshotKey)
	if err != nil {
		return fmt.Errorf("RebuildIndexes: source path: %w", err)
	}
	body, err := catalogmodel.PrettyEncode(src)
	if err != nil {
		return fmt.Errorf("RebuildIndexes: encode source %q: %w", src.SourceSnapshotKey, err)
	}
	if _, err := s.state.Write(ctx, p, body, statestore.WriteOptions{}); err != nil {
		return fmt.Errorf("RebuildIndexes: Write %s: %w", p, err)
	}
	return nil
}

// rebuildCatalogGlobalIndex re-encodes the CatalogSnapshot body and
// writes it to indexes/catalogs/<catKey>.json via plain Write.
func (s *store) rebuildCatalogGlobalIndex(ctx context.Context, cat catalogmodel.CatalogSnapshot) error {
	if err := ValidateCatalogKey(cat.CatalogSnapshotKey); err != nil {
		return fmt.Errorf("RebuildIndexes: %w", err)
	}
	p, err := CatalogGlobalIndexPath(cat.CatalogSnapshotKey)
	if err != nil {
		return fmt.Errorf("RebuildIndexes: catalog path: %w", err)
	}
	body, err := catalogmodel.PrettyEncode(cat)
	if err != nil {
		return fmt.Errorf("RebuildIndexes: encode catalog %q: %w", cat.CatalogSnapshotKey, err)
	}
	if _, err := s.state.Write(ctx, p, body, statestore.WriteOptions{}); err != nil {
		return fmt.Errorf("RebuildIndexes: Write %s: %w", p, err)
	}
	return nil
}

// writeComponentGlobalIndexPlain writes one accumulated CGI body to its
// global index path via plain Write. Spec §8 explicitly says rebuild
// uses plain Write (no CAS) — the rebuild is the authoritative writer
// for the duration of the operation.
func (s *store) writeComponentGlobalIndexPlain(ctx context.Context, cgi *catalogmodel.ComponentGlobalIndex) error {
	if err := catalogmodel.ValidateComponentKey(cgi.ComponentKey); err != nil {
		return fmt.Errorf("RebuildIndexes: %w: %v", ErrInvalidPathInput, err)
	}
	p, err := ComponentGlobalIndexPath(cgi.ComponentKey)
	if err != nil {
		return fmt.Errorf("RebuildIndexes: component path %q: %w", cgi.ComponentKey, err)
	}
	body, err := catalogmodel.PrettyEncode(*cgi)
	if err != nil {
		return fmt.Errorf("RebuildIndexes: encode component %q: %w", cgi.ComponentKey, err)
	}
	if _, err := s.state.Write(ctx, p, body, statestore.WriteOptions{}); err != nil {
		return fmt.Errorf("RebuildIndexes: Write %s: %w", p, err)
	}
	return nil
}

// buildComponentGlobalIndexShard derives a single-source CGI shard from
// a resolved manifest under one source. Used both by RebuildIndexes
// (every manifest yields one shard, then shards are merged) and as the
// canonical derivation rule for tests that need to assert "shard ≡
// rebuilt body" without re-implementing the rule.
//
// SourceScope routing:
//   - branch-main / branch-protected → Main pointer (with manifestPath)
//     plus Latest pointer (without manifestPath, per data-model §9.1).
//   - everything else (branch-feature, pr, tag, local-*, ci-event) →
//     Latest pointer plus a Previews entry tagged with the SourceScope.
//
// The merge layer (mergeComponentGlobalIndex) handles cross-source
// resolution: when multiple shards coexist for the same componentKey,
// the freshest writer's Latest/Main wins and Previews are unioned.
func buildComponentGlobalIndexShard(
	m catalogmodel.ComponentManifest,
	src catalogmodel.SourceSnapshot,
	catKey string,
) *catalogmodel.ComponentGlobalIndex {
	loc := catalogmodel.ComponentIndexLocation{
		SourceSnapshotKey:  src.SourceSnapshotKey,
		CatalogSnapshotKey: catKey,
	}
	manifestPath, _ := ComponentManifestPath(src.SourceSnapshotKey, catKey, m.Identity.Name)
	locWithPath := loc
	locWithPath.ManifestPath = manifestPath

	cgi := &catalogmodel.ComponentGlobalIndex{
		APIVersion:   m.APIVersion,
		Kind:         catalogmodel.KindComponentGlobal,
		ComponentKey: m.Identity.ComponentKey,
		Name:         m.Identity.Name,
		Repo:         src.Repo,
		// Canonical empty form is a non-nil slice so every write path —
		// the writer's CreateIfAbsent (raw shard), the writer's CAS merge,
		// and RebuildIndexes' pre-merge — encodes `"previews": []` for a
		// main-scope component. A nil here would encode `null` on the
		// first write and `[]` on a merge, breaking the byte-identical
		// rebuild post-condition (T-STORE-3).
		Previews: []catalogmodel.ComponentIndexPreview{},
	}
	switch src.SourceScope {
	case catalogmodel.SourceScopeBranchMain, catalogmodel.SourceScopeBranchProtected:
		cgi.Main = locWithPath
		cgi.Latest = loc
	default:
		cgi.Latest = loc
		cgi.Previews = []catalogmodel.ComponentIndexPreview{{
			SourceScope:        src.SourceScope,
			SourceSnapshotKey:  src.SourceSnapshotKey,
			CatalogSnapshotKey: catKey,
		}}
	}
	return cgi
}

// sortSourcesByKey is a deterministic ordering helper used for the
// per-source rebuild loop. Stable sort keeps insertion order on ties
// (there should be none — keys are unique by construction).
func sortSourcesByKey(s []catalogmodel.SourceSnapshot) {
	sort.SliceStable(s, func(i, j int) bool { return s[i].SourceSnapshotKey < s[j].SourceSnapshotKey })
}

func sortCatalogsByKey(c []catalogmodel.CatalogSnapshot) {
	sort.SliceStable(c, func(i, j int) bool { return c[i].CatalogSnapshotKey < c[j].CatalogSnapshotKey })
}
