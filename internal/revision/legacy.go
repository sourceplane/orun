package revision

import (
	"context"
	"fmt"
	"strings"

	"github.com/sourceplane/orun/internal/statestore"
)

// Legacy `.orun/plans/` mirror paths.
//
// The Phase 1 statestore is rooted at `.orun`, so the logical paths used by
// the compat mirror are simply `plans/<checksum>.json` and
// `plans/latest.json` — they translate to `.orun/plans/...` on disk
// (compatibility-and-migration.md §2). The path strings are constructed
// here, NOT in internal/statestore/paths.go, because the legacy directory
// is not part of the new layout and the M3 PR-A constraint forbids growing
// the statestore surface for revision-package-specific paths (see
// internal/revision/version.go::stateStoreVersionPath for the same rule
// applied to version.json).
//
// All caller-controlled segments are validated through statestore.ValidateComponent
// so the same alphabet rules apply as everywhere else in the store.

const (
	legacyPlansDir       = "plans"
	legacyPlanLatestStem = "latest"
)

// legacyPlanPath returns "plans/<checksum>.json" after validating checksum
// against the path-component alphabet. Returns an error wrapping
// statestore.ErrInvalid on validation failure.
func legacyPlanPath(checksum string) (string, error) {
	if err := statestore.ValidateComponent(checksum); err != nil {
		return "", err
	}
	return legacyPlansDir + "/" + checksum + ".json", nil
}

// legacyLatestPlanPath returns "plans/latest.json".
func legacyLatestPlanPath() (string, error) {
	// The stem is a compile-time constant inside the allowed alphabet —
	// validate it once anyway so any future edit that breaks the rule
	// fails immediately rather than producing an unreadable path.
	if err := statestore.ValidateComponent(legacyPlanLatestStem); err != nil {
		return "", err
	}
	return legacyPlansDir + "/" + legacyPlanLatestStem + ".json", nil
}

// normalizeLegacyChecksum strips the optional "sha256:" prefix and any
// surrounding whitespace, then validates that the remainder is non-empty
// lowercase hex. The legacy filename convention (data-model.md §10 / compat
// §2) is the bare lowercase hex digest — the "sha256:" prefix only appears
// in metadata fields like PlanRevision.PlanHash.
//
// Returns an error wrapping statestore.ErrInvalid when the input is empty,
// has fewer than 8 hex chars (matching PlanShortHash's minimum so callers
// don't see one validator pass and another reject the same string), or
// contains non-hex bytes.
func normalizeLegacyChecksum(planHash string) (string, error) {
	h := strings.TrimSpace(planHash)
	h = strings.TrimPrefix(h, "sha256:")
	h = strings.ToLower(h)
	if len(h) < planShortHashLen {
		return "", fmt.Errorf("%w: planHash %q has fewer than %d hex chars",
			statestore.ErrInvalid, planHash, planShortHashLen)
	}
	for i := 0; i < len(h); i++ {
		c := h[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return "", fmt.Errorf("%w: planHash %q contains non-hex byte %q",
				statestore.ErrInvalid, planHash, c)
		}
	}
	return h, nil
}

// WriteLegacyNamedPlan writes a `.orun/plans/<name>.json` alias byte-identical
// to the canonical plan.json, supporting the preserved `orun plan --name <n>`
// workflow (compatibility-and-migration.md §1). The path component is
// validated through statestore.ValidateComponent so the same alphabet rules
// apply as the rest of the store. Errors wrap statestore.ErrInvalid for bad
// names; transport errors propagate verbatim from the store.
//
// This helper is the in-revision-package seam for the legacy named-alias
// write — keeping it here (alongside legacyPlanPath / legacyLatestPlanPath)
// means cmd/orun never opens os.WriteFile against `.orun/` for compat work.
func WriteLegacyNamedPlan(ctx context.Context, store statestore.StateStore, name string, planBytes []byte) error {
	if store == nil {
		return fmt.Errorf("%w: WriteLegacyNamedPlan store is nil", statestore.ErrInvalid)
	}
	if err := statestore.ValidateComponent(name); err != nil {
		return fmt.Errorf("%w: invalid named-plan name %q: %v", statestore.ErrInvalid, name, err)
	}
	if name == legacyPlanLatestStem {
		// "latest" collides with legacyLatestPlanPath() — refuse to clobber
		// the canonical alias.
		return fmt.Errorf("%w: named-plan %q is reserved", statestore.ErrInvalid, name)
	}
	path := legacyPlansDir + "/" + name + ".json"
	if _, err := store.Write(ctx, path, planBytes, statestore.WriteOptions{}); err != nil {
		return fmt.Errorf("write legacy named plan %s: %w", path, err)
	}
	return nil
}

// isHexLower reports whether s is non-empty and entirely composed of
// lowercase hex digits. Used by the resolver to identify branch-5 input
// (legacy plan-hash arg) without committing to a specific length —
// historical legacy files used both 8-char and 64-char digests.
func isHexLower(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}
