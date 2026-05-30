# Current Roadmap Position

## Active Spec
`specs/orun-state-redesign/` (Phase 1, local-only) — trigger-first revision-first
local state model. See `specs/orun-state-redesign/README.md` for the index and
read order.

## Active Milestone
**M2 — `internal/statestore`** (frozen interface + local driver). PR A landed
on main; PR B (CAS/List + atomicity property suite) is the next implementer
task; PR C (typed refs/indexes marshallers) follows.

## Current Task
None active. Awaiting orchestrator scoping for **Task 0004 — M2 PR B
implementer** (see Next Task below).

## Last Completed Task (0003 — Verifier PASS, merged)
- Implementer prompt: `ai/tasks/task-0003.md`
- Verifier prompt:    `ai/tasks/task-0003-verifier.md`
- Implementer report: `ai/reports/task-0003-implementer.md`
- Verifier report:    `ai/reports/task-0003-verifier.md`
- PR **#154** (`impl/task-0003-m2-statestore-pra`) verified PASS and squash-merged
  on 2026-05-29 → main commit `9b0a39c`. Branch deleted.
- Durable outcome on main:
  - `internal/statestore` package — leaf-clean (zero `internal/*` deps).
  - **Frozen** `StateStore` interface (`Root`, `Read`, `Write`,
    `CreateIfAbsent`, `CompareAndSwap`, `List`, `Delete`) plus
    `ObjectMeta`, `ObjectInfo`, `WriteOptions`.
  - Four-error sentinel taxonomy: `ErrNotFound`, `ErrExists`,
    `ErrConflict`, `ErrInvalid`. All driver errors wrap a sentinel via
    `fmt.Errorf("%w: …", ErrX, …)` — `errors.Is` / `errors.As` only,
    no string sniffing.
  - `paths.go` covers every helper in `state-store.md` §2.1
    (revision dir/plan/trigger/revision/manifest, execution dir +
    execution doc + snapshot + event with zero-padded 20-digit decimal
    seq for lexicographic order, refs latest-revision/latest-execution
    /trigger latest+scope/named, indexes revision+execution).
    Alphabet policy `[a-zA-Z0-9._-]` enforced by `ValidateComponent` /
    `ValidatePath`.
  - `LocalStore` implements the non-CAS subset: `Root`, `Read`, `Write`,
    `CreateIfAbsent`, `Delete`. `Write` is atomic (temp + `fsync` +
    `os.Rename` with `EXDEV` cross-device fallback into a tempdir on the
    target FS). `CreateIfAbsent` uses `O_EXCL`. `Delete` no-ops on
    absent paths and refuses non-empty (and empty — slightly stricter)
    directories with `ErrInvalid`. Orphan-tempfile sweep at construction
    removes `.orun-tmp-*` older than 1 hour using `LocalConfig.Clock`.
  - `CompareAndSwap` and `List` are stubbed returning
    `%w: … not implemented in PR A` wrapping `ErrInvalid`. Higher
    layers MUST NOT call them until PR B lands.
  - No production callers wired (`cmd/orun`, `internal/state`,
    `internal/runner`, `internal/runbundle` untouched).
  - Coverage on `internal/statestore` measured 95.4 % locally
    (`make test-state-redesign` enforces ≥ 95 %).
- CI evidence: `CI / Orun Plan` run **26665146437** observed real
  `orun plan --from-ci github …` invocation with `0 components × 3 envs
  → 0 jobs` (legitimate empty-matrix M2-PR-A shape). `orun remote-state
  conformance / Harness dry-run guard` run **26665146435** logged 30+
  `[guard] PASS` assertions.
- Verifier non-blocking findings: empty-directory `Delete` returns
  `ErrInvalid` (state-store.md §3.4 only mandates non-empty); `LocalConfig.Clock`
  is an additive test-injection hook beyond §5. Both documented; neither
  blocks merge.

## Repo Checkpoint

| Attribute | Value |
|---|---|
| Branch | main (synced with origin/main) |
| Last commit on main | `9b0a39c` — Task 0003: M2 PR A — internal/statestore (StateStore interface + local driver) (#154) |
| Open PRs (state-redesign lineage) | none |
| Repo health | 🟢 Green |
| Last verified | 2026-05-29 (Task 0003, PR #154) |
| Active milestone | M2 (`internal/statestore`) — PR A landed; PR B next |

## Roadmap (M0 → M6)
1. ✅ **M0 Foundation** — landed on main at `4ea1980` (PR #152).
2. ✅ **M1 `internal/triggerctx`** — landed on main at `db342dd` (PR #153).
3. **M2 `internal/statestore`** ← current
   - ✅ PR A — frozen interface + local driver non-CAS ops (PR #154 → `9b0a39c`)
   - ⏭ PR B — `CompareAndSwap`, `List`, 100-goroutine atomicity / exclusivity property suite, `rapid` round-trip
   - PR C — typed refs/indexes marshallers (additive on top of the frozen interface)
4. M3 `internal/revision`
5. M4 `internal/executionstate` + runner bridge
6. M5 CLI rewire (`orun plan/run/status/logs/describe/get plans` + hidden `state migrate`)
7. M6 End-to-end + property gates

## Next Task (proposed)
**Task 0004 — M2 PR B (Implementer)** on a fresh branch
`impl/task-0004-m2-statestore-prb`:
- Implement `*LocalStore.CompareAndSwap` (Read → revision compare →
  Write under a per-path mutex or accepting the documented Phase-1
  best-effort race; `ErrNotFound` on absent, `ErrConflict` on revision
  mismatch).
- Implement `*LocalStore.List` (walk translated prefix, skip symlinks,
  return `[]ObjectInfo`; order unspecified).
- Property suite per `test-plan.md` §2/§3:
  - 100-goroutine `Write+Read` atomicity (readers always observe
    complete JSON).
  - 100-goroutine `CreateIfAbsent` exclusivity (exactly one wins).
  - Concurrent CAS conflict (one wins, one returns `ErrConflict`).
  - `pgregory.net/rapid` round-trip on path-alphabet inputs through
    `Write` → `Read`.
- Coverage gate stays ≥ 95 % (target ≥ 96 % once stubs are gone).
- No production-caller wiring; `internal/statestore` stays leaf-clean.

If anything later requires re-opening PR A's scope, file a fix-up task;
the verifier left a Minor on empty-dir Delete and an additive `Clock`
field — neither requires action unless a downstream consumer needs the
loosened semantic.

## Known Spec Drift / Open Questions
- Persistent local-only `kiox -- orun plan --changed --intent
  examples/intent.yaml` failure on the composition-cache resolution
  (`stack.yaml at ~/.orun/cache/compositions/c41fc08… has no
  spec.compositions`). Reproduces from Task 0001 onward on main and on
  PR branches. CI passes the same invocation. Not a regression; revisit
  only if it surfaces during M3+ verification.
- Optional clarification: `state-store.md` §3.4 could explicitly state
  whether empty-directory `Delete` succeeds or returns `ErrInvalid`. The
  PR-A implementation chose the conservative refuse-all-directories
  path. File a proposal under `ai/proposals/` if a future caller needs
  the loosened behavior.

## Secondary Specs (not driving new tasks this phase)
- `.kiro/specs/orun-tui-cockpit/` — paused. Resumes after M5 lands.
- `.kiro/specs/github-artifacts/` — cross-check only; new revision/execution
  keys must remain compatible with the existing
  `gh-{run_id}-{attempt}-{sha}` ExecID shape produced by `internal/runbundle`.
