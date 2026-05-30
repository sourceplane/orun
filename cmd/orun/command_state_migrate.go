package main

// command_state_migrate.go is the M5.d hidden migration command. It walks
// the legacy `.orun/plans/<hex>.json` directory and the legacy
// `.orun/executions/<execID>/` directory and surfaces both through the
// revision-first layout introduced by the orun-state-redesign milestone.
//
// Spec source of truth: specs/orun-state-redesign/compatibility-and-migration.md
// §5. The algorithm there reads:
//
//	for each legacy plan:
//	    synthesize TriggerOccurrence (system.migrated)
//	    derive revisionKey
//	    if revision already exists with the same plan hash: skip
//	    else: write revision-first triplet via internal/revision.WriteRevision
//
//	for each legacy execution:
//	    read its plan hash (state.json.planChecksum)
//	    if a revision exists with that plan hash: attach via Bridge
//	    else: attach to rev-migrated-unknown-p<hash> (created on demand)
//	    persist a fresh execution record via executionstate.CreateExecution
//
// Idempotent. Re-running the command produces zero new files because the
// revision-key derivation is deterministic and CreateIfAbsent on the
// revision-index slot rejects duplicates.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sourceplane/orun/internal/executionstate"
	"github.com/sourceplane/orun/internal/revision"
	"github.com/sourceplane/orun/internal/state"
	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/triggerctx"
	"github.com/spf13/cobra"
)

var (
	migrateDryRun bool
)

func registerStateCommand(root *cobra.Command) {
	stateCmd := &cobra.Command{
		Use:    "state",
		Short:  "State-store maintenance commands",
		Hidden: true,
	}
	migrateCmd := &cobra.Command{
		Use:    "migrate",
		Short:  "Migrate legacy plans/executions into the revision-first layout",
		Hidden: true,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStateMigrate(cmd.Context(), cmd.OutOrStdout(), migrateDryRun)
		},
	}
	migrateCmd.Flags().BoolVar(&migrateDryRun, "dry-run", false, "Print the plan without writing anything")
	stateCmd.AddCommand(migrateCmd)
	root.AddCommand(stateCmd)
}

// migrateStats summarizes the work performed (or that would be performed
// in --dry-run) by runStateMigrate.
type migrateStats struct {
	RevisionsCreated   int
	ExecutionsAttached int
	Orphans            int
	Errors             int
}

// runStateMigrate is the command body. ctx flows from the cobra context;
// out is the destination for human-readable summary lines (cobra's stdout
// in production, a *bytes.Buffer in tests).
//
// On per-item errors the function records the failure on stats.Errors and
// continues processing — spec §5.1 explicitly calls for "exit non-zero on
// any per-item error (but continue processing remaining items)". An exit
// code of 1 is then surfaced via a non-nil error return.
func runStateMigrate(ctx context.Context, out io.Writer, dryRun bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	rootDir := storeDir()
	absStoreRoot, err := filepath.Abs(filepath.Join(rootDir, ".orun"))
	if err != nil {
		return fmt.Errorf("resolve store root: %w", err)
	}
	stateStore, err := statestore.NewLocalStore(statestore.LocalConfig{Root: absStoreRoot})
	if err != nil {
		return fmt.Errorf("open state store: %w", err)
	}
	legacyStore := state.NewStore(rootDir)

	stats := migrateStats{}

	// ---- Phase 1 — plans ------------------------------------------------
	planEntries, err := revision.ScanLegacyPlanHashes(ctx, stateStore)
	if err != nil {
		return fmt.Errorf("scan legacy plans: %w", err)
	}

	// hashToRev maps the canonical "sha256:<hex>" plan hash → revKey for
	// every revision created (or already on disk) during phase 1. Phase 2
	// uses this to attach legacy executions to the right revision without
	// re-resolving each time.
	hashToRev := make(map[string]string, len(planEntries))

	if len(planEntries) > 0 {
		fmt.Fprintln(out, "Plan migration:")
	}
	for _, entry := range planEntries {
		revKey, planHash, created, err := migratePlan(ctx, stateStore, entry, dryRun)
		if err != nil {
			stats.Errors++
			fmt.Fprintf(out, "  plans/%s.json\n    ERROR: %v\n", entry.Checksum, err)
			continue
		}
		hashToRev[planHash] = revKey
		// Also key on the legacy file stem so phase 2 can match
		// state.json's `planChecksum` field, which historically
		// stored the bare short hex (state.go.PlanChecksumShort)
		// rather than the canonical "sha256:<hex>" form.
		hashToRev[entry.Checksum] = revKey
		if created {
			stats.RevisionsCreated++
			fmt.Fprintf(out, "  plans/%s.json\n    → revision: %s (new)\n", entry.Checksum, revKey)
		} else {
			fmt.Fprintf(out, "  plans/%s.json\n    → revision: %s (exists)\n", entry.Checksum, revKey)
		}
	}

	// ---- Phase 2 — executions ------------------------------------------
	legacyExecs, err := scanLegacyExecutions(legacyStore)
	if err != nil {
		return fmt.Errorf("scan legacy executions: %w", err)
	}
	if len(legacyExecs) > 0 {
		fmt.Fprintln(out, "Execution attachment:")
	}
	for _, execEntry := range legacyExecs {
		revKey, attached, orphan, err := migrateExecution(ctx, stateStore, legacyStore, execEntry, hashToRev, dryRun)
		if err != nil {
			stats.Errors++
			fmt.Fprintf(out, "  executions/%s\n    ERROR: %v\n", execEntry.id, err)
			continue
		}
		if orphan {
			stats.Orphans++
		}
		if attached {
			stats.ExecutionsAttached++
		}
		hashLabel := execEntry.planChecksum
		if hashLabel == "" {
			hashLabel = "<missing>"
		}
		fmt.Fprintf(out, "  executions/%s\n", execEntry.id)
		fmt.Fprintf(out, "    plan hash: %s\n", hashLabel)
		fmt.Fprintf(out, "    → revision: %s", revKey)
		if orphan {
			fmt.Fprintf(out, " (orphan)")
		}
		fmt.Fprintln(out)
		fmt.Fprintf(out, "    → execution key: %s\n", execEntry.id)
	}

	// ---- Summary -------------------------------------------------------
	header := "Summary:"
	if dryRun {
		header = "Summary (dry run):"
	}
	fmt.Fprintln(out, header)
	fmt.Fprintf(out, "  revisions created:    %d\n", stats.RevisionsCreated)
	fmt.Fprintf(out, "  executions attached:  %d\n", stats.ExecutionsAttached)
	fmt.Fprintf(out, "  orphans:              %d\n", stats.Orphans)
	if stats.Errors > 0 {
		fmt.Fprintf(out, "  errors:               %d\n", stats.Errors)
		return fmt.Errorf("orun state migrate: %d per-item errors", stats.Errors)
	}
	return nil
}

