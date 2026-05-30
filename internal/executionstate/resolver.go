package executionstate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/revision"
	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/triggerctx"
)

// ResolveSource enumerates the seven branches of ResolveExecution. The
// values are stable, never-cleaned-up strings — they appear in logs and
// structured CLI output and must not change across releases without a
// migration note. They mirror the analogous revision.ResolveSource so M5
// can compose both resolvers without surprise.
type ResolveSource string

const (
	// ResolveSourceExactKey covers the index-driven exact-key match
	// (branch 1). The arg matched indexes/executions/<arg>.json and the
	// resolver loaded the execution from the index entry's Path.
	ResolveSourceExactKey ResolveSource = "exact-key"

	// ResolveSourceRevisionScoped covers the revHint+arg lookup
	// (branch 2). The caller pinned a revision via revHint and the arg
	// resolved as an execution key directly under that revision.
	ResolveSourceRevisionScoped ResolveSource = "revision-scoped"

	// ResolveSourceLatestRef covers refs/latest-execution.json
	// (branch 3). Triggered by arg=="" or arg=="latest".
	ResolveSourceLatestRef ResolveSource = "latest-ref"

	// ResolveSourcePrefixScan covers the indexes/executions/ prefix
	// scan (branch 4). Triggered when arg is shorter than a full key
	// and matches exactly one indexed execution by prefix.
	ResolveSourcePrefixScan ResolveSource = "prefix-scan"

	// ResolveSourceLegacyFallback covers the on-disk `.orun/executions/`
	// scan (branch 5). The execution row is synthesized in-memory from
	// the legacy file content per compat §4 — read-only, no writes.
	ResolveSourceLegacyFallback ResolveSource = "legacy-fallback"
)

// ExecutionRef is the result of a successful ResolveExecution call. It
// bundles the execution row plus the branch that matched and, when the
// match was a legacy synthesis, the original on-disk path that fed it.
type ExecutionRef struct {
	Source       ResolveSource
	Execution    ExecutionRun
	RevisionKey  string
	Synthesized  bool
	LegacyPath   string
	NamedRefName string // reserved; not populated by Phase 1 branches.
}

// LegacyRoot lets ResolveExecution find legacy `.orun/executions/<id>/`
// trees. The default constructor wires this from a LocalStore root so
// callers never have to compute the path themselves; tests inject a
// fixture root under t.TempDir.
type LegacyRoot string

// String implements fmt.Stringer for diagnostic logging.
func (r LegacyRoot) String() string { return string(r) }

// ResolveOptions modulates ResolveExecution. Fields are optional; the
// zero value is safe and matches the cli-surface §3 default lookup.
type ResolveOptions struct {
	// LegacyRoot is the on-disk root of the workspace's `.orun/`
	// directory. When empty, the resolver tries to recover it from
	// store.Root() (the LocalStore convention is that Root() returns
	// the absolute filesystem path of the .orun directory). When the
	// store does not expose a usable filesystem root the legacy
	// fallback branch is skipped silently.
	LegacyRoot LegacyRoot
}

