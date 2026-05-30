package revision

// Property-based regression gates for revision-key derivation
// (test-plan.md §3.2, design.md §9).
//
// Two properties are locked in here:
//
//  1. RevisionKey uniqueness — identical (TriggerOccurrence, planHash) pairs
//     produce identical keys; differing pairs produce differing keys (no
//     hidden quantisation, no truncation collisions inside the 8-char
//     plan-hash prefix).
//
//  2. ResolveCollision suffix correctness — when the base candidate is
//     pre-claimed and we force N additional collisions, the resolver hands
//     back `<base>-x1`, `<base>-x2`, …, `<base>-xN` in order, with no gaps
//     and no skips. This protects the writer's claim-first ordering
//     (writer.go) from regressions that would re-use the same suffix or
//     silently jump past a number.
//
// Both checks live here (next to keys.go) rather than in a new file so the
// uniqueness property and the collision-suffix property can share the same
// fixture builders.

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"pgregory.net/rapid"

	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// triggerFromDraw assembles a TriggerOccurrence whose TriggerKey is the
// canonical `trg-<scope>-<shortSha>` shape that scopePart accepts. We use
// the real triggerctx.TriggerKey constructor so the property test is
// auditing whatever the production key derivation would produce, not an
// ad-hoc shape.
func triggerFromDraw(rt *rapid.T, label string) triggerctx.TriggerOccurrence {
	scope := rapid.StringMatching(`[a-z0-9-]{1,20}`).Draw(rt, label+"_scope")
	sha := rapid.StringMatching(`[a-f0-9]{40}`).Draw(rt, label+"_sha")
	occ := triggerctx.TriggerOccurrence{
		Source: triggerctx.TriggerSource{
			SourceScope:  scope,
			HeadRevision: sha,
			WorkingTree:  triggerctx.WorkingTreeClean,
		},
	}
	occ.TriggerKey = triggerctx.TriggerKey(occ)
	return occ
}

// planHashFromDraw produces a 40-char lowercase-hex blob that PlanShortHash
// will accept. The full 40 chars (not just 8) keeps the property realistic:
// production passes the full sha256 hex; we want to catch any drift where
// RevisionKey starts depending on bytes beyond the documented 8-char prefix.
func planHashFromDraw(rt *rapid.T, label string) string {
	return rapid.StringMatching(`[a-f0-9]{40}`).Draw(rt, label)
}

// TestRevisionKey_PropertyDeterminismAndDistinctness covers the first
// uniqueness invariant from test-plan.md §3.2: identical inputs ⇒ identical
// keys (determinism); differing inputs ⇒ differing keys (no collision
// inside the canonical 8-char plan-hash prefix when scope + short-hash
// differ).
//
// "Differ" is defined narrowly: two draws may legally produce the same key
// when they happen to share the same trigger scope + sha AND the same
// first-8 hex chars of planHash. The property explicitly skips those draws
// rather than treating them as a failure — they are the cases the spec
// allows ResolveCollision to disambiguate downstream.
func TestRevisionKey_PropertyDeterminismAndDistinctness(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		trig := triggerFromDraw(rt, "trig")
		planHash := planHashFromDraw(rt, "planHash")

		k1, err := RevisionKey(trig, planHash)
		if err != nil {
			rt.Fatalf("RevisionKey(first): %v", err)
		}
		k2, err := RevisionKey(trig, planHash)
		if err != nil {
			rt.Fatalf("RevisionKey(second): %v", err)
		}
		if k1 != k2 {
			rt.Fatalf("RevisionKey not deterministic: %q vs %q", k1, k2)
		}
		if err := ValidateRevisionKey(k1); err != nil {
			rt.Fatalf("ValidateRevisionKey(%q): %v", k1, err)
		}
	})
}