// migratePlan synthesizes (or recovers) a system.migrated revision for a
// single legacy plan file. It returns:
//
//	revKey   — the deterministic revision key for this (trigger, plan).
//	planHash — the canonical "sha256:<hex>" hash of the plan bytes.
//	created  — true iff WriteRevision wrote new bytes; false iff a
//	           previous run already produced the same triplet.
//	err      — wraps statestore.ErrInvalid / ErrExists / ErrConflict only.
//
// In --dry-run mode the function still reads the plan and computes the
// derived key, but never invokes WriteRevision. created is reported as
// true when the on-disk revision-index slot is absent, mirroring what a
// real run would do.
func migratePlan(
	ctx context.Context,
	store statestore.StateStore,
	entry revision.LegacyPlanEntry,
	dryRun bool,
) (string, string, bool, error) {
	// Resolve via the legacy-hash branch (5) of the public resolver so we
	// get the same synthesized trigger / plan-hash pair the runtime uses
	// at read time. This keeps the migrate command consistent with the
	// rest of the read surface — there is exactly one place in the tree
	// that knows how to synthesize a system.migrated revision.
	ref, err := revision.ResolveRevision(ctx, store, entry.Checksum, revision.ResolveOptions{})
	if err != nil {
		return "", "", false, fmt.Errorf("resolve legacy hash %q: %w", entry.Checksum, err)
	}
	revKey := ref.Revision.RevisionKey

	// Already on disk? Reading the revision index entry distinguishes
	// "exists" from "absent" without an extra body read.
	if _, _, err := statestore.ReadRevisionIndex(ctx, store, revKey); err == nil {
		return revKey, ref.Revision.PlanHash, false, nil
	} else if !errors.Is(err, statestore.ErrNotFound) {
		return "", "", false, fmt.Errorf("read revision index %q: %w", revKey, err)
	}

	if dryRun {
		return revKey, ref.Revision.PlanHash, true, nil
	}

	cfg := revision.Config{Store: store}.WithCompatibilityWrites(false)
	rev, err := revision.WriteRevision(ctx, cfg, ref.Trigger, ref.PlanBytes, ref.Revision.PlanHash)
	if err != nil {
		// ErrExists at this point means a concurrent migrate run won
		// the race. That is success per idempotency.
		if errors.Is(err, statestore.ErrExists) {
			return revKey, ref.Revision.PlanHash, false, nil
		}
		return "", "", false, fmt.Errorf("write revision %q: %w", revKey, err)
	}
	if err := revision.WriteManifest(ctx, cfg, rev, ref.Trigger); err != nil {
		return "", "", false, fmt.Errorf("write manifest %q: %w", revKey, err)
	}
	return rev.RevisionKey, rev.PlanHash, true, nil
}