// ResolveExecution implements the seven-branch ladder described in
// task-0012.md / cli-surface.md §1.4 / compat §3, with the legacy
// fallback from compat §4:
//
//  1. exact-key — arg matches indexes/executions/<arg>.json verbatim.
//  2. revHint — revHint+arg directly addresses
//     revisions/<revHint>/executions/<arg>/execution.json.
//  3. alias / latest — arg=="" or arg=="latest" reads
//     refs/latest-execution.json.
//  4. prefix scan — arg matches exactly one indexes/executions/ entry
//     when treated as a prefix. Multiple matches surface as
//     ErrConflict (branch 6).
//  5. legacy fallback — `.orun/executions/<arg>/execution.json` on
//     disk. Read-only synthesis per compat §4.
//  6. ambiguity → fmt.Errorf wrapping statestore.ErrConflict (a prefix
//     scan or legacy scan returned multiple equal-precedence hits).
//  7. nothing matched → fmt.Errorf wrapping statestore.ErrNotFound.
//
// Branch ordering is normative; do not reorder for performance. revHint
// is consulted as a strict pin only — when it is non-empty and the
// revHint+arg pair does not exist, the resolver falls through to the
// remaining branches rather than failing immediately.
func ResolveExecution(
	ctx context.Context,
	store statestore.StateStore,
	arg string,
	revHint string,
	opts ResolveOptions,
) (ExecutionRef, error) {
	if store == nil {
		return ExecutionRef{}, fmt.Errorf("%w: executionstate.ResolveExecution store is nil", statestore.ErrInvalid)
	}

	// Branch 3 — alias / latest. The empty-arg case dominates
	// branches 1/2 because there is nothing to match exactly; the
	// "latest" alias is a documented sugar for the same target.
	if arg == "" || arg == "latest" {
		ref, err := resolveLatestRef(ctx, store)
		if err == nil {
			return ref, nil
		}
		if errors.Is(err, statestore.ErrNotFound) && opts.LegacyRoot != "" {
			// On a fresh repo with no new-layout writes we still
			// honor the legacy `.orun/executions/` scan (compat
			// §4) — that's how migration UX previews work.
			if ref, lerr := resolveLegacyLatest(opts.LegacyRoot); lerr == nil {
				return ref, nil
			}
		}
		return ExecutionRef{}, err
	}

	// Branch 2 — revHint pinned scope. revHint takes precedence over
	// the index lookup so a caller saying "this exec under THIS
	// revision" never accidentally hits a same-keyed exec under a
	// different revision.
	if revHint != "" {
		if err := revision.ValidateRevisionKey(revHint); err == nil {
			if err := statestore.ValidateComponent(arg); err == nil {
				ref, err := resolveByRevAndKey(ctx, store, revHint, arg)
				if err == nil {
					return ref, nil
				}
				if !errors.Is(err, statestore.ErrNotFound) {
					return ExecutionRef{}, err
				}
				// Fall through; revHint was a pin, not a hard requirement.
			}
		}
	}

	// Branch 1 — exact-key via the executions index.
	if statestore.ValidateComponent(arg) == nil {
		ref, err := resolveExactByIndex(ctx, store, arg)
		if err == nil {
			return ref, nil
		}
		if !errors.Is(err, statestore.ErrNotFound) {
			return ExecutionRef{}, err
		}
	}

	// Branch 4 — prefix scan against indexes/executions/.
	ref, scanErr := resolvePrefixScan(ctx, store, arg)
	if scanErr == nil {
		return ref, nil
	}
	if errors.Is(scanErr, statestore.ErrConflict) {
		// Branch 6 — multiple prefix matches. Surface ErrConflict
		// with the arg so the CLI can render a "did you mean…" hint
		// without re-parsing the message.
		return ExecutionRef{}, scanErr
	}
	if !errors.Is(scanErr, statestore.ErrNotFound) {
		return ExecutionRef{}, scanErr
	}

	// Branch 5 — legacy `.orun/executions/<arg>/` fallback.
	if root := resolveLegacyRoot(store, opts); root != "" {
		ref, err := resolveLegacy(root, arg)
		if err == nil {
			return ref, nil
		}
		if !errors.Is(err, statestore.ErrNotFound) {
			return ExecutionRef{}, err
		}
	}

	// Branch 7 — nothing matched.
	return ExecutionRef{}, fmt.Errorf("%w: execution %q not found", statestore.ErrNotFound, arg)
}

// resolveLatestRef implements branch 3 — refs/latest-execution.json.
func resolveLatestRef(ctx context.Context, store statestore.StateStore) (ExecutionRef, error) {
	latest, _, err := statestore.ReadLatestExecutionRef(ctx, store)
	if err != nil {
		return ExecutionRef{}, fmt.Errorf("read latest-execution ref: %w", err)
	}
	ref, err := resolveByRevAndKey(ctx, store, latest.RevisionKey, latest.ExecutionKey)
	if err != nil {
		return ExecutionRef{}, fmt.Errorf("latest-execution %q/%q: %w",
			latest.RevisionKey, latest.ExecutionKey, err)
	}
	ref.Source = ResolveSourceLatestRef
	return ref, nil
}

