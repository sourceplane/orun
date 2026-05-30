package revision

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// casRetryBudget bounds the number of CompareAndSwap attempts WriteRevision
// makes against refs/latest-revision.json. The caller-owns-retry pattern in
// state-store.md §6 expects a small, bounded retry on ErrConflict — five
// attempts is comfortable headroom for normal contention while still
// surfacing systematic failures (e.g. a stuck remote driver) instead of
// looping forever.
const casRetryBudget = 5

// Config configures a WriteRevision call. Zero values for Now / NewID are
// filled in with sensible defaults (UTC time.Now and oklog/ulid/v2). The
// CompatibilityWrites flag defaults to true when the entire Config is the
// zero value but is honored verbatim once the caller sets it explicitly —
// see writeCompatibilityMirror for the legacy-stub semantics.
type Config struct {
	// Store is the StateStore the writer composes against. Required.
	Store statestore.StateStore

	// Now stamps CreatedAt on every persisted artifact. When nil,
	// time.Now().UTC is used.
	Now func() time.Time

	// NewID generates the RevisionID written into revision.json (data-model.md
	// §3). When nil, a monotonic ULID prefixed "rev_" is generated.
	NewID func() string

	// CompatibilityWrites toggles the legacy-mirror branch (currently a
	// // TODO(m5) stub — see writeCompatibilityMirror). The zero-value
	// resolution lives in resolveDefaults so the caller's intent — explicit
	// false vs unset — is preserved.
	CompatibilityWrites bool

	// compatibilityWritesSet is set in resolveDefaults to disambiguate the
	// "user explicitly passed false" case from "user didn't touch the
	// field". It is intentionally unexported: callers express the choice
	// via the field above; the writer normalizes inside resolveDefaults.
	compatibilityWritesSet bool
}

// resolveDefaults returns a Config copy with nil functions filled in and
// the CompatibilityWrites default applied. The original is left untouched
// so test fixtures stay intact across calls.
//
// The default-true semantics for CompatibilityWrites match the gating flag
// described in design.md §8 / compatibility-and-migration.md: legacy mirror
// stays on until M5 explicitly disables it.
func (c Config) resolveDefaults() Config {
	out := c
	if out.Now == nil {
		out.Now = func() time.Time { return time.Now().UTC() }
	}
	if out.NewID == nil {
		out.NewID = func() string {
			return idPrefixRevision + ulid.Make().String()
		}
	}
	if !out.compatibilityWritesSet {
		// Zero-value Configs default to compatibility-on per the M3 plan.
		out.CompatibilityWrites = true
	}
	return out
}

// WithCompatibilityWrites returns a copy of c with CompatibilityWrites set
// explicitly. Tests use this to flip the flag off without losing the "set"
// signal that resolveDefaults relies on.
func (c Config) WithCompatibilityWrites(on bool) Config {
	out := c
	out.CompatibilityWrites = on
	out.compatibilityWritesSet = true
	return out
}

// validateTrigger enforces the per-task constraint that a TriggerOccurrence
// flowing into WriteRevision already has TriggerID, TriggerKey, and
// CreatedAt populated. Returns an error wrapping statestore.ErrInvalid on
// missing fields so the writer surfaces a sentinel-routed failure rather
// than producing a half-formed revision.
func validateTrigger(trig triggerctx.TriggerOccurrence) error {
	if trig.TriggerID == "" {
		return fmt.Errorf("%w: missing trigger field TriggerID", statestore.ErrInvalid)
	}
	if trig.TriggerKey == "" {
		return fmt.Errorf("%w: missing trigger field TriggerKey", statestore.ErrInvalid)
	}
	if trig.TriggerName == "" {
		return fmt.Errorf("%w: missing trigger field TriggerName", statestore.ErrInvalid)
	}
	if trig.CreatedAt.IsZero() {
		return fmt.Errorf("%w: missing trigger field CreatedAt", statestore.ErrInvalid)
	}
	return nil
}