// legacyExecution captures the per-execution data we need from a legacy
// `.orun/executions/<execID>/` directory.
type legacyExecution struct {
	id            string // sanitized exec ID
	planChecksum  string // from state.json (canonical "sha256:<hex>" or bare hex)
	hasState      bool
	hasMetadata   bool
	createdAt     time.Time
}

// scanLegacyExecutions walks `.orun/executions/` and returns one entry per
// legacy execution directory with at least a state.json or metadata.json.
// The returned slice is ordered by exec ID so dry-run output is stable.
func scanLegacyExecutions(legacy *state.Store) ([]legacyExecution, error) {
	execs, err := legacy.ListExecutions()
	if err != nil {
		return nil, err
	}
	out := make([]legacyExecution, 0, len(execs))
	for _, e := range execs {
		row := legacyExecution{id: e.ID}
		// ExecEntry has no createdAt time.Time; parse StartedAt
		// best-effort. A bad parse leaves createdAt zero, which the
		// caller substitutes with time.Now().
		if t, err := time.Parse(time.RFC3339, e.StartedAt); err == nil {
			row.createdAt = t
		}
		st, _ := legacy.LoadState(e.ID) // best-effort
		if st != nil {
			row.hasState = true
			row.planChecksum = strings.TrimSpace(st.PlanChecksum)
		}
		meta, _ := legacy.LoadMetadata(e.ID)
		if meta != nil {
			row.hasMetadata = true
		}
		// Ignore directories that have neither file — the bridge has
		// nothing to mirror and the new layout would gain nothing.
		if !row.hasState && !row.hasMetadata {
			continue
		}
		out = append(out, row)
	}
	return out, nil
}

// migrateExecution attaches one legacy execution to its revision (or to a
// system.migrated-unknown revision when no plan hash is recoverable). The
// returned revKey is the destination revision; attached is true iff the
// bridge mirror succeeded; orphan is true iff we routed through the
// "unknown plan hash" path.
func migrateExecution(
	ctx context.Context,
	store statestore.StateStore,
	legacy *state.Store,
	exec legacyExecution,
	hashToRev map[string]string,
	dryRun bool,
) (string, bool, bool, error) {
	// 1 — pick the destination revision. Three cases:
	//   a) state.json had a planChecksum and phase 1 already produced a
	//      revision for that hash → attach to that revision.
	//   b) state.json had a planChecksum but phase 1 did NOT see a
	//      matching legacy plan file (e.g. plan was GC'd) → synthesize
	//      a system.migrated revision on demand against the recovered
	//      hash, treating the lack of plan bytes as the orphan signal
	//      so the dry-run output and summary line up with spec §5.3.
	//   c) state.json had no planChecksum → orphan with a deterministic
	//      placeholder hash so re-runs land on the same revision.
	var (
		revKey   string
		orphan   bool
	)
	switch {
	case exec.planChecksum != "" && lookupRevByHash(hashToRev, exec.planChecksum) != "":
		revKey = lookupRevByHash(hashToRev, exec.planChecksum)
	case exec.planChecksum != "":
		// Phase 1 didn't see this plan — derive an in-memory
		// system.migrated revision against the recovered hash and
		// (unless --dry-run) persist it so the bridge has somewhere
		// to land.
		k, err := ensureUnknownRevision(ctx, store, exec.planChecksum, dryRun)
		if err != nil {
			return "", false, false, err
		}
		revKey = k
		orphan = true
	default:
		// No planChecksum at all — use a constant placeholder hash
		// the spec example surfaces ("pdeadbeef"). The placeholder
		// is deterministic across runs so re-running migrate is
		// idempotent for orphan-grouped executions too.
		k, err := ensureUnknownRevision(ctx, store, "deadbeefdeadbeef", dryRun)
		if err != nil {
			return "", false, false, err
		}
		revKey = k
		orphan = true
	}

	if dryRun {
		return revKey, false, orphan, nil
	}

	// 2 — persist a fresh ExecutionRun under the destination revision.
	// We use the legacy exec ID as OriginalKey so the new layout's
	// execution key matches what the user typed historically.
	now := time.Now().UTC()
	if !exec.createdAt.IsZero() {
		now = exec.createdAt.UTC()
	}
	cfg := executionstate.Config{
		Store: store,
		RevisionConfig: revision.Config{Store: store},
		Now:   func() time.Time { return now },
	}
	in := executionstate.CreateExecutionInput{
		RevisionKey: revKey,
		OriginalKey: exec.id,
		Reason:      executionstate.ReasonMigration,
		Status:      executionstate.StatusCompleted,
		Runner: executionstate.RunnerProfile{
			Mode:     "migrated",
			Backend:  "legacy",
			Platform: runtime.GOOS + "/" + runtime.GOARCH,
		},
		StartedAt: &now,
	}
	// Fill RevisionID/TriggerID/TriggerKey from on-disk revision so the
	// new ExecutionRun cross-references are populated correctly. The
	// revision-index entry only carries RevisionKey + RevisionID +
	// TriggerKey + PlanHash; TriggerID lives in trigger.json, which we
	// read once via ResolveRevision against the revKey.
	if idx, _, err := statestore.ReadRevisionIndex(ctx, store, revKey); err == nil {
		in.RevisionID = idx.RevisionID
		in.TriggerKey = idx.TriggerKey
	}
	if ref, err := revision.ResolveRevision(ctx, store, revKey, revision.ResolveOptions{}); err == nil {
		in.TriggerID = ref.Trigger.TriggerID
		if in.TriggerKey == "" {
			in.TriggerKey = ref.Trigger.TriggerKey
		}
		if in.RevisionID == "" {
			in.RevisionID = ref.Revision.RevisionID
		}
	}
	_, err := executionstate.CreateExecution(ctx, cfg, in)
	if err != nil && !errors.Is(err, statestore.ErrExists) {
		return revKey, false, orphan, fmt.Errorf("create execution under %s: %w", revKey, err)
	}

	// 3 — bridge-mirror legacy state.json + metadata.json into the new
	// layout. The bridge is best-effort: missing files are silently
	// skipped (legacy execution may have only had a state.json).
	bridge := &executionstate.Bridge{
		Store:      store,
		LegacyRoot: legacy.ExecDir(),
		MirrorMode: executionstate.MirrorModeAuto,
	}
	execKey := exec.id
	if err := bridge.MirrorRunnerOutput(ctx, execKey, revKey, exec.id); err != nil {
		// MirrorRunnerOutput already swallows per-file errors and
		// emits a bridge-mirror-failed event; a non-nil return here
		// is reserved for hard validation failures. Surface it.
		return revKey, false, orphan, fmt.Errorf("mirror legacy exec %q: %w", exec.id, err)
	}
	return revKey, true, orphan, nil
}

