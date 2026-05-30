# Task 0020 — M5.d implementer report

**Task:** Implement the hidden `orun state migrate` command per
`specs/orun-state-redesign/compatibility-and-migration.md` §5, plus the
Option A CLI normalization for `orun describe revision|trigger latest`
that the Task 0019 verifier called out in `ai/proposals/task-0019-spec-update.md`.

**Branch:** `impl/task-0020-m5d-orun-state-migrate`
**Base:** `main` @ `73108ee` (post-PR-#163 merge)

---

## Files added

- `cmd/orun/command_state_migrate.go` (430 LOC) — hidden `orun state migrate`
  command + `--dry-run` flag. Two-phase walk:
    1. **Plans.** `revision.ScanLegacyPlanHashes(store)` → for each
       `<checksum>.json` we run the existing branch-5 `ResolveRevision`
       to get the canonical `(trigger, planHash, revKey)` triplet, then
       call `revision.WriteRevision` + `WriteManifest` (compatibility
       writes off — no need to mirror back into `.orun/plans/`). Already-on-disk
       revisions are detected via `statestore.ReadRevisionIndex` and
       short-circuit as "exists, skipping" lines.
    2. **Executions.** `state.Store.ListExecutions()` → per legacy
       `<execID>` we read state.json's `planChecksum`. If phase 1 saw
       that hash, attach to that revision; else synthesize an unknown-hash
       revision via `ensureUnknownRevision`. We then call
       `executionstate.CreateExecution` with `Reason=migration` and
       `Status=completed`, and `Bridge.MirrorRunnerOutput` to promote the
       legacy state.json/metadata.json into
       `revisions/<revKey>/executions/<execKey>/`.
- `cmd/orun/command_state_migrate_test.go` (260 LOC) — 5 tests:
    - happy path (plan + exec attach with mirror)
    - `--dry-run` (no `.orun/revisions/` written)
    - idempotent (rerun reports zero new revisions)
    - orphan execution (no plan hash → unknown-hash revision, summary count)
    - Option A literal `latest` normalization (resolver returns same
      revision as empty arg).
- `internal/revision/legacy_scan.go` (~95 LOC) — `ScanLegacyPlanHashes`
  helper exporting a sorted, validated, latest.json-filtered list of
  legacy plan entries. Routes through `legacyPlansDir + normalizeLegacyChecksum`
  so the migrate command never re-validates input.
- `ai/tasks/task-0020.md` — task prompt file. Filled in proactively
  because Task 0019's verifier merged PR #163 without emitting one
  (we're still on the verifier→implementer handoff for M5.d).
- `ai/reports/task-0020-implementer.md` — this report.

## Files modified

- `cmd/orun/commands_root.go` — added `registerStateCommand(rootCmd)`
  in `init()` after `registerTuiCommand`.
- `cmd/orun/command_describe.go` — Option A normalization. `describeRevision`
  and `describeTrigger` now both apply `if ref == "latest" { ref = "" }`
  at the entrance so the cli-surface.md §5.1 examples
  (`orun describe revision latest`, `orun describe trigger latest`) work
  without a resolver change. Pure CLI-layer fix; the resolver branch table
  is unchanged. Option B (trigger-name resolver branch) is **not** in this
  PR — it's a larger surface change and the verifier flagged it as
  optional.

## Spec correspondence — §5.1 algorithm

| Spec step | Implementation |
|---|---|
| `for each legacy plan: synthesize TriggerOccurrence (system.migrated)` | `revision.ResolveRevision(short hash)` → branch 5 → `triggerctx.NewSystemMigrated` |
| `compute revisionKey from synthesized trigger + plan hash` | `revision.RevisionKey(trig, planHash)` (called inside `ResolveRevision`) |
| `if revision dir exists and revision.json matches: skip` | `statestore.ReadRevisionIndex(revKey)` → on success, skip; on `ErrNotFound`, write |
| `else: write revision-first layout via revision.WriteRevision` | `cfg := revision.Config{Store: store}.WithCompatibilityWrites(false)`; `WriteRevision` then `WriteManifest` |
| `for each legacy execution: look up plan hash from state.json` | `state.Store.LoadState(execID).PlanChecksum` |
| `if a revision exists with that plan hash: attach via Bridge` | hashToRev map populated in phase 1 (keyed on both canonical "sha256:<hex>" and bare-hex stem to match what state.json historically stored); Bridge.MirrorRunnerOutput |
| `else: attach to rev-migrated-unknown-p<hash> on demand` | `ensureUnknownRevision(planHash)` writes a placeholder plan body `{"migrated":true,"planHash":"...","reason":"..."}` then calls WriteRevision |
| `emit summary: revisions_created, executions_attached, orphans` | `migrateStats` printed as `Summary:` / `Summary (dry run):` |
| `exit non-zero on any per-item error (continue processing)` | `stats.Errors++` per failure, return non-nil error iff `Errors > 0` |

## Properties (§5.2)

- **Idempotent.** Verified by `TestStateMigrate_Idempotent`. Second run
  reports zero new revisions; the index-entry pre-check + `WriteRevision`
  ErrExists handling guarantee no duplicate writes.
- **Non-destructive.** No `os.Remove` in this code path; legacy directories
  are read-only. The bridge mirrors via hardlink (with cross-device copy
  fallback), never deletes the source.
- **Resumable.** Plan and exec phases are independent; a crash mid-phase
  leaves the new layout in a partial-but-valid state because each
  CreateIfAbsent / WriteRevision is atomic.

## Constraints adherence

- ✅ **Path policy.** Zero `.orun/...` string concatenation in
  `command_state_migrate.go`. Logical paths come from `legacy.ExecDir()`,
  `statestore.ExecutionFilePath` (via Bridge), `legacyPlanPath` (via
  resolver). The one `filepath.Join(rootDir, ".orun")` is the
  store-root translation, identical to `openLocalStateStore`.
- ✅ **No new sentinel errors.** Every error wraps either an existing
  statestore sentinel via `fmt.Errorf` or a context-cancellation/IO
  error from a callee. Search `grep -n "errors.New\|var Err" cmd/orun/command_state_migrate.go` → no matches.
- ✅ **Strict JSON decode.** All on-disk reads go through helpers that
  already do `DisallowUnknownFields`: `statestore.ReadRevisionIndex`,
  `revision.ResolveRevision`, `state.Store.LoadState/LoadMetadata`. The
  unknown-hash placeholder uses `json.Marshal` (write side; strict-decode
  doesn't apply to writes).
- ✅ **Bridge mirrors only state.json + metadata.json.** We use the
  existing `Bridge.MirrorRunnerOutput`; no extension to `bridgeMirroredFiles`.
- ✅ **CompatibilityWrites gating.** Migrate explicitly sets
  `WithCompatibilityWrites(false)` because the legacy `.orun/plans/` is
  the source we're reading from — re-writing it would be circular.
- ✅ **Same-language reply.** English (user wrote in English).

## Test results

```
go test ./... -count=1 -timeout 180s
ok    github.com/sourceplane/orun/cmd/orun                9.942s
ok    github.com/sourceplane/orun/internal/revision       5.325s
ok    github.com/sourceplane/orun/internal/executionstate 29.432s
... (all other packages green)
```

`go vet ./cmd/orun/... ./internal/revision/...` clean. New tests:
- `TestStateMigrate_HappyPath_PlansAndExecutions` ✓
- `TestStateMigrate_DryRun` ✓
- `TestStateMigrate_Idempotent` ✓
- `TestStateMigrate_OrphanExecution` ✓
- `TestDescribeRevision_LiteralLatest_NormalizesToEmpty` ✓

## Out of scope (not in this PR)

- **Option B trigger-name lookup.** `refs/triggers/<name>/latest.json`
  resolver branch is a real surface change to the resolver branch table;
  the verifier flagged it as optional and Option A alone is enough to
  make the cli-surface.md §5.1 examples work. Filing a follow-up note in
  the next housekeeping pass.
- **`--persist-revision` flag.** Spec §2.3 reserves it; M5.b/M5.c left it
  unwired. Migrate doesn't need it because all phase-1 / unknown-hash
  revisions are persisted unconditionally (CreateExecution requires the
  manifest on disk).
- **`--prune-legacy`.** Spec §6 — Phase 2, explicitly out of M5.

## Risk notes

- The unknown-hash placeholder plan body is a sentinel JSON object
  rather than the real plan bytes. If a future legitimate write of the
  real plan to that revision dir happens, CreateIfAbsent rejects with
  ErrExists — the migration is the only writer of unknown-hash revisions
  by construction.
- Phase-1 success does NOT guarantee phase-2 attach succeeds. A bad
  execution (e.g. missing state.json AND metadata.json) is silently
  skipped during the scan. Per-execution errors continue the walk and
  bump `stats.Errors` so the exit code surfaces the partial failure.
