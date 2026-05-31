# Orchestrator Brief — Cycle 8

## Cache Fingerprint
- generated_at: 2026-05-31
- cycle_seq: 8
- head_sha: 30f36cfc493c148b829eb3a23c5ed1273579560e
- state_json_sha256: 4c7233b0565c436d1e9f1b1b14ea7bd05ef0eb28d02f21ee1ecde113fadf3ca6
- merged_pr_count: 168 (gh pr list --state merged --limit 1000 | wc -l)
- open_pr_count: 0 (no PR open at cycle-end; Task 0034 implementer
  has not yet opened the C4 PR-3 PR)
- last_task_agent: ai/tasks/task-0034.md
- last_worker_result: verifier-pass (Task 0033 → PR #174 merged
  at 73c6e8e on 2026-05-31)
- worktree_dirty: ai/ only (state.json + current.md + task-ledger.md +
  ai/tasks/task-0034.md + this brief; bookkeeping commit pending)

## Cache Validity Rule
The next cycle MAY skip the cold read iff ALL of:
- head_sha matches `git rev-parse HEAD` (will advance once cycle-8
  bookkeeping commit lands; expected new HEAD = next-tip after
  `ai: cycle 8 — Task 0033 verifier PASS (PR #174 merged); scope Task 0034 (C4 PR-3)`).
- state_json_sha256 matches recomputed hash.
- merged_pr_count = 168; open_pr_count expected 0 OR 1 (1 = the
  Task 0034 implementer opened the C4 PR-3 PR — that is the
  expected next state and does NOT invalidate the brief by itself;
  a PR squash-merge into main DOES invalidate because cycle 9 must
  scope Task 0035 verifier or a new task, not re-read this one).
- cycle_seq within 3 of next cycle's seq.
Otherwise: discard this brief and do a full cold read.