// resolveByRevAndKey implements branch 2 (and is the worker for
// branches 1, 3, 4 once they have produced a (revKey, execKey) pair).
func resolveByRevAndKey(ctx context.Context, store statestore.StateStore, revKey, execKey string) (ExecutionRef, error) {
	raw, _, err := store.Read(ctx, statestore.ExecutionDocPath(revKey, execKey))
	if err != nil {
		return ExecutionRef{}, err
	}
	var rec ExecutionRun
	if err := strictJSON(raw, &rec); err != nil {
		return ExecutionRef{}, fmt.Errorf("decode execution.json: %w", err)
	}
	return ExecutionRef{
		Source:      ResolveSourceRevisionScoped,
		Execution:   rec,
		RevisionKey: revKey,
	}, nil
}

// resolveExactByIndex implements branch 1 — indexes/executions/<arg>.json
// → revisions/<entry.RevisionKey>/executions/<arg>/execution.json.
func resolveExactByIndex(ctx context.Context, store statestore.StateStore, arg string) (ExecutionRef, error) {
	entry, _, err := statestore.ReadExecutionIndex(ctx, store, arg)
	if err != nil {
		return ExecutionRef{}, err
	}
	ref, err := resolveByRevAndKey(ctx, store, entry.RevisionKey, entry.ExecutionKey)
	if err != nil {
		return ExecutionRef{}, fmt.Errorf("index entry %q → %q/%q: %w",
			arg, entry.RevisionKey, entry.ExecutionKey, err)
	}
	ref.Source = ResolveSourceExactKey
	return ref, nil
}

// resolvePrefixScan implements branch 4 — list every executions index
// entry whose key starts with arg. Zero matches → ErrNotFound; one
// match → branch 1 worker; ≥2 matches → ErrConflict per branch 6.
func resolvePrefixScan(ctx context.Context, store statestore.StateStore, arg string) (ExecutionRef, error) {
	if arg == "" {
		return ExecutionRef{}, fmt.Errorf("%w: empty prefix", statestore.ErrNotFound)
	}
	prefix := statestore.ExecutionIndexDir()
	infos, err := store.List(ctx, prefix)
	if err != nil {
		return ExecutionRef{}, fmt.Errorf("list executions index: %w", err)
	}
	var matches []string
	for _, info := range infos {
		base := pathBase(info.Path)
		base = strings.TrimSuffix(base, ".json")
		if base == "" {
			continue
		}
		if strings.HasPrefix(base, arg) {
			matches = append(matches, base)
		}
	}
	switch len(matches) {
	case 0:
		return ExecutionRef{}, fmt.Errorf("%w: no execution matches prefix %q", statestore.ErrNotFound, arg)
	case 1:
		ref, err := resolveExactByIndex(ctx, store, matches[0])
		if err != nil {
			return ExecutionRef{}, err
		}
		ref.Source = ResolveSourcePrefixScan
		return ref, nil
	default:
		sort.Strings(matches)
		return ExecutionRef{}, fmt.Errorf("%w: prefix %q matches %d executions: %v",
			statestore.ErrConflict, arg, len(matches), matches)
	}
}

// resolveLegacyRoot picks the legacy root from opts or, failing that,
// from store.Root() when the store exposes a stringy filesystem root.
func resolveLegacyRoot(store statestore.StateStore, opts ResolveOptions) LegacyRoot {
	if opts.LegacyRoot != "" {
		return opts.LegacyRoot
	}
	type rooted interface{ Root() string }
	if r, ok := store.(rooted); ok {
		return LegacyRoot(r.Root())
	}
	return ""
}