// summaryFromScope derives a RevSummary from the trigger's PlanScope. The
// summary fields cloned here are the ones revision.json surfaces; other
// PlanScope fields stay only inside trigger.json. JobCount is left at zero
// in PR-A — the planner-supplied count threads through in PR-B (Task 0008)
// once WriteManifest lands.
func summaryFromScope(trig triggerctx.TriggerOccurrence) RevSummary {
	scope := trig.PlanScope.Mode
	if scope == "" {
		scope = triggerctx.PlanScopeFull
	}
	out := RevSummary{
		Scope:              scope,
		ActiveEnvironments: append([]string(nil), trig.PlanScope.ActiveEnvironments...),
	}
	if len(trig.PlanScope.ChangedComponents) > 0 {
		out.ChangedComponents = append([]string(nil), trig.PlanScope.ChangedComponents...)
	}
	// JSON marshalling treats a nil slice as null; data-model.md §3 wants
	// activeEnvironments present (even when empty) — emit [] explicitly.
	if out.ActiveEnvironments == nil {
		out.ActiveEnvironments = []string{}
	}
	return out
}

// WriteRevision executes the seven-step ordered write list from design.md
// §5.1 / cli-surface.md §1.2 against cfg.Store:
//
//  0. (precondition) reserve indexes/revisions/<revisionKey>.json via
//     CreateIfAbsent, applying -xN collision suffixes up to
//     CollisionSuffixCap. The reservation guarantees concurrent writers see
//     distinct revision keys before any body file is written.
//  1. write revisions/<revKey>/trigger.json
//  2. write revisions/<revKey>/revision.json
//  3. write revisions/<revKey>/plan.json
//  4. update refs/latest-revision.json via CompareAndSwap (caller-owns-retry,
//     bounded by casRetryBudget — state-store.md §6).
//  5. update refs/triggers/<triggerName>/{latest.json, <scope>.json}.
//  6. finalize indexes/revisions/<revKey>.json with the real
//     RevisionIndexEntry (overwriting the reservation).
//  7. on first new-layout creation, write .orun/version.json via
//     CreateIfAbsent (idempotent; ErrExists is success).
//
// The pre-step ordering — claim-first, body-second — is the only structure
// that produces distinct revision keys under concurrent (trig, planHash)
// duplicates without polluting the body directory. See
// ai/reports/task-0007-implementer.md → Assumptions for the rationale.
//
// Returns the persisted PlanRevision so PR-B's manifest helper can compose
// without re-reading revision.json. Errors wrap one of the four statestore
// sentinels; this package introduces no new sentinels.
//
// Manifest writes and the legacy compatibility-mirror body are out of scope
// for PR-A — see writeCompatibilityMirror for the // TODO(m5) seam.
func WriteRevision(
	ctx context.Context,
	cfg Config,
	trig triggerctx.TriggerOccurrence,
	planBytes []byte,
	planHash string,
) (PlanRevision, error) {
	if cfg.Store == nil {
		return PlanRevision{}, fmt.Errorf("%w: revision.Config.Store is nil", statestore.ErrInvalid)
	}
	if err := validateTrigger(trig); err != nil {
		return PlanRevision{}, err
	}
	if len(planBytes) == 0 {
		return PlanRevision{}, fmt.Errorf("%w: planBytes is empty", statestore.ErrInvalid)
	}
	short, err := PlanShortHash(planHash)
	if err != nil {
		return PlanRevision{}, err
	}
	cfg = cfg.resolveDefaults()
	now := cfg.Now().UTC()
	store := cfg.Store

	// Step 0 — claim the revision-key slot.
	candidate, err := RevisionKey(trig, planHash)
	if err != nil {
		return PlanRevision{}, err
	}
	revKey, err := ResolveCollision(ctx, store, candidate)
	if err != nil {
		return PlanRevision{}, err
	}

	// Step 1 — trigger.json.
	if _, err := store.Write(ctx, statestore.TriggerPath(revKey),
		marshalCanonicalJSON(trig), statestore.WriteOptions{}); err != nil {
		return PlanRevision{}, fmt.Errorf("write trigger.json: %w", err)
	}

	// Step 2 — revision.json.
	rev := PlanRevision{
		APIVersion:    APIVersion,
		Kind:          KindName,
		RevisionID:    cfg.NewID(),
		RevisionKey:   revKey,
		TriggerID:     trig.TriggerID,
		TriggerKey:    trig.TriggerKey,
		PlanHash:      planHash,
		PlanShortHash: short,
		Source:        trig.Source,
		Summary:       summaryFromScope(trig),
		CreatedAt:     now,
	}
	if _, err := store.Write(ctx, statestore.RevisionDocPath(revKey),
		marshalCanonicalJSON(rev), statestore.WriteOptions{}); err != nil {
		return PlanRevision{}, fmt.Errorf("write revision.json: %w", err)
	}

	// Step 3 — plan.json (verbatim caller-supplied bytes — this is the
	// canonical plan over which planHash was computed; mutating the bytes
	// would invalidate the hash).
	if _, err := store.Write(ctx, statestore.PlanPath(revKey),
		planBytes, statestore.WriteOptions{}); err != nil {
		return PlanRevision{}, fmt.Errorf("write plan.json: %w", err)
	}

	// Step 4 — refs/latest-revision.json via bounded CAS retry.
	if err := updateLatestRevisionRef(ctx, store, rev, now); err != nil {
		return PlanRevision{}, err
	}

	// Step 5 — refs/triggers/<triggerName>/{latest.json, <scope>.json}.
	if err := updateTriggerRefs(ctx, store, trig, rev, now); err != nil {
		return PlanRevision{}, err
	}

	// Step 6 — finalize indexes/revisions/<revKey>.json. The reservation
	// from step 0 is overwritten with the real RevisionIndexEntry; we use
	// store.Write here because CreateIfAbsent would surface ErrExists
	// against our own reservation.
	entry := statestore.RevisionIndexEntry{
		RevisionKey: revKey,
		RevisionID:  rev.RevisionID,
		TriggerKey:  trig.TriggerKey,
		PlanHash:    planHash,
		CreatedAt:   now,
		Path:        statestore.RevisionDir(revKey),
	}
	if _, err := store.Write(ctx, statestore.RevisionIndexPath(revKey),
		marshalCanonicalJSON(entry), statestore.WriteOptions{}); err != nil {
		return PlanRevision{}, fmt.Errorf("finalize revision index: %w", err)
	}

	// Step 7 — .orun/version.json (idempotent, first-write-wins).
	if err := EnsureStateStoreVersion(ctx, store, cfg.Now); err != nil {
		return PlanRevision{}, err
	}

	// Compatibility branch — currently a // TODO(m5) stub. Invoking the
	// stub when the flag is true gives M5 a single seam to fill; skipping
	// when the flag is false matches the intent of disabling compat
	// writes entirely.
	if cfg.CompatibilityWrites {
		if err := writeCompatibilityMirror(ctx, store, rev, planBytes); err != nil {
			return PlanRevision{}, err
		}
	}

	return rev, nil
}

