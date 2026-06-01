package executionstate

import (
	"context"
	"errors"
	"fmt"
	"path"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/revision"
	"github.com/sourceplane/orun/internal/statestore"
)

// claimRetryBudget bounds the number of CreateIfAbsent attempts a single
// CreateExecution call makes when the next-key claim races against another
// concurrent writer. The loser-retries-from-Read pattern (state-store.md
// §6) re-derives NextExecutionKey on each loss; 1024 attempts gives
// comfortable headroom up through a steady-state run-1024 collision while
// still bounding pathological live-locks.
const claimRetryBudget = 1024

// casRetryBudget bounds the read-modify-CAS loops on execution.json (used
// by MarkTerminal) and the manifest update. Mirrors the budget in the
// revision package so concurrency behavior is symmetric across packages.
const casRetryBudget = 5

// runKeyPattern matches the canonical NextExecutionKey output: "run-NNN"
// with at least 3 zero-padded decimal digits. The pattern is also used by
// scanForNextRunSeq to recover the highest sequence number under a
// revision's executions/ directory.
var runKeyPattern = regexp.MustCompile(`^run-(\d{3,})$`)

// runKeyDigitWidth is the minimum padding width applied by NextExecutionKey
// when rendering the next sequence. Wider sequences (≥ 1000) overflow the
// pad naturally — strconv.FormatInt is unconditional.
const runKeyDigitWidth = 3

// sanitizeExecIDMaxLen caps the length of a sanitized exec id. The cap is
// generous enough to preserve human-readable names while still fitting
// inside the path-component policy and giving CreateIfAbsent room for any
// future "-x<n>" disambiguation suffix without overflowing typical
// filesystem name limits.
const sanitizeExecIDMaxLen = 64

// Config configures execution-state writers. Zero values for Now / NewID
// receive sensible defaults (UTC time.Now and oklog/ulid/v2). The Config
// shape mirrors revision.Config so M5 can use a single value across both
// packages.
type Config struct {
	// Store is the StateStore the writer composes against. Required.
	Store statestore.StateStore

	// RevisionConfig is forwarded to revision.UpdateLatestExecutionSummary
	// from CreateExecution / MarkTerminal. RevisionConfig.Store, when
	// nil, is filled with Store; the indirection lets tests inject a
	// distinct mock if they want. Now/NewID propagate as-is.
	RevisionConfig revision.Config

	// Now stamps CreatedAt / StartedAt / FinishedAt on every persisted
	// artifact. When nil, time.Now().UTC is used. Mirrors revision.Config.Now.
	Now func() time.Time

	// NewID generates the ExecutionID written into execution.json
	// (data-model.md §5). When nil, a monotonic ULID prefixed "exec_"
	// is generated.
	NewID func() string

	// CatalogParent, when both keys are non-empty, requests that
	// CreateExecution/MarkTerminal additionally mirror execution.json
	// under the catalog-parent layout
	// sources/<SourceKey>/catalogs/<CatalogKey>/revisions/<revKey>/executions/<execKey>/
	// per design.md §7 / implementation-plan.md C7. The Phase 1 global-layout
	// writes are emitted unconditionally. When either key is empty the
	// catalog-parent mirror is skipped and only the Phase 1 layout is written.
	CatalogParent revision.CatalogParentRef
}

// resolveDefaults returns a Config copy with nil functions filled in.
// Mirrors revision.Config.resolveDefaults so behaviour stays in lockstep.
func (c Config) resolveDefaults() Config {
	out := c
	if out.Now == nil {
		out.Now = func() time.Time { return time.Now().UTC() }
	}
	if out.NewID == nil {
		out.NewID = func() string {
			return idPrefixExecution + ulid.Make().String()
		}
	}
	if out.RevisionConfig.Store == nil {
		out.RevisionConfig.Store = out.Store
	}
	if out.RevisionConfig.Now == nil {
		out.RevisionConfig.Now = out.Now
	}
	return out
}

