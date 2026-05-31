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

// listrefs.go is the C5 PR-1 ref-enumeration seam. The Resolver interface
// resolves a catalog/source *per selector* but cannot *list* refs;
// `orun catalog refs` (and later `orun catalog list`) need to walk every
// ref under refs/sources/* and refs/catalogs/* and join them by ref name.
//
// ListRefs is a pure read helper over statestore.List — it issues no raw
// os / path / filepath calls (the package's no-raw-FS lint must stay
// green), building every prefix via the pathJoin helper that the rest of
// paths.go uses.

// RefListing is one joined ref entry: a ref name (current / main / latest
// / branches/<b> / prs/<n>) plus the source + catalog pointers that ref
// resolves to and the authoritative flag carried on the ref bodies. Either
// side may be absent (a refs/sources entry with no matching refs/catalogs
// entry, or vice versa) — the corresponding *Key field is then empty.
type RefListing struct {
	// Name is the ref identity relative to refs/{sources,catalogs}/ with
	// the .json suffix stripped: "current", "main", "latest",
	// "branches/<branch>", or "prs/<pr>".
	Name string
	// SourceScope is the scope label from whichever ref body is present
	// (catalog preferred, then source).
	SourceScope string
	// SourceSnapshotKey is the key the source ref resolves to. Empty if no
	// refs/sources entry exists for this name.
	SourceSnapshotKey string
	// CatalogSnapshotKey is the key the catalog ref resolves to. Empty if
	// no refs/catalogs entry exists for this name.
	CatalogSnapshotKey string
	// Authoritative is true when the ref points at a catalog-of-record.
	// Taken from the catalog ref body when present, else the source ref.
	Authoritative bool
}

// ListRefs enumerates every ref under refs/sources/* and refs/catalogs/*,
// joins them by ref name, and returns the merged listing sorted by Name
// for deterministic output. Pure read: no writes, no side effects.
//
// Missing ref directories (a fresh .orun root with no refresh yet) are
// not an error — statestore.List returns an empty slice for an absent
// prefix, so ListRefs returns an empty slice too.
func ListRefs(ctx context.Context, state statestore.StateStore) ([]RefListing, error) {
	byName := map[string]*RefListing{}

	get := func(name string) *RefListing {
		if r, ok := byName[name]; ok {
			return r
		}
		r := &RefListing{Name: name}
		byName[name] = r
		return r
	}

	// Source side.
	srcPrefix := pathJoin("refs", "sources")
	srcInfos, err := state.List(ctx, srcPrefix)
	if err != nil {
		return nil, fmt.Errorf("catalogstore: ListRefs: list %s: %w", srcPrefix, err)
	}
	for _, info := range srcInfos {
		name, ok := refNameFromPath(info.Path, srcPrefix)
		if !ok {
			continue
		}
		body, _, rerr := state.Read(ctx, info.Path)
		if rerr != nil {
			return nil, fmt.Errorf("catalogstore: ListRefs: read %s: %w", info.Path, rerr)
		}
		var sref catalogmodel.SourceRef
		if err := json.Unmarshal(body, &sref); err != nil {
			return nil, fmt.Errorf("catalogstore: ListRefs: decode %s: %w", info.Path, err)
		}
		r := get(name)
		r.SourceSnapshotKey = sref.SourceSnapshotKey
		if r.SourceScope == "" {
			r.SourceScope = sref.SourceScope
		}
		r.Authoritative = r.Authoritative || sref.Authoritative
	}

	// Catalog side (preferred for scope/authoritative when present).
	catPrefix := pathJoin("refs", "catalogs")
	catInfos, err := state.List(ctx, catPrefix)
	if err != nil {
		return nil, fmt.Errorf("catalogstore: ListRefs: list %s: %w", catPrefix, err)
	}
	for _, info := range catInfos {
		name, ok := refNameFromPath(info.Path, catPrefix)
		if !ok {
			continue
		}
		body, _, rerr := state.Read(ctx, info.Path)
		if rerr != nil {
			return nil, fmt.Errorf("catalogstore: ListRefs: read %s: %w", info.Path, rerr)
		}
		var cref catalogmodel.CatalogRef
		if err := json.Unmarshal(body, &cref); err != nil {
			return nil, fmt.Errorf("catalogstore: ListRefs: decode %s: %w", info.Path, err)
		}
		r := get(name)
		r.CatalogSnapshotKey = cref.CatalogSnapshotKey
		if cref.SourceSnapshotKey != "" {
			r.SourceSnapshotKey = cref.SourceSnapshotKey
		}
		r.SourceScope = cref.SourceScope
		r.Authoritative = cref.Authoritative
	}

	out := make([]RefListing, 0, len(byName))
	for _, r := range byName {
		out = append(out, *r)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// refNameFromPath turns a full ref object path into its ref name relative
// to prefix, with the trailing ".json" stripped. Returns ok=false for any
// path that is not a .json file under prefix (defensive — statestore.List
// only yields regular files, but a stray non-json entry is skipped rather
// than mis-parsed).
//
//	refNameFromPath("refs/sources/current.json", "refs/sources")            → "current", true
//	refNameFromPath("refs/sources/branches/feat-x.json", "refs/sources")    → "branches/feat-x", true
//	refNameFromPath("refs/sources/prs/139.json", "refs/sources")            → "prs/139", true
func refNameFromPath(p, prefix string) (string, bool) {
	rest := strings.TrimPrefix(p, prefix+"/")
	if rest == p {
		// prefix was not actually a prefix of p.
		return "", false
	}
	if !strings.HasSuffix(rest, ".json") {
		return "", false
	}
	name := strings.TrimSuffix(rest, ".json")
	if name == "" {
		return "", false
	}
	return name, true
}