// updateLatestRevisionRef performs the caller-owns-retry CAS dance on
// refs/latest-revision.json (state-store.md §6). The first iteration handles
// the bootstrap case (ref does not yet exist) by falling back to
// CreateIfAbsent so we don't surface a misleading ErrNotFound.
func updateLatestRevisionRef(
	ctx context.Context,
	store statestore.StateStore,
	rev PlanRevision,
	now time.Time,
) error {
	next := statestore.LatestRevisionRef{
		RevisionKey: rev.RevisionKey,
		RevisionID:  rev.RevisionID,
		PlanHash:    rev.PlanHash,
		CreatedAt:   now,
	}
	for attempt := 0; attempt < casRetryBudget; attempt++ {
		_, prev, err := statestore.ReadLatestRevisionRef(ctx, store)
		if err != nil {
			if !errors.Is(err, statestore.ErrNotFound) {
				return fmt.Errorf("read latest-revision ref: %w", err)
			}
			// Bootstrap: try to create. On loss (someone else won the
			// bootstrap race) drop into the CAS path on the next loop.
			if _, err := store.CreateIfAbsent(ctx, statestore.LatestRevisionRefPath(),
				marshalCanonicalJSON(next)); err == nil {
				return nil
			} else if !errors.Is(err, statestore.ErrExists) {
				return fmt.Errorf("create latest-revision ref: %w", err)
			}
			continue
		}
		if _, err := statestore.CASLatestRevisionRef(ctx, store, prev, next); err != nil {
			if errors.Is(err, statestore.ErrConflict) {
				continue
			}
			return fmt.Errorf("cas latest-revision ref: %w", err)
		}
		return nil
	}
	return fmt.Errorf("%w: latest-revision ref CAS retry budget (%d) exhausted",
		statestore.ErrConflict, casRetryBudget)
}

