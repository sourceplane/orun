# Orchestrator Brief

## Cache Fingerprint
- generated_at: 2026-05-31T09:56:05Z
- cycle_seq: 4
- head_sha: 75082ca7042bc1d2e9781449add5163ef80f90b4
- state_json_sha256: bd0b2f8b136bda1ac9efe9a303c302188be61e312e3cd14050bc6a4e5dffc649
- merged_pr_count: 166
- open_pr_count: 0
- last_task_agent: ai/tasks/task-0030.md
- last_worker_result: verifier-pass
- cycle_4_action: scope Task 0030 (C4 PR-1 implementer — `internal/catalogstore`
  paths + body writer). PR #172 squash-merged at `75082ca`; Task 0029 verifier
  PASSed; bookkeeping cycle closed. State / current / ledger updated; this
  brief overwritten.

## Cache Validity Rule
The next cycle MAY skip the cold read (loop steps 1–7) iff ALL of:
- `git rev-parse HEAD` == `75082ca7042bc1d2e9781449add5163ef80f90b4`
- `shasum -a 256 ai/state.json` first field ==
  `bd0b2f8b136bda1ac9efe9a303c302188be61e312e3cd14050bc6a4e5dffc649`
- `gh pr list --state merged --limit 1000 | wc -l` == 166
- `gh pr list --state open | wc -l` == 0 (Task 0030 not yet pushed) OR == 1
  (Task 0030 PR open — predicted implementer-pass path; treat that as a
  fingerprint-mismatch on `open_pr_count` and cold-read for verifier emission)
- next cycle_seq ≤ 7 (this brief generated at seq 4; valid for 5–6)
Otherwise: discard this brief and do a full cold read.

## Mental Model (the synthesis)
Phase 2 has crossed C3 and is now standing at the gates of C4 — the first
milestone that puts bytes on disk. PR #172 squash-merged cleanly at
`75082ca`; `BuildCatalog(ctx, opts, ResolverInputs)` is now the
deterministic data-only upstream that consumers will read from.
`internal/catalogstore` does not exist on `main` yet — this is greenfield.
The C4 spec (`catalog-store.md`) is dense (248 lines, 9 sections) and
naturally fans into 2–3 PRs per the implementation plan. I scoped PR-1 as
the path layer plus the body-write half of `Writer` (steps A and B of §3
write-order). The non-obvious decision was carving `WriteRefs` /
`WriteGlobalIndexes` / `AppendComponentEvent` (steps C and D) out of
PR-1 entirely and forcing them into a typed `ErrNotImplemented` stub —
this lets PR-1 finalise the public surface (`Writer` / `Resolver` /
`Store` interfaces) so PR-2 and PR-3 can fill bodies without widening
signatures. The other key call: pre-flight `ErrInputsInconsistent`
guard rejects mismatched `sourceSnapshotKey` / `catalogSnapshotKey`
linkage between `src` / `cat` / `manifests` BEFORE issuing any write.
That converts a class of programmer error into a fast deterministic
failure instead of a partially-written catalog tree. I deliberately
deferred `-x<n>` collision-suffix logic on `catalogSnapshotKey` to PR-3
(resolver territory): PR-1 asserts inputs are already final and refuses
inconsistent ones; collision probing belongs upstream of the writer.
Risk to watch on the verifier pass: B.4 (local indexes via plain
`Write`) is intentionally non-CAS per spec — verifiers sometimes
"harden" rebuildable artifacts with CAS reflexively; the spec says no.

## Active Spec Pointer
- spec: specs/orun-component-catalog
- milestone: C4 (PR-1 in flight)
- milestone_done_when_remaining:
  - All of `catalog-store.md` §3 steps A–D and §4 reader fallback +
    `RebuildIndexes` (T-STORE-3) shipped across PR-1 / PR-2 / PR-3.
  - `internal/catalogstore` package coverage ≥ 90 %.
  - All writes go through `internal/statestore`; an `import-restriction`
    lint enforces it in CI (PR-2 or PR-3 will add this).
  - Refs `current` / `main` / `branches/<x>` / `prs/<n>` round-trip
    (PR-2).
  - Reader fallback exercised by a test that scrubs the global index
    and asserts walk-based recovery (PR-3).
  - `RebuildIndexes` byte-identical idempotence (T-STORE-3) (PR-3 or
    follow-up under C4 scope).
- next_milestone_after: C5 — Catalog CLI (`orun catalog refresh / list /
  describe / refs / tree / history / validate`, `diff` stubbed for C8).
  Cannot start until C4 closes because every CLI command consumes the
  Resolver shipped in C4 PR-3.

## Open PRs (one line each)
_none_ — PR #172 merged at `75082ca`. Task 0030 implementer is expected
to open the next PR on branch `task-0030-catalogstore-c4-pr1`.

## Deferred Backlog (parking lot summary)
_none_ — no `/ai/deferred.md` file exists. Phase 1 carry-forward
candidates (MirrorModeHardlink debug-fold, RunnerHooks.AfterStateUpdate
async-mirror, `--persist-revision` flag, Option B trigger-name resolver,
`--prune-legacy`) remain tracked in `state.json.notes` as post-Phase-2
candidates, not currently scheduled.

## Active Proposals
- `ai/proposals/task-0002-spec-update.md` — closed (rapid import-path
  clarification, folded into Phase 1 docs at M-time). Stance: archived.
- `ai/proposals/task-0019-spec-update.md` — closed (Phase 1 trigger-name
  resolver Option B; deferred as Phase 1 carry-forward in
  `state.json.notes`). Stance: deferred (Phase 2 carry-forward).
