// Package revkey derives the canonical human revision key
// (rev-<scope>-<sha7>-p<planHash8>, data-model.md §3.1) from a trigger
// occurrence and a plan hash. It is the object model's source of the revision
// display key (stamped into plan.metadata.revision and the object-model
// PlanRevision.HumanKey), decoupled from the retired internal/revision store.
package revkey

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/sourceplane/orun/internal/triggerctx"
)

// ErrInvalid wraps malformed-input errors from key derivation.
var ErrInvalid = errors.New("revkey: invalid input")

// revisionKeyPattern is the validation regex from data-model.md §3.1:
//
//	^rev-[a-z0-9-]+-p[a-f0-9]{8}(-x\d+)?$
//
// The base form is rev-<scope>-p<planHash8>; the optional -xN suffix was used
// by the legacy store's collision resolver and is still accepted here.
var revisionKeyPattern = regexp.MustCompile(`^rev-[a-z0-9-]+-p[a-f0-9]{8}(-x\d+)?$`)

// planShortHashLen is the leading-hex-character count from data-model.md §3.
const planShortHashLen = 8

// PlanShortHash returns the first 8 hex characters of planHash, lower-cased.
// Both bare hex digests and the canonical "sha256:<hex>" form are accepted;
// both surface as the same 8-char prefix. Returns an error wrapping ErrInvalid
// when fewer than 8 lowercase hex characters are available.
func PlanShortHash(planHash string) (string, error) {
	h := strings.TrimSpace(planHash)
	h = strings.TrimPrefix(h, "sha256:")
	h = strings.ToLower(h)
	if len(h) < planShortHashLen {
		return "", fmt.Errorf("%w: planHash %q has fewer than %d hex chars", ErrInvalid, planHash, planShortHashLen)
	}
	for i := 0; i < planShortHashLen; i++ {
		c := h[i]
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return "", fmt.Errorf("%w: planHash %q contains non-hex byte %q", ErrInvalid, planHash, c)
		}
	}
	return h[:planShortHashLen], nil
}

// scopePart extracts the "<scope>-<sha7>" portion of a revision key from a
// trigger occurrence by stripping the "trg-" prefix off its trigger key, making
// the revision key a deterministic function of the trigger plus the plan hash.
func scopePart(trig triggerctx.TriggerOccurrence) (string, error) {
	tk := strings.TrimSpace(trig.TriggerKey)
	if tk == "" {
		return "", fmt.Errorf("%w: missing trigger field TriggerKey", ErrInvalid)
	}
	if !triggerctx.TriggerKeyPattern().MatchString(tk) {
		return "", fmt.Errorf("%w: TriggerKey %q does not match TriggerKeyPattern", ErrInvalid, tk)
	}
	const prefix = "trg-"
	if !strings.HasPrefix(tk, prefix) {
		return "", fmt.Errorf("%w: TriggerKey %q is missing the trg- prefix", ErrInvalid, tk)
	}
	return tk[len(prefix):], nil
}

// RevisionKey returns the canonical base revision key for the given
// (TriggerOccurrence, planHash) pair: rev-<scope>-<sha7>-p<planHash8>. The
// function is total and deterministic: identical inputs produce identical
// output. Returns an error wrapping ErrInvalid when the trigger is missing
// TriggerKey or planHash exposes fewer than 8 hex characters.
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
		// scopePart accepts the trigger-key alphabet (which can include
		// uppercase via triggerctx's dirty/no-git sentinels) but §3.1 only
		// allows lowercase; surface a clear validation error.
		return "", err
	}
	return key, nil
}

// ValidateRevisionKey enforces the regex from data-model.md §3.1. Returns an
// error wrapping ErrInvalid on mismatch.
func ValidateRevisionKey(key string) error {
	if key == "" {
		return fmt.Errorf("%w: empty revision key", ErrInvalid)
	}
	if !revisionKeyPattern.MatchString(key) {
		return fmt.Errorf("%w: revision key %q does not match %s", ErrInvalid, key, revisionKeyPattern.String())
	}
	return nil
}
