package revision

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// RevisionRef is the result of a successful ResolveRevision call. It bundles
// the persisted-or-synthesized revision plus the trigger that produced it
// and the canonical plan bytes the execution will compile against. Source
// records which of the seven resolver branches matched (compat §3) so
// downstream callers (M5 `orun run`) can drive UX cues — e.g. printing a
// "synthesized from legacy plan" notice for branch 5.
type RevisionRef struct {
	Source     ResolveSource
	Revision   PlanRevision
	Trigger    triggerctx.TriggerOccurrence
	PlanBytes  []byte
	// Synthesized is true when the revision was produced in-memory (compat
	// §3 branches 2 and 5). Synthesized revisions are NOT written to disk
	// by ResolveRevision; the caller materializes them iff
	// `--persist-revision` is passed (Phase 1 reserves but does not
	// implement that flag).
	Synthesized bool
	// LegacyPath, when non-empty, is the bare arg consumed by branch 5 —
	// useful for telemetry and the migration command's "saw this hash"
	// summary. Always empty for branches other than 5.
	LegacyPath string
	// FilePath, when non-empty, is the plan-file path consumed by branch 2
	// (the caller-supplied plan file). Empty for other branches.
	FilePath string
	// NamedRefName, when non-empty, is the named-ref alias matched by
	// branch 4 — handy for logging (rev-foo via @release).
	NamedRefName string
}

// ResolveSource enumerates the seven resolver branches in compat §3. The
// values are stable strings (never-cleaned-up by stringer) so they can
// appear in logs and structured output without lying about ordering.
type ResolveSource string

const (
	ResolveSourceLatest        ResolveSource = "latest-revision"
	ResolveSourceFile          ResolveSource = "plan-file"
	ResolveSourceRevisionKey   ResolveSource = "revision-key"
	ResolveSourceNamedRef      ResolveSource = "named-ref"
	ResolveSourceLegacyHash    ResolveSource = "legacy-plan-hash"
	ResolveSourceComponentName ResolveSource = "component-name"
)

// ResolveOptions modulates ResolveRevision. Fields are optional; the zero
// value is safe and matches `orun run <arg>` defaults.
type ResolveOptions struct {
	// IsComponentName is consulted by branch 6 — the resolver does NOT
	// know the component registry; the caller (M5 CLI) injects whether
	// arg matches a known component name. When nil, branch 6 is treated
	// as "no component matched" and the resolver falls through to branch
	// 7 (ErrAmbiguousArg). The function takes the bare arg and returns
	// true iff it names a registered component.
	IsComponentName func(string) bool

	// Now stamps any synthesized revision's CreatedAt. Mirrors
	// Config.Now; defaults to time.Now().UTC() when nil.
	Now func() time.Time
}