- `ai/proposals/task-0025-spec-update.md` — closed (folded into Task
  0026 prompt; convention adopted as load-bearing Phase 2 rule).
  Stance: closed-and-folded.

No new proposals owed by the orchestrator this cycle. Task 0029 verifier
recorded "no proposals — C3 implementation matches spec; minor wording
polish on data-model §4 / resolution-pipeline §7 node-ordering noted as
non-blocking editorial". Brief confirms: no spec-update task warranted.

## Last Decision Rationale
Why Task 0030 = C4 PR-1 (paths + body writer) was the highest-leverage
emission this cycle:
- C3 closed cleanly (PR #172 merged, all CI green, verifier PASS, no
  proposals owed). The orchestrator loop step 14 ("Wait for worker
  result") is satisfied; the natural emission is the C4 implementer.
- C4 spec has a built-in 2–3 PR seam per `implementation-plan.md` —
  emitting a "land all of C4 in one PR" task would violate the
  PR-Sized Task Standard (paths + writer + resolver + refs + indexes
  is at least 4 reviewable surfaces). Splitting at the body-write
  boundary is the cleanest seam: PR-1 is purely additive, has no
  read-path concerns, and ships the public surface that PR-2 / PR-3
  can fill without widening.
- Forcing `Writer` / `Resolver` / `Store` interface decls into PR-1
  (with `ErrNotImplemented` stubs for unfilled methods) is the
  standard "freeze the contract first" move — it eliminates the risk
  of PR-2 silently widening a method signature mid-flight and
  invalidating PR-1's reviewer mental model.
- Pre-flight `ErrInputsInconsistent` instead of trusting `CreateIfAbsent`
  alone: deterministic failure on mismatched keys catches programmer
  errors before any partial-write state hits disk. Cheap to write,
  high leverage.
- Deferring `-x<n>` collision-suffix logic to PR-3 (resolver territory)
  is correct: that probing logic needs to read existing
  `sources/<srcKey>/catalogs/*` to test prefix collisions, which is a
  read-side concern. PR-1 should not own a read-side responsibility.
- The kanban-style alternative ("emit Task 0030 = the entire C4
  milestone") was rejected because the spec itself explicitly suggests
  2–3 PRs and the resulting prompt would either be too vague to
  enforce or so prescriptive it would dictate the implementer's
  internal sequencing.

## Next Cycle Hypothesis
- **if implementer-pass on Task 0030:** emit Task 0031 = C4 PR-2 verifier
  on the new PR (review paths.go boundedness, body-write call order,
  pre-flight inconsistency guard, stub semantics, coverage floors held).
  After verifier-pass + merge, emit Task 0032 = C4 PR-2 implementer
  (`refs.go` + `indexes.go` covering write-order steps C and D plus
  `AppendComponentEvent`'s seq.lock retry-up-to-16 contract).
- **if implementer-blocked:** likely blockers — (a) `internal/catalogmodel`
  doesn't expose a public canonical-encoder entry point usable from a
  fresh sibling package (would need a tiny additive export under the
  C2 PR-1 convention; no spec change needed); (b) `statestore.StateStore`
  lacks a clean test fake exposed for reuse, forcing the implementer to
  build one inline (acceptable per task constraints); (c) the implementer
  discovers that `data-model.md` doesn't pin the local-index file shape
  (`indexes/components/<name>.json` body schema) — that would warrant a
  proposal at `ai/proposals/task-0030-spec-update.md`. Pivot accordingly.
- **if verifier-pass on the eventual PR:** as above — proceed to PR-2.
- **if verifier-fail with bounded fix:** likely surfaces — graph order
  not actually fixed (map iteration leaking through), a missing
  `Validate*` for one of the path helpers, coverage on
  `internal/catalogstore` falling under 90 % because a stub branch
  isn't reachable. Remediation stays inside Task 0030's PR.
- **if verifier-fail with scope expansion:** unlikely. If it happens,
  most plausible cause is the pre-flight `ErrInputsInconsistent` guard
  needing to be a separate validator method (so C5 CLI / C6 plan can
  call it without committing). Emit Task 0030.1 narrowly extracting
  the validator if that's the call.
- **if proposal arrives at `ai/proposals/task-0030-*.md`:** orchestrator
  must adjudicate before merging. Most likely topic: stub policy
  (typed `ErrNotImplemented` vs. panic) or the local-index body schema.
  Decide accept-leaning unless it touches Phase 1 invariants or widens
  the public surface declared in `catalog-store.md` §1.

## Stale Signals (what would invalidate this brief early)
- A new spec proposal arrives at `ai/proposals/task-0030-*.md` — force
  cold read to adjudicate before continuing.
- A user redirect away from C4 PR-1 (e.g. "land all of C4 in one PR"
  or "skip to C5 CLI") — force cold read and re-scope per
  `references/user-directed-roadmap-override.md` patterns.
- CI starts failing on `main` (a `75082ca` post-merge regression) —
  force cold read; stabilize before continuing C4.
- `internal/catalogmodel.SourceSnapshot` / `CatalogSnapshot` /
  `ComponentManifest` types change between scope and verify (data-model
  spec drift mid-flight) — force cold read; the writer is consuming
  those types by reference.
- A Phase 1 floor regression (statestore < 95.7, revision < 90.3,
  executionstate < 90.0) — force cold read; treat as a Priority-1
  invariant break and scope a stabilisation task before continuing.
- The implementer pushes the PR but `gh pr view` shows it cannot run
  required CI (a workflow file edit needed) — force cold read and
  decide between attaching the workflow fix to the same PR or
  scoping a small unblock task.
