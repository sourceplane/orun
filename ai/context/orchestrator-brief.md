# Orchestrator Brief — Cycle 11

## Cache Fingerprint
- generated_at: 2026-05-31T00:00:00Z
- cycle_seq: 11
- head_sha: 96e3bbda60ce09c30264a76aecd9f726cf95e16e (main; C5 PR-1 / PR #176
  merged. ai/ bookkeeping for this cycle is uncommitted pending the seal
  commit — that commit will advance HEAD past 96e3bbd.)
- state_json_sha256: f35f0c11cd5aa57b8067be01671aeff128ef9be410a9dd84a44a96c28469c6c2
- merged_pr_count: 170 (gh pr list --state merged --limit 1000 | wc -l)
- open_pr_count: 0
- last_task_agent: ai/reports/task-0037-verifier.md
- last_worker_result: single-pass closure (Task 0036 implementer + Task 0037
  verifier in one session) → PR #176 merged, C5 PR-1 CLOSED. This cycle
  EMITTED the next focus (Task 0038 = C5 PR-2 implementer), no worker run yet.

## Cache Validity Rule
The next cycle MAY skip the cold read (loop steps 1–7) iff ALL of:
- head_sha matches `git rev-parse HEAD` (will advance once this cycle's seal
  commit lands; a C5 PR-2 merge then bumps merged→171 and advances main —
  that INVALIDATES this brief).
- state_json_sha256 matches recomputed hash.
- merged_pr_count = 170 AND open_pr_count tracks Task 0038's PR lifecycle
  (0 now → 1 when the implementer opens the C5 PR-2 → 0 after merge).
- cycle_seq within 3 of next cycle's seq.
Otherwise: discard and cold-read. NOTE: the most likely next event is the
Task 0038 implementer opening a PR (open→1) — that alone does NOT invalidate;
cycle 12 then scopes Task 0039 = C5 PR-2 verifier, the predicted path below.

