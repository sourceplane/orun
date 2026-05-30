package revision

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// revisionKeyPattern is the validation regex from data-model.md §3.1:
//
//	^rev-[a-z0-9-]+-p[a-f0-9]{8}(-x\d+)?$
//
// The base form is rev-<scope>-p<planHash8>; the optional -xN suffix is
// appended by ResolveCollision when the index entry for the base key is
// already taken (see writer.go for the claim-first ordering).
var revisionKeyPattern = regexp.MustCompile(`^rev-[a-z0-9-]+-p[a-f0-9]{8}(-x\d+)?$`)

// CollisionSuffixCap bounds the -xN counter explored by ResolveCollision.
// 99 is comfortable headroom: in a steady state a key collision implies two
// independent triggers produced the same (TriggerKey, planHash8) pair within
// the same wall-clock window, which we expect to be vanishingly rare.
// Hitting the cap is treated as a hard failure rather than a silent retry
// because it almost certainly indicates a clock/seed misconfiguration.
const CollisionSuffixCap = 99

// planShortHashLen is the leading-hex-character count from data-model.md §3.
const planShortHashLen = 8

// PlanShortHash returns the first 8 hex characters of planHash, lower-cased.
// Inputs accepted include both bare hex digests and the canonical
// "sha256:<hex>" form persisted in plan metadata; both surface as the same
// 8-char prefix.
//
// Returns an error wrapping statestore.ErrInvalid when the input does not
// expose at least 8 lowercase hex characters after the optional "sha256:"
// prefix is stripped.
func PlanShortHash(planHash string) (string, error) {
	h := strings.TrimSpace(planHash)
	h = strings.TrimPrefix(h, "sha256:")
	h = strings.ToLower(h)
	if len(h) < planShortHashLen {
		return "", fmt.Errorf("%w: planHash %q has fewer than %d hex chars", statestore.ErrInvalid, planHash, planShortHashLen)
	}
	for i := 0; i < planShortHashLen; i++ {
		c := h[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return "", fmt.Errorf("%w: planHash %q contains non-hex byte %q", statestore.ErrInvalid, planHash, c)
		}
	}
	return h[:planShortHashLen], nil
}

// scopePart extracts the "<scope>-<shortSha>" portion of a revision key from
// a TriggerOccurrence. We reuse the trigger key the trigger resolver already
// produced — its format (trg-<scope>-<shortSha>) makes the revision key a
// deterministic function of the trigger plus the plan hash, which is the
// uniqueness property tested in test-plan.md §3.2.
//
// The leading "trg-" prefix is stripped so the rendered revision key reads
// rev-<scope>-<shortSha>-p<planHash8> per data-model.md §3.1.
func scopePart(trig triggerctx.TriggerOccurrence) (string, error) {
	tk := strings.TrimSpace(trig.TriggerKey)
	if tk == "" {
		return "", fmt.Errorf("%w: missing trigger field TriggerKey", statestore.ErrInvalid)
	}
	if !triggerctx.TriggerKeyPattern().MatchString(tk) {
		return "", fmt.Errorf("%w: TriggerKey %q does not match TriggerKeyPattern", statestore.ErrInvalid, tk)
	}
	const prefix = "trg-"
	if !strings.HasPrefix(tk, prefix) {
		return "", fmt.Errorf("%w: TriggerKey %q is missing the trg- prefix", statestore.ErrInvalid, tk)
	}
	return tk[len(prefix):], nil
}

// RevisionKey returns the canonical base revision key for the given
// (TriggerOccurrence, planHash) pair:
//
//	rev-<scope>-<shortSha>-p<planHash8>
//
// The function is total and deterministic: identical inputs produce
// identical output. Collision-resolution suffixes (-x1, -x2, …) are added
// only by ResolveCollision against an actual StateStore.
//
// Returns an error wrapping statestore.ErrInvalid when the trigger is
// missing required fields (TriggerKey) or planHash exposes fewer than 8 hex
// characters.
func RevisionKey(trig triggerctx.TriggerOccurrence, planHash string) (string, error) {
	scope, err := scopePart(trig)
	if err != nil {
		return "", err
	}
	short, err := PlanShortHash(planHash)
	if err != nil {
		return "", err
	}
	key := fmt.Sprintf("rev-%s-p%s", scope, short)
	if err := ValidateRevisionKey(key); err != nil {
		// scopePart accepts the trigger-key alphabet (which includes
		// uppercase via the dirty/no-git sentinels of triggerctx) but
		// data-model.md §3.1 only allows lowercase. Surface a clear
		// validation error rather than producing an unparseable key.
		return "", err
	}
	return key, nil
}

// ValidateRevisionKey enforces the regex from data-model.md §3.1. Returns
// an error wrapping statestore.ErrInvalid on mismatch.
func ValidateRevisionKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: empty revision key", statestore.ErrInvalid)
	}
	if !revisionKeyPattern.MatchString(key) {
		return fmt.Errorf("%w: revision key %q does not match %s", statestore.ErrInvalid, key, revisionKeyPattern.String())
	}
	return nil
}

// ResolveCollision claims the revision-index slot for a candidate key by
// writing reservation bytes via CreateIfAbsent. If the base candidate is
// taken, ResolveCollision tries candidate-x1, candidate-x2, …, up to
// CollisionSuffixCap. The first key that wins the CreateIfAbsent race is
// returned along with the reservation bytes the writer must overwrite with
// the real RevisionIndexEntry once trigger/revision/plan have been written.
//
// Concurrent ResolveCollision callers with the same candidate are guaranteed
// distinct return values: CreateIfAbsent is exclusive (state-store.md §3),
// so each call either wins the slot it tries or moves on.
//
// Returns an error wrapping statestore.ErrConflict if every suffix up to
// the cap is taken; the caller surfaces that to the user verbatim.
//
// reservation is intentionally a sentinel value the production writer
// replaces with the real RevisionIndexEntry on completion. Crash-recovery
// tooling can identify orphan reservations by their {"reserved":true}
// shape; M3+ migration is responsible for cleaning them up.
func ResolveCollision(ctx context.Context, store statestore.StateStore, candidate string) (string, error) {
	if err := ValidateRevisionKey(candidate); err != nil {
		return "", err
	}
	// reservation is a tiny placeholder doc CreateIfAbsent writes to claim
	// the slot. The full RevisionIndexEntry overwrite happens later in the
	// writer (step 6) via store.Write, after the body files have landed.
	reservation := []byte(`{"reserved":true}` + "\n")
	for i := 0; i <= CollisionSuffixCap; i++ {
		key := candidate
		if i > 0 {
			key = fmt.Sprintf("%s-x%d", candidate, i)
		}
		if err := ValidateRevisionKey(key); err != nil {
			return "", err
		}
		_, err := store.CreateIfAbsent(ctx, statestore.RevisionIndexPath(key), reservation)
		switch {
		case err == nil:
			return key, nil
		case errors.Is(err, statestore.ErrExists):
			continue
		default:
			return "", err
		}
	}
	return "", fmt.Errorf("%w: revision-key collision suffix exhausted at -x%d for %q",
		statestore.ErrConflict, CollisionSuffixCap, candidate)
}
