package revision

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/statestore"
)

// LegacyPlanEntry is a single legacy `.orun/plans/<checksum>.json` file
// observed by ScanLegacyPlanHashes.
//
// Path is the logical path in the statestore (relative to the .orun root,
// forward-slash, no leading slash) — i.e. "plans/<checksum>.json". The
// caller can use it for diagnostics / dry-run output but should NOT
// concatenate further state paths on top of it.
//
// Checksum is the bare lowercase-hex stem of the filename (the "<checksum>"
// segment). It has been validated through normalizeLegacyChecksum so the
// caller can hand it to legacyPlanPath / canonicalPlanHash without
// re-validating.
//
// The ".../latest.json" alias is filtered out; only content-addressed
// entries are returned.
type LegacyPlanEntry struct {
	Path     string
	Checksum string
}

// ScanLegacyPlanHashes walks "plans/" under store and returns one entry per
// legacy plan file whose stem is a valid hex checksum. The "latest.json"
// alias is filtered. Output is sorted by Checksum so callers see a stable
// dry-run / migration order.
//
// ErrNotFound on the prefix listing is treated as "no legacy plans" — the
// function returns a nil slice and nil error in that case so callers don't
// need to special-case a fresh workspace.
//
// All other errors propagate verbatim (wrapping the underlying statestore
// sentinel through fmt.Errorf — no new sentinels are introduced here).
//
// Compatibility-and-migration.md §5.1 step 1 is the consumer of this
// helper: `orun state migrate` walks the returned entries to synthesize
// `system.migrated` revisions for every legacy plan checksum present in
// the workspace.
func ScanLegacyPlanHashes(ctx context.Context, store statestore.StateStore) ([]LegacyPlanEntry, error) {
	if store == nil {
		return nil, fmt.Errorf("%w: ScanLegacyPlanHashes store is nil", statestore.ErrInvalid)
	}
	// statestore.List rejects trailing slashes; pass the bare directory
	// name and reconstruct the prefix locally for relative-path
	// trimming.
	prefix := legacyPlansDir
	infos, err := store.List(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("list legacy plans %q: %w", prefix, err)
	}
	prefixWithSlash := prefix + "/"
	out := make([]LegacyPlanEntry, 0, len(infos))
	for _, info := range infos {
		// Filter to immediate children with the .json suffix. List
		// is unspecified-order and may include nested entries on
		// drivers that surface them; gate on a single path segment
		// after the prefix.
		rest := strings.TrimPrefix(info.Path, prefixWithSlash)
		if rest == "" || strings.Contains(rest, "/") {
			continue
		}
		if !strings.HasSuffix(rest, ".json") {
			continue
		}
		stem := strings.TrimSuffix(rest, ".json")
		if stem == legacyPlanLatestStem {
			continue
		}
		// normalizeLegacyChecksum enforces the lowercase-hex
		// alphabet and the planShortHashLen floor — non-checksum
		// filenames (e.g. user-named aliases via WriteLegacyNamedPlan)
		// are skipped silently. The migrate command only cares about
		// content-addressed plan files because those are the ones
		// whose hash is recoverable from the filename.
		checksum, err := normalizeLegacyChecksum(stem)
		if err != nil {
			continue
		}
		out = append(out, LegacyPlanEntry{
			Path:     info.Path,
			Checksum: checksum,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Checksum < out[j].Checksum })
	return out, nil
}