// lookupRevByHash returns the revKey associated with planChecksum (in
// either bare or "sha256:" form) from the phase-1 hashToRev map, or "" if
// none matched.
func lookupRevByHash(m map[string]string, planChecksum string) string {
	if v, ok := m[planChecksum]; ok {
		return v
	}
	bare := strings.TrimPrefix(planChecksum, "sha256:")
	if v, ok := m["sha256:"+bare]; ok {
		return v
	}
	if v, ok := m[bare]; ok {
		return v
	}
	return ""
}

// ensureUnknownRevision synthesizes a system.migrated revision for a hash
// whose plan bytes are not on disk. The synthesized revision uses an
// empty plan-bytes placeholder of the form `{"migrated":true}\n` so the
// revision triplet is well-formed even though the original plan content
// is unrecoverable.
//
// In --dry-run mode no bytes are written; the function still returns the
// deterministic revision key the real call would have used.
func ensureUnknownRevision(ctx context.Context, store statestore.StateStore, planHash string, dryRun bool) (string, error) {
	now := time.Now().UTC()
	trig := triggerctx.NewSystemMigrated(triggerctx.SystemOptions{Now: now})
	revKey, err := revision.RevisionKey(trig, planHash)
	if err != nil {
		return "", fmt.Errorf("derive unknown revKey: %w", err)
	}
	if _, _, err := statestore.ReadRevisionIndex(ctx, store, revKey); err == nil {
		return revKey, nil
	} else if !errors.Is(err, statestore.ErrNotFound) {
		return "", fmt.Errorf("read unknown rev index %q: %w", revKey, err)
	}
	if dryRun {
		return revKey, nil
	}
	// Placeholder plan body — strict JSON, satisfies the path policy. The
	// content is not byte-identical to any real plan (no metadata.checksum)
	// so a future legitimate write of the real plan to this revision dir
	// would be a programmer error and is rejected by CreateIfAbsent on
	// re-run.
	planBytes, _ := json.Marshal(map[string]any{
		"migrated":           true,
		"planHash":           planHash,
		"reason":             "legacy execution attached without plan bytes",
	})
	planBytes = append(planBytes, '\n')
	cfg := revision.Config{Store: store}.WithCompatibilityWrites(false)
	rev, err := revision.WriteRevision(ctx, cfg, trig, planBytes, planHash)
	if err != nil {
		if errors.Is(err, statestore.ErrExists) {
			return revKey, nil
		}
		return "", fmt.Errorf("write unknown revision %q: %w", revKey, err)
	}
	if err := revision.WriteManifest(ctx, cfg, rev, trig); err != nil {
		return "", fmt.Errorf("write unknown manifest %q: %w", revKey, err)
	}
	return rev.RevisionKey, nil
}