// updateTriggerRefs writes both refs/triggers/<name>/latest.json and the
// per-scope ref. Trigger refs are eventual-freshness pointers (design.md
// §9): the writes are unconditional rather than CAS, matching the cli-surface
// description ("update refs/triggers/<triggerName>/{latest.json,
// <scope>.json}") — multiple writers racing for the same trigger key are
// permitted to clobber each other; the loser's bytes were equivalent to the
// winner's bytes for the same (TriggerKey, RevisionKey) pair.
func updateTriggerRefs(
	ctx context.Context,
	store statestore.StateStore,
	trig triggerctx.TriggerOccurrence,
	rev PlanRevision,
	now time.Time,
) error {
	tr := statestore.TriggerRef{
		TriggerName:  trig.TriggerName,
		TriggerKey:   trig.TriggerKey,
		RevisionKey:  rev.RevisionKey,
		HeadRevision: trig.Source.HeadRevision,
		CreatedAt:    now,
	}
	if _, err := statestore.WriteTriggerRef(ctx, store,
		statestore.TriggerRefScope{Name: trig.TriggerName, Latest: true}, tr); err != nil {
		return fmt.Errorf("write trigger latest ref: %w", err)
	}
	if scope := trig.Source.SourceScope; scope != "" {
		if _, err := statestore.WriteTriggerRef(ctx, store,
			statestore.TriggerRefScope{Name: trig.TriggerName, Scope: scope}, tr); err != nil {
			return fmt.Errorf("write trigger scope ref: %w", err)
		}
	}
	return nil
}

// EnsureStateStoreVersion writes .orun/version.json on first creation only.
// Subsequent invocations observe ErrExists from CreateIfAbsent and return
// nil — concurrent first-callers therefore converge to a single version.json
// without coordination.
//
// now is the same clock the writer uses elsewhere; passing it explicitly
// keeps tests deterministic.
func EnsureStateStoreVersion(ctx context.Context, store statestore.StateStore, now func() time.Time) error {
	if store == nil {
		return fmt.Errorf("%w: EnsureStateStoreVersion store is nil", statestore.ErrInvalid)
	}
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	v := StateStoreVersion{
		APIVersion: APIVersion,
		Kind:       StateStoreVersionKind,
		Layout:     StateStoreLayoutRevisionFirst,
		Version:    StateStoreVersionCurrent,
		CreatedAt:  now().UTC(),
	}
	_, err := store.CreateIfAbsent(ctx, stateStoreVersionPath(), marshalCanonicalJSON(v))
	if err == nil {
		return nil
	}
	if errors.Is(err, statestore.ErrExists) {
		return nil
	}
	return fmt.Errorf("write version doc: %w", err)
}

// writeCompatibilityMirror is the legacy mirror branch. PR-A ships the seam
// only — actual mirroring of .orun/plans/<checksum>.json + latest.json
// happens during M5 CLI rewire (compatibility-and-migration.md §4).
//
// TODO(m5): mirror plan.json as .orun/plans/<sha256-of-plan>.json and
// update .orun/plans/latest.json so legacy `orun run` paths keep working
// during the rollover window.
func writeCompatibilityMirror(
	_ context.Context,
	_ statestore.StateStore,
	_ PlanRevision,
	_ []byte,
) error {
	// Intentionally a no-op in PR-A. The function is invoked when
	// CompatibilityWrites is true so M5 has a single, already-wired seam
	// to flesh out — see Config.CompatibilityWrites.
	return nil
}