// SanitizeExecID projects an arbitrary user-supplied execution-id string
// onto the path-component alphabet `[a-z0-9._-]` (state-store.md §2)
// while also enforcing the tighter `[a-z0-9-]` shape expected by
// run-NNN-style keys (data-model.md §5 / test-plan.md §3.3). The
// projection rules are deterministic:
//
//  1. Lowercase ASCII letters and digits are kept verbatim.
//  2. ASCII '-' is kept verbatim. '_' and '.' are mapped to '-' so the
//     output stays inside the tighter alphabet test-plan.md §3.3
//     specifies.
//  3. Every other rune is replaced with '-'.
//  4. Runs of consecutive '-' collapse to a single '-'.
//  5. Leading and trailing '-' are stripped.
//  6. The result is truncated to sanitizeExecIDMaxLen runes.
//
// On empty/all-disallowed input the function returns ("", error) wrapping
// statestore.ErrInvalid so the caller surfaces a sentinel-routed failure
// rather than producing an unparseable key.
func SanitizeExecID(in string) (string, error) {
	var b strings.Builder
	b.Grow(len(in))
	for _, r := range in {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r + ('a' - 'A'))
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteByte('-')
		default:
			b.WriteByte('-')
		}
	}
	out := collapseDashes(b.String())
	out = strings.Trim(out, "-")
	if len(out) > sanitizeExecIDMaxLen {
		out = strings.TrimRight(out[:sanitizeExecIDMaxLen], "-")
	}
	if out == "" {
		return "", fmt.Errorf("%w: exec id %q has no representable characters", statestore.ErrInvalid, in)
	}
	if err := statestore.ValidateComponent(out); err != nil {
		return "", err
	}
	return out, nil
}

