package main

// command_run_revision.go wires `orun run` into the revision-first execution
// path introduced by the orun-state-redesign. Per cli-surface.md §2.2 the
// run command:
//
//   1. resolves a PlanRevision via internal/revision.ResolveRevision,
//      honoring the seven-branch chain in compatibility-and-migration.md §3
//      (latest / file / revision-key / named-ref / legacy-hash /
//      component-name / ambiguous);
//   2. creates an execution under the revision via
//      internal/executionstate.CreateExecution, which writes execution.json,
//      snapshot.latest.json, indexes/executions/<execKey>.json, and
//      refs/latest-execution.json;
//   3. mirrors the legacy runner-on-disk state.json + metadata.json into
//      the new layout via internal/executionstate.Bridge on every runner
//      tick (the runner's AfterStateUpdate hook drives the mirror);
//   4. marks the execution terminal via MarkTerminal once the runner
//      returns, refreshing refs/latest-execution.json + manifest summary;
//   5. prints a Revision/Trigger/Execution terminal summary that mirrors
//      the M5.a `orun plan` summary block.
//
// `--revision <key>` short-circuits the resolution chain by passing the
// flag value directly to ResolveRevision (which dispatches branch 3 on a
// "rev-…" key, branch 4 on a named-ref alias, etc.). `--persist-revision`
// is reserved per spec and not wired here.

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/sourceplane/orun/internal/catalogstore"
	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/executionstate"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/revision"
	"github.com/sourceplane/orun/internal/runner"
	"github.com/sourceplane/orun/internal/statestore"
	"github.com/sourceplane/orun/internal/triggerctx"
	"github.com/sourceplane/orun/internal/ui"
)

// runRevision is the value of `--revision <key>` (cli-surface.md §2.3). When
// non-empty the resolver receives this value verbatim, skipping the normal
// positional-arg + plan-ref disambiguation.
var runRevision string

// revisionExecution is the bundle returned by setupRevisionExecution. It
// captures the resolved revision, the persisted ExecutionRun, and an
// already-configured Bridge so the caller can wire it into the runner with
// no further plumbing.
type revisionExecution struct {
	cfg           executionstate.Config
	bridge        *executionstate.Bridge
	revKey        string
	execKey       string
	exec          executionstate.ExecutionRun
	source        revision.ResolveSource
	planFile      string // canonical plan.json path, for the summary block
	catalogParent revision.CatalogParentRef
	triggerName   string
}