// ResolveRevision implements the seven-branch resolver from compat §3:
//
//	1. arg == ""        → refs/latest-revision.json
//	2. arg is a file    → load plan from file; synthesize manual revision
//	3. arg matches the revision-key regex → revisions/<arg>/{plan,revision,trigger}.json
//	4. arg matches refs/named/<arg>.json   → indirect to revision key
//	5. arg is hex       → plans/<arg>.json (legacy) + synthesize migrated revision
//	6. arg names a component (per opts.IsComponentName) → ErrComponentRunUnchanged
//	7. otherwise        → ErrAmbiguousArg
//
// Branch ordering matches the spec exactly; we do NOT reorder for
// performance because precedence collisions (an arg that is both a file
// and a hex string, say) MUST be resolved file-first per the spec.
//
// Branches 2 and 5 do not write to disk; the synthesized revision flows
// to the caller in RevisionRef with Synthesized=true. The
// `--persist-revision` flag is reserved for future addition.
func ResolveRevision(
	ctx context.Context,
	store statestore.StateStore,
	arg string,
	opts ResolveOptions,
) (RevisionRef, error) {
	if store == nil {
		return RevisionRef{}, fmt.Errorf("%w: revision.ResolveRevision store is nil", statestore.ErrInvalid)
	}

	// Branch 1 — empty arg → latest-revision.
	if arg == "" {
		return resolveFromLatestRef(ctx, store)
	}

	// Branch 2 — arg is a real file path on the local filesystem. The
	// resolver does not gate this on extension or content-type because
	// the spec is path-shaped: any readable file wins this branch.
	if isExistingFile(arg) {
		return resolveFromFile(ctx, store, arg, opts)
	}

	// Branch 3 — arg matches the revision-key regex.
	if revisionKeyPattern.MatchString(arg) {
		ref, err := resolveFromRevisionKey(ctx, store, arg)
		if err == nil {
			return ref, nil
		}
		// Not on disk: fall through. The spec lets a bare hex hash that
		// ALSO happens to match the regex be tried as a legacy plan in
		// branch 5; in practice the regex requires a "rev-" prefix so
		// this fall-through is paranoia.
		if !errors.Is(err, statestore.ErrNotFound) {
			return RevisionRef{}, err
		}
	}

	// Branch 4 — named ref. Use a single-component validator so an arg
	// like "foo/bar" cannot escape into a multi-segment path.
	if statestore.ValidateComponent(arg) == nil {
		named, _, err := statestore.ReadNamedRef(ctx, store, arg)
		if err == nil {
			ref, err := resolveFromRevisionKey(ctx, store, named.RevisionKey)
			if err != nil {
				return RevisionRef{}, fmt.Errorf("named ref %q → revision %q: %w",
					arg, named.RevisionKey, err)
			}
			ref.Source = ResolveSourceNamedRef
			ref.NamedRefName = arg
			return ref, nil
		}
		if !errors.Is(err, statestore.ErrNotFound) {
			return RevisionRef{}, fmt.Errorf("read named ref %q: %w", arg, err)
		}
	}

	// Branch 5 — legacy plan hash. Hex check has no length floor (some
	// historical legacy files used 8-char names, others used the full
	// 64-char digest), but normalizeLegacyChecksum will reject < 8.
	if isHexLower(arg) && len(arg) >= planShortHashLen {
		ref, err := resolveFromLegacyHash(ctx, store, arg, opts)
		if err == nil {
			return ref, nil
		}
		if !errors.Is(err, statestore.ErrNotFound) {
			return RevisionRef{}, err
		}
		// Fall through to branch 6/7 if no legacy file exists.
	}

	// Branch 6 — component name. We never invoke the component-run code
	// path from here; the caller dispatches based on the sentinel.
	if opts.IsComponentName != nil && opts.IsComponentName(arg) {
		return RevisionRef{Source: ResolveSourceComponentName},
			fmt.Errorf("%w: %q", ErrComponentRunUnchanged, arg)
	}

	// Branch 7 — nothing matched.
	return RevisionRef{}, fmt.Errorf("%w: %q", ErrAmbiguousArg, arg)
}

// resolveFromLatestRef is branch 1.
func resolveFromLatestRef(ctx context.Context, store statestore.StateStore) (RevisionRef, error) {
	latest, _, err := statestore.ReadLatestRevisionRef(ctx, store)
	if err != nil {
		return RevisionRef{}, fmt.Errorf("read latest-revision ref: %w", err)
	}
	ref, err := resolveFromRevisionKey(ctx, store, latest.RevisionKey)
	if err != nil {
		return RevisionRef{}, fmt.Errorf("latest-revision %q: %w", latest.RevisionKey, err)
	}
	ref.Source = ResolveSourceLatest
	return ref, nil
}

// resolveFromRevisionKey is the branches-3/1/4 worker that loads the
// already-persisted revision triplet.
func resolveFromRevisionKey(ctx context.Context, store statestore.StateStore, revKey string) (RevisionRef, error) {
	if err := ValidateRevisionKey(revKey); err != nil {
		return RevisionRef{}, err
	}
	planBytes, _, err := store.Read(ctx, statestore.PlanPath(revKey))
	if err != nil {
		return RevisionRef{}, fmt.Errorf("read plan.json: %w", err)
	}
	revBytes, _, err := store.Read(ctx, statestore.RevisionDocPath(revKey))
	if err != nil {
		return RevisionRef{}, fmt.Errorf("read revision.json: %w", err)
	}
	trigBytes, _, err := store.Read(ctx, statestore.TriggerPath(revKey))
	if err != nil {
		return RevisionRef{}, fmt.Errorf("read trigger.json: %w", err)
	}
	var rev PlanRevision
	if err := strictJSON(revBytes, &rev); err != nil {
		return RevisionRef{}, fmt.Errorf("decode revision.json: %w", err)
	}
	var trig triggerctx.TriggerOccurrence
	if err := strictJSON(trigBytes, &trig); err != nil {
		return RevisionRef{}, fmt.Errorf("decode trigger.json: %w", err)
	}
	return RevisionRef{
		Source:    ResolveSourceRevisionKey,
		Revision:  rev,
		Trigger:   trig,
		PlanBytes: planBytes,
	}, nil
}