## Mental Model
Phase 2 is into the user surface and C5 PR-1 has landed: `orun catalog
refresh` + `orun catalog refs` are live, backed by two new pure seams in
internal/catalogstore — `AssembleBundle` (CatalogView → the four writer-input
bundles, pure/deterministic) and `ListRefs` (the source⋈catalog ref-tree
join the Resolver interface couldn't do). The shared CLI foundation is also
in place: the `orun catalog` root + subcommand index, the single
`parseCatalogSelector` → catalogstore.RefSelector bridge, and the §11 JSON
envelope writer. Every later C5 command reuses all three.

C5 PR-2 is the remaining read surface: `list`, `describe`, `tree`, `history`,
`validate` (with `diff` stubbed → C8). These are pure consumers of what PR-1
built — `list` reuses ListRefs + Resolver.ResolveCatalog; `describe`/`tree`
walk a resolved CatalogSnapshot + its CatalogGraphs; `history` is read-only
event enumeration (event APPEND stays C7); `validate` re-runs the resolver in
strict mode and reports issues. No new write paths, no new on-disk contract.

## Active Spec Pointer
- spec: specs/orun-component-catalog
- milestone: C5 (Catalog CLI surface) — PR-1 ✅ CLOSED, PR-2 remaining
- milestone_done_when_remaining (whole C5, PR-2 closes it):
  - `list` enumerates catalogs/components with `--json`. (Task 0038)
  - `describe` resolves a component selector; ambiguity exits 4 with
    candidates. (Task 0038)
  - `tree` renders the dependency/systems/owners graphs. (Task 0038)
  - `history` enumerates component events read-only. (Task 0038)
  - `validate` re-runs resolution strict + reports issues; `diff` stubbed
    → C8. (Task 0038)
  - All commands inherit the shared RefSelector parser + §11 envelope +
    help-fixture gating from PR-1.
- next_milestone_after: C6 — `orun plan` integration (plans live under
  (SourceSnapshot, CatalogSnapshot) parents). Unlocked once C5 closes.

## Open PRs
_none_ — PR #176 (C5 PR-1) merged at 96e3bbd. Task 0038 implementer has not
opened its PR yet.

## Deferred Backlog
_none_ — `/ai/deferred.md` carries no entries. All roadmap candidates
human-independent; loop is running on Task 0038.

## Active Proposals
_none_ pending. Carry-forward for the PR-2 implementer: CatalogLocalIndexes
currently emits component-execution indexes only (empty executions[]); the
owner/system/domain/type axes are unspecified in data-model.md §9. If `list`/
`describe` need those axes populated, the implementer files
`ai/proposals/task-0038-spec-update.md` and cycle 12 adjudicates first.
Standing non-escalated drift: retry-budget 8→16 (advisory, harmless
single-writer Phase 2, revisit C9/Phase 3).

## Last Decision Rationale
Why this cycle closed C5 PR-1 as a single-pass closure (implementer +
verifier in one session) rather than two cycles:
- The user issued a full-ship-cycle directive ("do your full duties … always
  produce new tasks or verifier as suited"), which the user-profile exception
  treats as standing approval to run end-to-end (implement → PR → CI → verify
  → merge) without per-phase pauses.
- The verification gates were still run in full inline (scope, code
  inspection, dependency-direction grep, no-raw-FS grep, -race, coverage,
  verify-generated, CI watch) — only the agent identity collapsed.
- Seam-home call confirmed at verification: AssembleBundle landed in
  catalogstore (not catalogresolve as the scope hint suggested) because it
  returns catalogstore types and the architecture rule forbids
  catalogresolve importing catalogstore. This is the correct, invariant-
  preserving home — recorded so PR-2 doesn't relitigate it.

## Next Cycle Hypothesis
- if **implementer-pass on Task 0038** (PR opened, CI green): cycle 12 scopes
  Task 0039 = C5 PR-2 verifier — verify the read-surface commands, re-measure
  coverage on catalogstore/catalogresolve/sourcectx/cmd-orun, adjudicate via
  the three-branch policy (path-a attach tests if a floor is missed — the
  recurring C4 pattern), confirm reuse of the PR-1 seams + envelope + parser,
  then merge. On merge **C5 CLOSES** and C6 (plan integration) unlocks.
- if **implementer-blocked** (a read command needs an index axis the spec
  doesn't define, e.g. owner/system/domain/type for `list`): cycle 12 reads
  the blocker/proposal, adjudicates the axis shape, re-emits a narrowed
  Task 0038.1.
- if **implementer files ai/proposals/task-0038-spec-update.md** (likely
  candidate: CatalogLocalIndexes axis population or the `describe` ambiguity/
  exit-4 candidate-list shape): cycle 12 adjudicates first; default posture
  accept-with-revision unless it changes the on-disk index contract (touches
  identity-and-keys.md / data-model.md §9 — handle with care).
- if **verifier later FAILs on a coverage floor** (recurring C4 pattern):
  scope a path-(a) attach-tests top-up on the same PR branch; PR stays open
  until ≥ 90 %.
- if **executionstate flaps red again** on Task 0038 CI (R-008): not a
  blocker (rerun clears); promote R-008 to a scoped micro-task only if it
  reds a third time. Do NOT fold into the catalog PR.

## Stale Signals
- Task 0038 implementer opens its PR → open_pr_count 0→1 (expected; cycle 12
  scopes the verifier, does NOT re-read this brief's planning).
- The C5 PR-2 PR squash-merges → merged→171, main advances past 96e3bbd →
  forces cold read; **C5 CLOSES**, cycle then scopes C6 (plan integration).
- A new file under `ai/proposals/` (CatalogLocalIndexes axis or describe
  candidate-list shape).
- User redirect away from C5 PR-2 (e.g. "jump to plan integration" or "fold
  remaining catalog commands differently").
- The implementer reuses a PR-1 seam incorrectly — re-implements ListRefs/
  AssembleBundle instead of importing, or buries read logic in cobra RunE
  instead of a tested helper — verifier must catch this.
- Any Phase-1 or Phase-2 coverage floor drops below its recorded value.