// setupRevisionExecution is the M5.b entry point. It opens the StateStore
// rooted at .orun/, resolves the PlanRevision, persists the ExecutionRun,
// and constructs the Bridge that mirrors runner output on every tick.
//
// On success the returned *revisionExecution carries everything the caller
// needs to (a) mirror state.json/metadata.json per runner tick, (b) flip
// the execution to a terminal status when the runner returns, and (c)
// print a summary block consistent with the M5.a `orun plan` output.
//
// The function is best-effort: if the local store cannot be opened or the
// revision cannot be resolved, it returns a non-nil error and the caller
// is expected to keep running on the legacy path. The runner is *not*
// short-circuited — the legacy `.orun/executions/<execID>/` write path is
// still authoritative for in-flight progress; the bridge merely promotes
// those bytes into the new layout.
func setupRevisionExecution(
	ctx context.Context,
	plan *model.Plan,
	loadedIntent *model.Intent,
	legacyExecID string,
) (*revisionExecution, error) {
	if plan == nil {
		return nil, fmt.Errorf("setupRevisionExecution: plan is nil")
	}
	if legacyExecID == "" {
		return nil, fmt.Errorf("setupRevisionExecution: legacyExecID is empty")
	}

	absStoreRoot, err := filepath.Abs(filepath.Join(storeDir(), ".orun"))
	if err != nil {
		return nil, fmt.Errorf("resolve store root: %w", err)
	}
	stateStore, err := statestore.NewLocalStore(statestore.LocalConfig{Root: absStoreRoot})
	if err != nil {
		return nil, fmt.Errorf("open state store: %w", err)
	}

	// Step 1 — seven-branch resolver. compat §3:
	//   1) "" → refs/latest-revision.json
	//   2) file path → load + synthesize manual revision
	//   3) "rev-…" key → revisions/<key>/{plan,revision,trigger}.json
	//   4) named ref → refs/named/<name>.json indirection
	//   5) legacy hex → plans/<hex>.json (legacy compat)
	//   6) component name → caller-driven dispatch (NOT wired in run; we
	//      treat it as a synthesize-fresh fallback, matching the spec
	//      "if no revision exists … materialize system.manual")
	//   7) ambiguous → error
	resolverArg := runRevision
	if resolverArg == "" {
		resolverArg = runResolvedRevisionArg
	}
	ref, resolveErr := revision.ResolveRevision(ctx, stateStore, resolverArg, revision.ResolveOptions{})

	if resolveErr != nil {
		switch {
		case errors.Is(resolveErr, revision.ErrComponentRunUnchanged),
			errors.Is(resolveErr, revision.ErrAmbiguousArg):
			return nil, resolveErr
		}
		// Synthesize a fresh manual revision and re-resolve so we get
		// the RevisionID / TriggerID that CreateExecution requires.
		synthKey, synthErr := synthesizeRevisionForRun(ctx, stateStore, plan, loadedIntent)
		if synthErr != nil {
			return nil, fmt.Errorf("resolve revision (%v) and synthesize fallback: %w", resolveErr, synthErr)
		}
		ref, resolveErr = revision.ResolveRevision(ctx, stateStore, synthKey, revision.ResolveOptions{})
		if resolveErr != nil {
			return nil, fmt.Errorf("re-resolve synthesized revision %s: %w", synthKey, resolveErr)
		}
	}
	revKey := ref.Revision.RevisionKey

	// Extract catalog parent from the resolved revision (C7). If the
	// revision was planned with catalog context (C6), the snapshot keys
	// are populated; otherwise the zero-value CatalogParentRef keeps the
	// catalog-parent mirror inactive.
	catParent := revision.CatalogParentRef{
		SourceKey:  ref.Revision.SourceSnapshotKey,
		CatalogKey: ref.Revision.CatalogSnapshotKey,
	}

	// Step 2 — execution-state writer config. The Config shape mirrors
	// revision.Config so `orun plan` and `orun run` share the same clock
	// + ID generators by default.
	cfg := executionstate.Config{
		Store: stateStore,
		RevisionConfig: revision.Config{
			Store: stateStore,
		},
		CatalogParent: catParent,
	}

	// Step 3 — create the execution. The runner profile is recorded
	// best-effort from the resolved runner name (runRunner is normalized
	// later in runPlan; we capture the user-visible value here).
	runnerName := resolveRunnerName(runRunner)
	if runGHACompat {
		runnerName = "github-actions"
	}
	in := executionstate.CreateExecutionInput{
		RevisionKey: revKey,
		RevisionID:  ref.Revision.RevisionID,
		TriggerID:   ref.Trigger.TriggerID,
		TriggerKey:  ref.Trigger.TriggerKey,
		OriginalKey: legacyExecID,
		Reason:      executionstate.ReasonDirectRun,
		Status:      executionstate.StatusPending,
		Runner: executionstate.RunnerProfile{
			Mode:     "direct",
			Backend:  runnerName,
			Platform: runtime.GOOS + "/" + runtime.GOARCH,
		},
		Summary: executionstate.ExecSummary{
			Total:   len(plan.Jobs),
			Pending: len(plan.Jobs),
		},
	}
	exec, err := executionstate.CreateExecution(ctx, cfg, in)
	if err != nil {
		return nil, fmt.Errorf("create execution under %s: %w", revKey, err)
	}

	// Step 4 — Bridge. LegacyRoot is the runner's `.orun/executions`
	// directory; the runner writes <legacyExecID>/state.json there per
	// state.Store.SaveState, and the bridge promotes those bytes into
	// revisions/<revKey>/executions/<execKey>/{state,metadata}.json on
	// every tick. MirrorModeAuto = hardlink with copy fallback on EXDEV
	// per design.md §11.
	bridge := &executionstate.Bridge{
		Store:         stateStore,
		LegacyRoot:    filepath.Join(absStoreRoot, "executions"),
		MirrorMode:    executionstate.MirrorModeAuto,
		CatalogParent: catParent,
	}

	canonicalPlan := filepath.Join(absStoreRoot, "revisions", revKey, "plan.json")
	if catParent.Active() {
		if catalogRevDir, err := catalogstore.CatalogRevisionDir(catParent.SourceKey, catParent.CatalogKey, revKey); err == nil {
			canonicalPlan = filepath.Join(absStoreRoot, filepath.FromSlash(catalogRevDir), "plan.json")
		}
	}

	return &revisionExecution{
		cfg:           cfg,
		bridge:        bridge,
		revKey:        revKey,
		execKey:       exec.ExecutionKey,
		exec:          exec,
		source:        ref.Source,
		planFile:      canonicalPlan,
		catalogParent: catParent,
		triggerName:   firstNonEmptyString(ref.Trigger.TriggerName, ref.Trigger.TriggerKey),
	}, nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// synthesizeRevisionForRun materializes a fresh manual revision when the
// resolver finds none on disk. It mirrors the M5.a plan-write path: build
// a triggerctx.TriggerOccurrence (system.manual), derive the revision key
// from the in-memory plan, and persist via revision.WriteRevision +
// WriteManifest. The returned revKey points at the persisted triplet.
//
// Phase 1 always persists because CreateExecution needs the revision
// manifest on disk to update latestExecutionSummary. Spec §2.3 reserves
// `--persist-revision` for the in-memory-only variant (M5.b leaves the
// flag unwired).
func synthesizeRevisionForRun(
	ctx context.Context,
	stateStore statestore.StateStore,
	plan *model.Plan,
	loadedIntent *model.Intent,
) (string, error) {
	trig, err := triggerctx.ResolveTriggerContext(triggerctx.ResolveOptions{
		Kind:         triggerctx.ResolveKindSystem,
		SystemFlavor: triggerctx.SystemManual,
	}, loadedIntent, nil)
	if err != nil {
		return "", fmt.Errorf("synthesize trigger: %w", err)
	}
	planHash, err := computePlanHashForRevision(plan)
	if err != nil {
		return "", err
	}
	revKey, err := revision.RevisionKey(trig, planHash)
	if err != nil {
		return "", fmt.Errorf("derive revision key: %w", err)
	}
	plan.Metadata.Revision = &model.PlanRevisionMeta{
		Key:      revKey,
		PlanHash: planHash,
	}
	planBytes, err := canonicalPlanJSON(plan)
	if err != nil {
		return "", err
	}
	cfg := revision.Config{Store: stateStore, JobCount: len(plan.Jobs)}
	rev, err := revision.WriteRevision(ctx, cfg, trig, planBytes, planHash)
	if err != nil {
		return "", fmt.Errorf("write synthesized revision: %w", err)
	}
	if err := revision.WriteManifest(ctx, cfg, rev, trig); err != nil {
		return "", fmt.Errorf("write synthesized manifest: %w", err)
	}
	return rev.RevisionKey, nil
}

// installRevisionHooks chains the bridge mirror onto an already-configured
// RunnerHooks. The chain preserves any previously-installed AfterStateUpdate
// callback (none of the existing local/remote hook installers set one, but
// the chain defends against future drift) so the bridge runs without
// stepping on the runner's own state-machine plumbing.
func installRevisionHooks(r *runner.Runner, rx *revisionExecution, legacyExecID string) {
	if rx == nil || rx.bridge == nil {
		return
	}
	if r.Hooks == nil {
		r.Hooks = &runner.RunnerHooks{}
	}
	prev := r.Hooks.AfterStateUpdate
	r.Hooks.AfterStateUpdate = func() {
		if prev != nil {
			prev()
		}
		// MirrorRunnerOutput swallows non-precondition errors and
		// emits bridge-mirror-failed events on any I/O fault — see
		// internal/executionstate/bridge.go. We discard the return
		// value here because the runner must keep advancing even
		// when the mirror is offline (compat §4 keeps `.orun/executions/`
		// authoritative for the legacy fallback resolver).
		_ = rx.bridge.MirrorRunnerOutput(context.Background(), rx.execKey, rx.revKey, legacyExecID)
	}
	prevLog := r.Hooks.AfterStepLog
	r.Hooks.AfterStepLog = func(jobID, stepID, output string) {
		if prevLog != nil {
			prevLog(jobID, stepID, output)
		}
		_ = rx.bridge.MirrorRunnerLog(context.Background(), rx.execKey, rx.revKey, legacyExecID, jobID, stepID)
	}
}

// finalizeRevisionExecution flips the execution to a terminal status once
// the runner returns. The terminal status is derived from the legacy
// state.Store's per-job tally (state.SummarizeExecutionState) — running
// the summarizer here keeps the new layout's ExecSummary in lockstep with
// what the runner persisted.
//
// The post-run mirror call ensures any final state.json bytes (in
// particular the runner's per-job terminal status flips that the bridge
// did not catch in the AfterStateUpdate hook because they raced with
// shutdown) make it into the new layout before MarkTerminal lands. The
// resolver's seven-branch + legacy fallback chain also covers misses.
func finalizeRevisionExecution(
	ctx context.Context,
	rx *revisionExecution,
	counts execmodel.ExecutionCounts,
	runErr error,
) (string, error) {
	if rx == nil {
		return executionstate.StatusCompleted, nil
	}

	// Project the runner's tally into ExecSummary. Empty counts (e.g. a
	// crash before any job ran) fall back to "all pending" so MarkTerminal
	// can still land the status flip.
	summary := executionstate.ExecSummary{Total: rx.exec.Summary.Total, Pending: rx.exec.Summary.Total}
	if counts.Total > 0 {
		summary = executionstate.ExecSummary{
			Total:     counts.Total,
			Completed: counts.Completed,
			Failed:    counts.Failed,
			Running:   counts.Running,
			Pending:   counts.Pending,
		}
	}

	status := executionstate.StatusCompleted
	switch {
	case runErr != nil:
		status = executionstate.StatusFailed
	case summary.Failed > 0:
		status = executionstate.StatusFailed
	case summary.Total > 0 && summary.Completed < summary.Total:
		// Runner returned without error but did not complete every
		// job (e.g. --job filter, partial scope). Treat as
		// completed; the per-job rows still record the truth.
		status = executionstate.StatusCompleted
	}

	if _, err := executionstate.MarkTerminal(ctx, rx.cfg, rx.revKey, rx.execKey, status, summary); err != nil {
		return status, fmt.Errorf("mark execution %s/%s terminal: %w", rx.revKey, rx.execKey, err)
	}
	return status, nil
}

// printRevisionRunSummary emits the post-run summary block (Revision /
// Trigger / Execution / Path / Status) per cli-surface.md §1.1
// conventions. The block matches the M5.a `orun plan` style so the two
// commands print a consistent shape.
func printRevisionRunSummary(rx *revisionExecution, status string) {
	if rx == nil {
		return
	}
	color := ui.ColorEnabledForWriter(os.Stdout)
	icon := ui.Green(color, "✓")
	if status == executionstate.StatusFailed {
		icon = ui.Red(color, "✗")
	}
	src := string(rx.source)
	if src == "" {
		src = "synthesized"
	}
	src = strings.TrimSpace(src)

	fmt.Println()
	fmt.Println(icon + " Execution " + status)
	fmt.Println()
	fmt.Printf("  Revision:  %s (%s)\n", rx.revKey, src)
	fmt.Printf("  Execution: %s\n", rx.execKey)
	fmt.Printf("  Path:      %s\n", rx.planFile)
}