// resolveLegacy reads `.orun/executions/<arg>/execution.json` directly
// from the local filesystem (compat §4 — read-only, no writeback) and
// synthesizes an ExecutionRun in memory with system.migrated provenance.
func resolveLegacy(root LegacyRoot, arg string) (ExecutionRef, error) {
	if root == "" {
		return ExecutionRef{}, fmt.Errorf("%w: legacy root unavailable", statestore.ErrNotFound)
	}
	if err := statestore.ValidateComponent(arg); err != nil {
		// Bare arg is unsafe to interpolate into a path — surface a
		// not-found rather than walking off the alphabet so the
		// resolver remains predictable for fuzzed input.
		return ExecutionRef{}, fmt.Errorf("%w: legacy exec %q invalid component: %v",
			statestore.ErrNotFound, arg, err)
	}
	rel := statestore.LegacyExecutionDocPath(arg)
	path := filepath.Join(string(root), filepath.FromSlash(rel))
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return ExecutionRef{}, fmt.Errorf("%w: legacy exec %q (%s)",
				statestore.ErrNotFound, arg, path)
		}
		return ExecutionRef{}, fmt.Errorf("%w: read legacy exec %q: %v",
			statestore.ErrInvalid, arg, err)
	}
	rec, err := synthesizeFromLegacy(arg, raw)
	if err != nil {
		return ExecutionRef{}, err
	}
	return ExecutionRef{
		Source:      ResolveSourceLegacyFallback,
		Execution:   rec,
		RevisionKey: rec.RevisionKey,
		Synthesized: true,
		LegacyPath:  path,
	}, nil
}

// resolveLegacyLatest scans `.orun/executions/` for the most recent
// child directory and reads its execution.json — used as the fallback
// for the empty / "latest" arg when no new-layout ref exists.
func resolveLegacyLatest(root LegacyRoot) (ExecutionRef, error) {
	dir := filepath.Join(string(root), filepath.FromSlash(statestore.LegacyExecutionsRoot()))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ExecutionRef{}, fmt.Errorf("%w: read legacy executions dir %q: %v",
			statestore.ErrNotFound, dir, err)
	}
	type cand struct {
		name string
		mod  int64
	}
	var cands []cand
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		cands = append(cands, cand{e.Name(), info.ModTime().UnixNano()})
	}
	if len(cands) == 0 {
		return ExecutionRef{}, fmt.Errorf("%w: legacy executions dir empty", statestore.ErrNotFound)
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].mod > cands[j].mod })
	return resolveLegacy(root, cands[0].name)
}

// synthesizeFromLegacy parses a legacy execution.json (whatever shape it
// happens to have — older runners wrote a free-form object) and projects
// it onto the M4 ExecutionRun model with system.migrated provenance per
// compat §4. The function is intentionally lenient: any fields it does
// not recognise are ignored, and missing fields fall back to safe
// defaults (Status="completed" if absent, Reason="migration").
func synthesizeFromLegacy(execID string, raw []byte) (ExecutionRun, error) {
	// Decode into a permissive map first — we cannot DisallowUnknownFields
	// against a schema that pre-dates this package.
	var loose map[string]any
	if err := looseJSON(raw, &loose); err != nil {
		return ExecutionRun{}, fmt.Errorf("%w: decode legacy execution.json: %v",
			statestore.ErrInvalid, err)
	}
	execKey, sErr := SanitizeExecID(execID)
	if sErr != nil {
		return ExecutionRun{}, sErr
	}
	rec := ExecutionRun{
		APIVersion:   APIVersion,
		Kind:         KindName,
		ExecutionID:  idPrefixExecution + "legacy-" + execKey,
		ExecutionKey: execKey,
		OriginalKey:  execID,
		// RevisionKey is unknown for legacy rows — compat §4 specifies
		// the synthesized record points at no revision and the
		// trigger flavor is system.migrated.
		RevisionKey: "",
		TriggerID:   "",
		TriggerKey:  string(triggerctx.SystemMigrated),
		Reason:      ReasonMigration,
		Status:      StatusCompleted,
		Attempt:     1,
		Runner: RunnerProfile{
			Mode:     "legacy",
			Backend:  "local",
			Platform: "unknown",
		},
	}
	if v, ok := loose["status"].(string); ok && v != "" {
		rec.Status = v
	}
	return rec, nil
}

// looseJSON is the permissive sibling of strictJSON: it accepts unknown
// fields. Used only by synthesizeFromLegacy.
func looseJSON(raw []byte, out any) error {
	return looseUnmarshal(raw, out)
}