## Mental Model
Phase 2 / Milestone C4 / write-side complete. PR-1 (paths + writer
Steps A & B) and PR-2 (refs/indexes/events Steps C & D) are both
merged. The `Writer` interface is fully wired; only the `Resolver`
side remains, with five `ErrNotImplemented` stubs in `store.go`
and a missing `RebuildIndexes` method (the spec calls for it in §8
but it isn't on the interface yet — this PR adds it). Task 0034
scopes the final C4 PR: implement all five Resolver methods using
the §4 fallback ladder, add `RebuildIndexes` to the `Resolver`
interface, and prove byte-identical T-STORE-3 rebuild. After this
PR merges, C4 closes and C5 (the `orun catalog *` CLI) becomes
the active milestone.

The single carry-over tension from cycle 7: PR-2 closed at 90.1 %
on `internal/catalogstore` (in the PASS-with-note band, after
the verifier attached 28 tests to recover from the implementer's
85.3 % landing). The Task 0034 prompt sets the bar at ≥ 91 % and
explicitly cites that lesson — the goal is for the implementer to
land cleanly without a verifier coverage rescue this round. The
three-branch adjudication policy still applies if they don't.

## Active Spec Pointer
- spec: specs/orun-component-catalog
- milestone: C4 (`internal/catalogstore` Writer + Resolver)
- milestone_done_when_remaining:
  - C4 PR-3 (resolver + fallback chain `current → latest → main` +
    `RebuildIndexes`) — PENDING (Task 0034 implementer slot).
- next_milestone_after: C5 — Catalog CLI surface (`orun catalog *`
  subcommands per `cli-surface.md`). Suggested 2 PRs (refresh +
  list + describe + refs / tree + history + validate; diff stubbed
  for C8). Unlocked once C4 PR-3 merges.

## Open PRs
_none_ — PR #174 squash-merged at 73c6e8e on 2026-05-31. No PR
currently in flight. Cycle 9 expects the Task 0034 implementer
to open the C4 PR-3 PR.

## Deferred Backlog
_none_ — `/ai/deferred.md` carries no entries. All roadmap
candidates remain human-independent.

## Active Proposals
_none_ — no `/ai/proposals/**` entries pending adjudication. The
retry-budget 8→16 spec drift surfaced in PR-2 was ruled
non-escalating by the Task 0033 verifier (advisory spec wording,
harmless single-writer Phase 2 behaviour). Re-evaluate at C9 /
Phase 3 when remote drivers and concurrency widen.

## Last Decision Rationale
Why scope Task 0034 = C4 PR-3 implementer (and NOT, e.g., a
coverage-buffer remediation on `internal/catalogstore` or a
preview of a C5 CLI seam):
- C4 PR-3 is the direct dependency-unblock for C5. No other
  human-independent PR-sized work is closer to the critical path.
- The Resolver shape is fully specified by `catalog-store.md` §4
  + §8 + §9; no design questions to defer or proposals to
  adjudicate. The implementer can ship inline.
- The PR-2 coverage lesson (85.3 % implementer landing → 90.1 %
  verifier-attached recovery) is a one-time cost — the prompt
  explicitly sets the ≥ 91 % bar and the same three-branch
  adjudication tree, but doesn't gold-plate around it.
- Adding `RebuildIndexes` to the `Resolver` interface in this PR
  rather than splitting it out is the right scope: the function
  belongs naturally with the other read-side bodies, the
  T-STORE-3 byte-identical proof is the canonical close-out test
  for the milestone, and splitting would force an awkward
  one-method-only PR that doesn't ship a meaningful surface.
- Coverage buffer remediation as a standalone task would be busy
  work — PR-3 will move the denominator anyway, and the floor
  is held.
- `RefSelector.Snapshot` (C8 diff input) explicitly out of scope
  to keep PR-3 narrow; documented as a "not yet implemented (C8)"
  rejection path the implementer must include for safety.

## Next Cycle Hypothesis
- if **implementer-pass on Task 0034 (PR opened, CI green,
  coverage ≥ 91 %)**: cycle 9 scopes Task 0035 = C4 PR-3 verifier.
  Verifier inspects fallback ladder code path, re-runs T-STORE-3
  byte-identical assertion, audits `errors.Is` chain through
  `ErrCatalogNotFound` / `ErrComponentNotFound` /
  `statestore.ErrNotFound`, confirms zero `ErrNotImplemented`
  surfaces remain, validates ctx-cancellation behaviour in walks.
  PASS = merge → C4 closes → cycle 10 scopes Task 0036 = C5 PR-1
  implementer (`orun catalog refresh|list|describe|refs`).
- if **implementer-pass with coverage 90 ≤ x < 91**: cycle 9 still
  scopes Task 0035 verifier; verifier may attach a 6–10-test
  top-up (PR-2 precedent) and merge in path (a). C5 then unlocks.
- if **implementer-blocked (e.g. fallback walk perf budget
  unmeetable, or T-STORE-3 byte-identity reveals encoder
  non-determinism)**: cycle 9 scopes a remediation Task 0034.1
  scoped to whichever specific failure surfaced. Most likely
  trigger is encoder/merge-policy non-determinism (e.g. component
  preview ordering instability under specific input shapes).
- if **implementer raises a spec proposal**: most likely candidate
  is `RebuildIndexes` placement (interface vs free function) or
  the §4 `ResolveComponentLatest` fallback walk's source-scope
  filter rule. Cycle 9 reviews the proposal first; default
  posture is accept-with-revision unless it changes the on-disk
  contract.
- if **verifier-pass via attached fix (cycle 9 path c→a)**:
  same as the clean implementer-pass branch; cycle 10 still
  scopes Task 0036 = C5 PR-1.
- if **verifier-fail with no attached fix viable**: cycle 10
  scopes Task 0035.1 implementer-fix on the same PR branch;
  PR stays open until cleared.

## Stale Signals
- A new PR opened on the repo before cycle 9 starts —
  invalidates `open_pr_count` from 0 to 1; that is the EXPECTED
  next state and only forces a cold read if cycle 9 needs to
  scope a verifier rather than just confirm Task 0034 progress.
- `main` advances past `30f36cf` by anything other than the
  cycle-8 bookkeeping commit before cycle 9 starts.
- Squash-merge of the Task 0034 implementer PR before cycle 9
  evaluates — invalidates the brief because cycle 9 must scope
  Task 0035 verifier rather than re-read this one.
- New file under `ai/proposals/` (e.g. `RebuildIndexes`
  placement, `ResolveComponentLatest` walk filter, encoder
  determinism question).
- A user redirect away from C4 PR-3 (e.g. "skip resolver, ship
  the CLI on top of stubs") — would force cold read and update
  of Active Spec Pointer.
- Any drop in a sibling coverage floor — invalidates the
  "floors held byte-for-byte" precondition this brief assumes.
- A surprise rebuild non-determinism finding (encoder map
  iteration, sorting drift) on `internal/catalogresolve` or
  `internal/catalogmodel` — would force re-read of those packages
  and possibly a Phase 2 hash-stability proposal before C4 closes.