// collapseDashes returns s with runs of '-' folded to a single '-'.
func collapseDashes(s string) string {
	if !strings.Contains(s, "--") {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	prevDash := false
	for _, r := range s {
		if r == '-' {
			if prevDash {
				continue
			}
			prevDash = true
		} else {
			prevDash = false
		}
		b.WriteRune(r)
	}
	return b.String()
}

// NextExecutionKey returns the next "run-NNN" execution key under
// revKey by scanning the executions/ directory and incrementing the
// highest existing sequence by one. New revisions return "run-001"
// (data-model.md §5 default).
//
// The scan is List-based: each known execution key contributes a single
// candidate via the runKeyPattern regex; non-matching keys (e.g.
// caller-supplied --exec-id values) are ignored so they do not perturb
// the monotonic sequence.
//
// The function does NOT claim a slot; CreateExecution wraps it in a
// CreateIfAbsent loop and re-derives on conflict so concurrent writers
// converge on distinct keys per test-plan.md §3.3.
func NextExecutionKey(ctx context.Context, store statestore.StateStore, revKey string) (string, error) {
	if store == nil {
		return "", fmt.Errorf("%w: NextExecutionKey store is nil", statestore.ErrInvalid)
	}
	if err := revision.ValidateRevisionKey(revKey); err != nil {
		return "", err
	}
	seq, err := scanForNextRunSeq(ctx, store, revKey)
	if err != nil {
		return "", err
	}
	return formatRunKey(seq), nil
}

// scanForNextRunSeq returns the next run-N sequence integer for revKey.
// Sequence 1 is returned for fresh revisions (no executions yet).
func scanForNextRunSeq(ctx context.Context, store statestore.StateStore, revKey string) (int, error) {
	prefix := statestore.ExecutionsDir(revKey)
	infos, err := store.List(ctx, prefix)
	if err != nil {
		return 0, fmt.Errorf("list executions under %s: %w", prefix, err)
	}
	max := 0
	for _, info := range infos {
		key := executionKeyFromPath(prefix, info.Path)
		if key == "" {
			continue
		}
		m := runKeyPattern.FindStringSubmatch(key)
		if m == nil {
			continue
		}
		n, err := strconv.Atoi(m[1])
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return max + 1, nil
}

// executionKeyFromPath extracts the immediate execKey segment from a
// listed object path under prefix ("revisions/<rev>/executions"). Returns
// "" for paths that do not lie under prefix or that have no segment after
// it.
func executionKeyFromPath(prefix, p string) string {
	if !strings.HasPrefix(p, prefix+"/") {
		return ""
	}
	rest := strings.TrimPrefix(p, prefix+"/")
	if rest == "" {
		return ""
	}
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// formatRunKey renders a run-NNN execution key with at least
// runKeyDigitWidth zero-padded digits.
func formatRunKey(n int) string {
	return fmt.Sprintf("run-%0*d", runKeyDigitWidth, n)
}

// CreateExecutionInput is the per-call input to CreateExecution. The
// caller-supplied OriginalKey is sanitized iff non-empty and used as the
// execution key directly; otherwise the writer derives the next "run-NNN"
// monotonic key. Reason / Runner / Summary are written verbatim. Attempt
// defaults to 1 when zero so callers don't need to remember the special
// case.
type CreateExecutionInput struct {
	RevisionKey string
	RevisionID  string
	TriggerID   string
	TriggerKey  string

	// OriginalKey, if non-empty, is the user-supplied --exec-id. It is
	// sanitized via SanitizeExecID and used as the execution key. The
	// raw input is preserved on ExecutionRun.OriginalKey for audit
	// (data-model.md §5).
	OriginalKey string

	Reason  string
	Status  string
	Attempt int
	Runner  RunnerProfile
	Summary ExecSummary

	// StartedAt is optional. CreateExecution honors a non-nil pointer
	// verbatim so callers that move directly to "running" can stamp the
	// start time at creation; nil leaves the field absent (data-model.md
	// §5 omitempty).
	StartedAt *time.Time
}

// CreateExecution persists a fresh execution record under revKey. It:
//
//  1. Resolves the execution key (sanitized OriginalKey or NextExecutionKey).
//  2. Claims revisions/<revKey>/executions/<execKey>/execution.json via
//     CreateIfAbsent. On ErrExists with a derived key, re-derives the
//     next sequence and tries again, bounded by claimRetryBudget. On
//     ErrExists with a caller-supplied key, surfaces the sentinel verbatim.
//  3. Writes the execution-index entry (idempotent — CreateIfAbsent;
//     ErrExists is success on retry).
//  4. Updates refs/latest-execution.json via Write (last-write-wins, per
//     data-model.md §6.2).
//  5. Appends the execution-created event under
//     executions/<execKey>/events/00000000000000000001-execution-created.json
//     via CreateIfAbsent (data-model.md §9).
//  6. Calls revision.UpdateLatestExecutionSummary so the manifest's
//     latestExecutionKey/Status fields track the new execution.
//
// Failures wrap statestore sentinels via fmt.Errorf — no new sentinels.
//
// Returns the persisted ExecutionRun so the caller does not have to
// re-read execution.json.
func CreateExecution(ctx context.Context, cfg Config, in CreateExecutionInput) (ExecutionRun, error) {
	if cfg.Store == nil {
		return ExecutionRun{}, fmt.Errorf("%w: executionstate.Config.Store is nil", statestore.ErrInvalid)
	}
	if err := validateInput(in); err != nil {
		return ExecutionRun{}, err
	}
	cfg = cfg.resolveDefaults()
	now := cfg.Now().UTC()
	store := cfg.Store

	// Step 1 — derive the execution key.
	derived := in.OriginalKey == ""
	var execKey string
	if !derived {
		k, err := SanitizeExecID(in.OriginalKey)
		if err != nil {
			return ExecutionRun{}, err
		}
		execKey = k
	}

	// Step 2 — claim the slot via CreateIfAbsent. On ErrExists with a
	// derived key, re-derive the next sequence and retry; with a
	// caller-supplied key, surface ErrExists verbatim — re-deriving
	// would change the user-visible key, which the spec disallows.
	attempt := in.Attempt
	if attempt == 0 {
		attempt = 1
	}
	for i := 0; i < claimRetryBudget; i++ {
		if derived {
			k, err := NextExecutionKey(ctx, store, in.RevisionKey)
			if err != nil {
				return ExecutionRun{}, err
			}
			execKey = k
		}

		rec := ExecutionRun{
			APIVersion:   APIVersion,
			Kind:         KindName,
			ExecutionID:  cfg.NewID(),
			ExecutionKey: execKey,
			OriginalKey:  in.OriginalKey,
			RevisionID:   in.RevisionID,
			RevisionKey:  in.RevisionKey,
			TriggerID:    in.TriggerID,
			TriggerKey:   in.TriggerKey,
			Reason:       in.Reason,
			Status:       in.Status,
			Attempt:      attempt,
			Runner:       in.Runner,
			Summary:      in.Summary,
			CreatedAt:    now,
			StartedAt:    in.StartedAt,
		}
		_, err := store.CreateIfAbsent(ctx,
			statestore.ExecutionDocPath(in.RevisionKey, execKey),
			marshalCanonicalJSON(rec))
		if err == nil {
			if err := finalizeExecution(ctx, cfg, rec, now); err != nil {
				return ExecutionRun{}, err
			}
			return rec, nil
		}
		if errors.Is(err, statestore.ErrExists) {
			if !derived {
				return ExecutionRun{}, fmt.Errorf("claim execution.json: %w", err)
			}
			continue
		}
		return ExecutionRun{}, fmt.Errorf("claim execution.json: %w", err)
	}
	return ExecutionRun{}, fmt.Errorf("%w: execution-key claim retry budget (%d) exhausted",
		statestore.ErrConflict, claimRetryBudget)
}

// finalizeExecution runs steps 3–6 of CreateExecution against rec. Pulled
// out so the post-claim flow is testable without re-deriving keys.
func finalizeExecution(ctx context.Context, cfg Config, rec ExecutionRun, now time.Time) error {
	store := cfg.Store

	// Step 3 — execution-index entry. Re-claiming an existing entry
	// (idempotent rerun under the same exec key) is treated as success.
	idx := statestore.ExecutionIndexEntry{
		ExecutionKey: rec.ExecutionKey,
		ExecutionID:  rec.ExecutionID,
		RevisionKey:  rec.RevisionKey,
		Status:       rec.Status,
		CreatedAt:    now,
		Path:         statestore.ExecutionDir(rec.RevisionKey, rec.ExecutionKey),
	}
	if cfg.CatalogParent.Active() {
		p, err := catalogstore.CatalogExecutionDir(cfg.CatalogParent.SourceKey, cfg.CatalogParent.CatalogKey, rec.RevisionKey, rec.ExecutionKey)
		if err != nil {
			return fmt.Errorf("catalog-parent execution index path: %w", err)
		}
		idx.Path = p
	}
	if _, err := statestore.WriteExecutionIndex(ctx, store, idx); err != nil &&
		!errors.Is(err, statestore.ErrExists) {
		return fmt.Errorf("write execution index: %w", err)
	}

	// Step 4 — refs/latest-execution.json. Last-write-wins per
	// data-model.md §6.2 (callers needing CAS use CASLatestExecutionRef
	// directly).
	if _, err := statestore.WriteLatestExecutionRef(ctx, store, statestore.LatestExecutionRef{
		RevisionKey:  rec.RevisionKey,
		ExecutionKey: rec.ExecutionKey,
		ExecutionID:  rec.ExecutionID,
		Status:       rec.Status,
		CreatedAt:    now,
	}); err != nil {
		return fmt.Errorf("write latest-execution ref: %w", err)
	}

	// Step 5 — execution-created event. Sequence 1 is reserved for the
	// creation event; subsequent runner-emitted events start at 2+.
	evt := executionCreatedEvent{
		Kind: "execution-created",
		At:   now,
		Payload: executionCreatedPayload{
			ExecutionKey: rec.ExecutionKey,
			ExecutionID:  rec.ExecutionID,
			RevisionKey:  rec.RevisionKey,
			Reason:       rec.Reason,
			Status:       rec.Status,
		},
	}
	evtPath := statestore.EventPath(rec.RevisionKey, rec.ExecutionKey, 1, "execution-created")
	if _, err := store.CreateIfAbsent(ctx, evtPath, marshalCanonicalJSON(evt)); err != nil &&
		!errors.Is(err, statestore.ErrExists) {
		return fmt.Errorf("write execution-created event: %w", err)
	}

	// Step 6 — manifest's latestExecutionSummary. The helper
	// short-circuits on byte-equality so an idempotent re-call is free.
	if err := updateRevisionSummary(ctx, cfg, rec); err != nil {
		// ErrNotFound from the manifest read means M3 callers wrote
		// the revision but did not call WriteManifest — the writer's
		// loud surfacing here exposes that gap rather than hiding it.
		return fmt.Errorf("update revision manifest: %w", err)
	}

	// Step 7 — catalog-parent execution mirror (C7). Additive: only
	// runs when the caller resolved a (source, catalog) pair. Phase 1
	// layout above is unaffected.
	if cfg.CatalogParent.Active() {
		if err := writeCatalogParentExecution(ctx, store, cfg.CatalogParent, rec); err != nil {
			return fmt.Errorf("write catalog-parent execution: %w", err)
		}
	}

	return nil
}

// updateRevisionSummary calls revision.UpdateLatestExecutionSummary with
// rec's projected summary. Pulled out for direct testing of the
// CAS-conflict-then-retry path.
func updateRevisionSummary(ctx context.Context, cfg Config, rec ExecutionRun) error {
	return revision.UpdateLatestExecutionSummary(ctx, cfg.RevisionConfig, rec.RevisionKey,
		revision.LatestExecutionSummary{
			Key:    rec.ExecutionKey,
			Status: rec.Status,
		})
}

// UpdateSnapshot writes revisions/<revKey>/executions/<execKey>/snapshot.latest.json
// (data-model.md §5.1). Snapshots are point-in-time projections of the
// execution row used by `orun status --watch`; they are overwritten
// unconditionally via store.Write so concurrent runner ticks observe the
// latest tick (data-model.md §5.1, §6 atomicity).
func UpdateSnapshot(ctx context.Context, cfg Config, snapshot ExecutionRun) error {
	if cfg.Store == nil {
		return fmt.Errorf("%w: executionstate.Config.Store is nil", statestore.ErrInvalid)
	}
	if err := revision.ValidateRevisionKey(snapshot.RevisionKey); err != nil {
		return err
	}
	if err := statestore.ValidateComponent(snapshot.ExecutionKey); err != nil {
		return err
	}
	cfg = cfg.resolveDefaults()
	if _, err := cfg.Store.Write(ctx,
		statestore.SnapshotPath(snapshot.RevisionKey, snapshot.ExecutionKey),
		marshalCanonicalJSON(snapshot), statestore.WriteOptions{}); err != nil {
		return fmt.Errorf("write snapshot.latest.json: %w", err)
	}
	return nil
}

// MarkTerminal flips an existing execution to a terminal status, stamps
// FinishedAt, and refreshes the revision manifest's latestExecutionSummary.
// The mutation is read-modify-CAS on execution.json (state-store.md §6 —
// caller-owned retry, bounded by casRetryBudget). Calling MarkTerminal
// twice with the same status is idempotent — CAS short-circuits when the
// next bytes equal the current bytes.
//
// The summary update is invoked after the CAS lands so the manifest never
// reflects a status the execution.json has not yet committed.
//
// Returns the updated ExecutionRun.
func MarkTerminal(
	ctx context.Context,
	cfg Config,
	revKey, execKey, status string,
	summary ExecSummary,
) (ExecutionRun, error) {
	if cfg.Store == nil {
		return ExecutionRun{}, fmt.Errorf("%w: executionstate.Config.Store is nil", statestore.ErrInvalid)
	}
	if !IsTerminal(status) {
		return ExecutionRun{}, fmt.Errorf("%w: status %q is not terminal", statestore.ErrInvalid, status)
	}
	if err := revision.ValidateRevisionKey(revKey); err != nil {
		return ExecutionRun{}, err
	}
	if err := statestore.ValidateComponent(execKey); err != nil {
		return ExecutionRun{}, err
	}
	cfg = cfg.resolveDefaults()
	docPath := statestore.ExecutionDocPath(revKey, execKey)
	store := cfg.Store

	for attempt := 0; attempt < casRetryBudget; attempt++ {
		raw, meta, err := store.Read(ctx, docPath)
		if err != nil {
			return ExecutionRun{}, fmt.Errorf("read execution.json: %w", err)
		}
		var current ExecutionRun
		if err := strictJSON(raw, &current); err != nil {
			return ExecutionRun{}, fmt.Errorf("decode execution.json: %w", err)
		}
		next := current
		next.Status = status
		next.Summary = summary
		now := cfg.Now().UTC()
		if next.FinishedAt == nil {
			finished := now
			next.FinishedAt = &finished
		}
		nextBytes := marshalCanonicalJSON(next)
		if equalBytes(raw, nextBytes) {
			// Already terminal at the same status — mirror the
			// manifest update so a crash-and-replay still
			// converges the manifest field even when the
			// execution.json bytes are pinned.
			if err := updateRevisionSummary(ctx, cfg, next); err != nil {
				return ExecutionRun{}, fmt.Errorf("update revision manifest: %w", err)
			}
			return next, nil
		}
		_, err = store.CompareAndSwap(ctx, docPath, meta.Revision, nextBytes)
		if err == nil {
			if err := updateRevisionSummary(ctx, cfg, next); err != nil {
				return ExecutionRun{}, fmt.Errorf("update revision manifest: %w", err)
			}
			// Mirror the new status into refs/latest-execution.json
			// so cli-surface §3.1 callers see the terminal status
			// without a manifest read. Last-write-wins is correct
			// here — readers fall back to the execution row on
			// refresh.
			if _, err := statestore.WriteLatestExecutionRef(ctx, store,
				statestore.LatestExecutionRef{
					RevisionKey:  next.RevisionKey,
					ExecutionKey: next.ExecutionKey,
					ExecutionID:  next.ExecutionID,
					Status:       next.Status,
					CreatedAt:    now,
				}); err != nil {
				return ExecutionRun{}, fmt.Errorf("refresh latest-execution ref: %w", err)
			}
			// Catalog-parent execution mirror (C7). Overwrite the
			// catalog-parent copy with the terminal status so both
			// layouts stay byte-identical.
			if cfg.CatalogParent.Active() {
				if err := writeCatalogParentExecution(ctx, store, cfg.CatalogParent, next); err != nil {
					return ExecutionRun{}, fmt.Errorf("write catalog-parent execution: %w", err)
				}
			}
			return next, nil
		}
		if errors.Is(err, statestore.ErrConflict) {
			continue
		}
		return ExecutionRun{}, fmt.Errorf("cas execution.json: %w", err)
	}
	return ExecutionRun{}, fmt.Errorf("%w: execution.json CAS retry budget (%d) exhausted",
		statestore.ErrConflict, casRetryBudget)
}

// validateInput enforces the per-task constraint that CreateExecution
// receives a usable input. We refuse to invent values for the caller —
// the M3 writer takes the same conservative-on-unknowns posture (see
// validateTrigger in revision/writer.go).
func validateInput(in CreateExecutionInput) error {
	if err := revision.ValidateRevisionKey(in.RevisionKey); err != nil {
		return err
	}
	if in.RevisionID == "" {
		return fmt.Errorf("%w: missing input field RevisionID", statestore.ErrInvalid)
	}
	if in.TriggerID == "" {
		return fmt.Errorf("%w: missing input field TriggerID", statestore.ErrInvalid)
	}
	if in.TriggerKey == "" {
		return fmt.Errorf("%w: missing input field TriggerKey", statestore.ErrInvalid)
	}
	if in.Reason == "" {
		return fmt.Errorf("%w: missing input field Reason", statestore.ErrInvalid)
	}
	if in.Status == "" {
		return fmt.Errorf("%w: missing input field Status", statestore.ErrInvalid)
	}
	return nil
}

// executionCreatedEvent + payload encode the data-model.md §9 event log
// entry. The shape is internal to this package; CLI/UX consumers read it
// generically as JSON so adding fields is non-breaking.
type executionCreatedEvent struct {
	Kind    string                  `json:"kind"`
	At      time.Time               `json:"at"`
	Payload executionCreatedPayload `json:"payload"`
}

type executionCreatedPayload struct {
	ExecutionKey string `json:"executionKey"`
	ExecutionID  string `json:"executionId"`
	RevisionKey  string `json:"revisionKey"`
	Reason       string `json:"reason"`
	Status       string `json:"status"`
}

// listExecutionKeys returns the immediate-child execution keys under
// revKey, sorted ascending. Used by ResolveExecution's prefix scan.
func listExecutionKeys(ctx context.Context, store statestore.StateStore, revKey string) ([]string, error) {
	prefix := statestore.ExecutionsDir(revKey)
	infos, err := store.List(ctx, prefix)
	if err != nil {
		return nil, fmt.Errorf("list executions under %s: %w", prefix, err)
	}
	seen := map[string]struct{}{}
	for _, info := range infos {
		k := executionKeyFromPath(prefix, info.Path)
		if k == "" {
			continue
		}
		seen[k] = struct{}{}
	}
	out := make([]string, 0, len(seen))
	for k := range seen {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// pathBase exists so callers do not import path/filepath just to pluck
// the trailing segment of a logical path. Kept tiny and local.
func pathBase(p string) string { return path.Base(p) }

// writeCatalogParentExecution mirrors execution.json under the catalog-parent
// layout: sources/<srcKey>/catalogs/<catKey>/revisions/<revKey>/executions/<execKey>/execution.json.
// Pattern mirrors writeCatalogParentRevision in internal/revision/catalog_parent.go.
func writeCatalogParentExecution(
	ctx context.Context,
	store statestore.StateStore,
	parent revision.CatalogParentRef,
	rec ExecutionRun,
) error {
	docPath, err := catalogstore.CatalogExecutionDocPath(parent.SourceKey, parent.CatalogKey, rec.RevisionKey, rec.ExecutionKey)
	if err != nil {
		return fmt.Errorf("catalog-parent execution path: %w", err)
	}
	if _, err := store.Write(ctx, docPath, marshalCanonicalJSON(rec), statestore.WriteOptions{}); err != nil {
		return fmt.Errorf("write catalog-parent execution.json: %w", err)
	}
	return nil
}