// resolveFromFile is branch 2 — reads a plan file from the local
// filesystem and synthesizes a system.manual revision in memory.
func resolveFromFile(ctx context.Context, store statestore.StateStore, path string, opts ResolveOptions) (RevisionRef, error) {
	planBytes, err := os.ReadFile(path)
	if err != nil {
		return RevisionRef{}, fmt.Errorf("%w: read plan file %q: %v",
			statestore.ErrInvalid, path, err)
	}
	planHash := canonicalPlanHash(planBytes)
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now().UTC()
	}
	trig := triggerctx.NewSystemReplay(triggerctx.SystemOptions{
		Now: now,
	})
	rev, err := synthesizeRevision(trig, planHash, now)
	if err != nil {
		return RevisionRef{}, err
	}
	_ = ctx // store/ctx unused on branch 2 by design — synthesis is in-memory.
	_ = store
	return RevisionRef{
		Source:      ResolveSourceFile,
		Revision:    rev,
		Trigger:     trig,
		PlanBytes:   planBytes,
		Synthesized: true,
		FilePath:    path,
	}, nil
}

// resolveFromLegacyHash is branch 5 — reads .orun/plans/<arg>.json from
// the same statestore (compat §2 keeps the legacy path inside the new
// store) and synthesizes a system.migrated revision in memory.
func resolveFromLegacyHash(ctx context.Context, store statestore.StateStore, arg string, opts ResolveOptions) (RevisionRef, error) {
	checksum, err := normalizeLegacyChecksum(arg)
	if err != nil {
		return RevisionRef{}, err
	}
	path, err := legacyPlanPath(checksum)
	if err != nil {
		return RevisionRef{}, err
	}
	planBytes, _, err := store.Read(ctx, path)
	if err != nil {
		return RevisionRef{}, fmt.Errorf("read legacy plan %q: %w", path, err)
	}
	now := time.Now().UTC()
	if opts.Now != nil {
		now = opts.Now().UTC()
	}
	planHash := canonicalPlanHash(planBytes)
	trig := triggerctx.NewSystemMigrated(triggerctx.SystemOptions{
		Now: now,
	})
	rev, err := synthesizeRevision(trig, planHash, now)
	if err != nil {
		return RevisionRef{}, err
	}
	return RevisionRef{
		Source:      ResolveSourceLegacyHash,
		Revision:    rev,
		Trigger:     trig,
		PlanBytes:   planBytes,
		Synthesized: true,
		LegacyPath:  arg,
	}, nil
}

// synthesizeRevision builds an in-memory PlanRevision for resolver branches
// 2 and 5. The revision is NOT written to disk — Synthesized=true on the
// returned ref signals this to the caller.
func synthesizeRevision(trig triggerctx.TriggerOccurrence, planHash string, now time.Time) (PlanRevision, error) {
	short, err := PlanShortHash(planHash)
	if err != nil {
		return PlanRevision{}, err
	}
	revKey, err := RevisionKey(trig, planHash)
	if err != nil {
		return PlanRevision{}, err
	}
	return PlanRevision{
		APIVersion:    APIVersion,
		Kind:          KindName,
		RevisionID:    idPrefixRevision + "synthetic",
		RevisionKey:   revKey,
		TriggerID:     trig.TriggerID,
		TriggerKey:    trig.TriggerKey,
		PlanHash:      planHash,
		PlanShortHash: short,
		Source:        trig.Source,
		Summary:       summaryFromScope(trig, 0),
		CreatedAt:     now,
	}, nil
}

// canonicalPlanHash returns the "sha256:<hex>" form. The resolver uses this
// when the caller-supplied plan bytes were not accompanied by a hash —
// branches 2 and 5 derive the hash directly from the plan content.
func canonicalPlanHash(planBytes []byte) string {
	sum := sha256.Sum256(planBytes)
	return "sha256:" + hex.EncodeToString(sum[:])
}

// isExistingFile reports whether path names a regular file the OS can
// stat. Used by branch 2 only; the resolver intentionally never tries to
// stat a directory or follow non-regular files.
func isExistingFile(path string) bool {
	if path == "" {
		return false
	}
	// Reject obvious non-paths early — a bare hex string shouldn't
	// trigger an os.Stat that always fails.
	if !strings.ContainsAny(path, "/.") {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// strictJSON unmarshals raw into out with DisallowUnknownFields. Mirrors
// statestore.unmarshalRef but stays inside this package — refs and
// revision/trigger documents share the strict-decode posture but the
// helper there is unexported.
func strictJSON(raw []byte, out any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("%w: %v", statestore.ErrInvalid, err)
	}
	return nil
}