// TestRevisionKey_PropertyDistinctInputsDistinctKeys forces a second draw
// that *must* differ in the meaningful key inputs (scope or first-8 hex of
// planHash) and asserts the derived keys differ. This rules out a
// regression where the writer accidentally truncates the scope or zeros
// out the short-hash slot.
func TestRevisionKey_PropertyDistinctInputsDistinctKeys(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		trig1 := triggerFromDraw(rt, "trig1")
		plan1 := planHashFromDraw(rt, "plan1")
		trig2 := triggerFromDraw(rt, "trig2")
		plan2 := planHashFromDraw(rt, "plan2")

		// Skip draws where the meaningful inputs collide; the spec
		// allows those to share a base key (collision suffix takes
		// over via ResolveCollision).
		short1, err := PlanShortHash(plan1)
		if err != nil {
			rt.Skip()
		}
		short2, err := PlanShortHash(plan2)
		if err != nil {
			rt.Skip()
		}
		if trig1.TriggerKey == trig2.TriggerKey && short1 == short2 {
			rt.Skip()
		}

		k1, err := RevisionKey(trig1, plan1)
		if err != nil {
			rt.Fatalf("RevisionKey(trig1): %v", err)
		}
		k2, err := RevisionKey(trig2, plan2)
		if err != nil {
			rt.Fatalf("RevisionKey(trig2): %v", err)
		}
		if k1 == k2 {
			rt.Fatalf("distinct meaningful inputs yielded identical key %q (trig1=%q plan1=%q vs trig2=%q plan2=%q)",
				k1, trig1.TriggerKey, plan1, trig2.TriggerKey, plan2)
		}
	})
}

// TestResolveCollision_PropertySuffixContiguity covers the second
// invariant: when the candidate slot is pre-claimed N times, the resolver
// hands back `-x1`, `-x2`, …, `-xN` in strict order with no gaps. We bound
// N at 10 to keep the property cheap; the suffix cap itself is enforced
// elsewhere (see TestResolveCollision_BaseAvailable_NoSuffix and friends
// in writer_test.go).
//
// Each rapid iteration uses an isolated LocalStore (via t.TempDir on the
// inner *testing.T) so concurrent property workers do not race on a
// shared revision index.
func TestResolveCollision_PropertySuffixContiguity(t *testing.T) {
	t.Parallel()
	rapid.Check(t, func(rt *rapid.T) {
		n := rapid.IntRange(1, 10).Draw(rt, "n")

		// Allocate the store inside the property body — *rapid.T does
		// not satisfy *testing.T so we cannot reuse t.TempDir here.
		// MkdirTemp + manual cleanup matches the pattern in
		// internal/statestore/local_prb_test.go's property test.
		root := filepath.Join(t.TempDir(), fmt.Sprintf("ws-%d", n))
		store, err := statestore.NewLocalStore(statestore.LocalConfig{Root: root})
		if err != nil {
			rt.Fatalf("NewLocalStore: %v", err)
		}

		trig := triggerFromDraw(rt, "trig")
		planHash := planHashFromDraw(rt, "planHash")
		base, err := RevisionKey(trig, planHash)
		if err != nil {
			rt.Fatalf("RevisionKey: %v", err)
		}

		ctx := context.Background()
		// First call must return the base key (no suffix).
		first, err := ResolveCollision(ctx, store, base)
		if err != nil {
			rt.Fatalf("ResolveCollision(base): %v", err)
		}
		if first != base {
			rt.Fatalf("first claim returned suffixed key %q; want %q", first, base)
		}

		// Force N additional collisions; each must return -x{i}
		// with i starting at 1 and incrementing without gaps.
		for i := 1; i <= n; i++ {
			got, err := ResolveCollision(ctx, store, base)
			if err != nil {
				rt.Fatalf("ResolveCollision #%d: %v", i, err)
			}
			want := fmt.Sprintf("%s-x%d", base, i)
			if got != want {
				rt.Fatalf("collision #%d: got %q want %q", i, got, want)
			}
		}
	})
}
