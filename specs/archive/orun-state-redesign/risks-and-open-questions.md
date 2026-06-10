# Risks and Open Questions

Tracking surface for Phase 1. Risks have mitigations and an owner;
open questions have a default decision and the trigger that would force a
re-decision.

---

## Risks

### R1 — Bridge mirror desync corrupts `orun status`

- **Likelihood:** Medium
- **Impact:** High
- **Mitigation:** Single mirror entry point in `internal/executionstate.Bridge`;
  failures emit a structured `bridge-mirror-failed` event in
  `events/`; `orun status` reader prefers revision-first paths but falls back
  to a legacy `.orun/executions/` scan on miss.
- **Owner:** M4 implementer + M5 verifier.

### R2 — Revision key collision on identical `(plan, scope, sha)`

- **Likelihood:** Medium (test environments, replay flows)
- **Impact:** Medium
- **Mitigation:** `-xN` suffix algorithm gated by
  `StateStore.CreateIfAbsent`. Property test in M3 enforces correctness.
- **Owner:** M3 implementer.

### R3 — Performance regression on `.orun/` trees with thousands of revisions

- **Likelihood:** Low
- **Impact:** Medium
- **Mitigation:** Refs and indexes avoid full-tree scans for hot paths.
  `BenchmarkResolveLatestRevision` in `test-plan.md` §7 catches regressions.
- **Owner:** M3 implementer + M6 verifier.

### R4 — Concurrent `orun plan` from two shells corrupts a ref

- **Likelihood:** Low
- **Impact:** Medium
- **Mitigation:** Refs use `StateStore.CompareAndSwap`; loser retries. Worst
  case is a one-cycle stale ref before fallback scan.
- **Owner:** M2 implementer.

### R5 — Hardlink mirror fails on cross-device FS

- **Likelihood:** Low (Docker mounts, network drives)
- **Impact:** Low
- **Mitigation:** Bridge auto-falls-back to copy on `EXDEV`. Logged.
- **Owner:** M4 implementer.

### R6 — Migration mis-attaches legacy executions to wrong revision

- **Likelihood:** Medium
- **Impact:** Medium
- **Mitigation:** Hash-based match only; orphan bucket
  `rev-migrated-unknown-p<hash>`; `--dry-run` is the documented onboarding step.
- **Owner:** M5.d implementer + M5 verifier.

### R7 — New layout invalidates existing `internal/runbundle` ExecID assumptions

- **Likelihood:** Low
- **Impact:** Medium
- **Mitigation:** Bridge preserves the GHA-style
  `gh-{run_id}-{attempt}-{sha}` ExecID as the `executionKey` (via
  `SanitizeExecID`). `internal/runbundle` unchanged.
- **Owner:** M4 implementer (verify cross-compatibility with `internal/runbundle`).

---

## Open questions

### Q1 — Auto-migrate on first invocation against a legacy `.orun/`?

- **Default decision:** No. Migration is manual only in Phase 1 (`orun state
  migrate`).
- **Re-decision trigger:** Three or more user reports of confusion about
  legacy paths not appearing in `orun status` defaults.

### Q2 — Refs/indexes format: JSON or msgpack?

- **Default decision:** JSON for inspectability with `cat` / `jq`.
- **Re-decision trigger:** A workspace exceeds 10 000 refs or 50 MB of
  ref+index files.

### Q3 — Bridge default mirror mode: hardlink or copy?

- **Default decision:** Hardlink, copy fallback on `EXDEV`.
- **Re-decision trigger:** Bug report where hardlink semantics confuse a user
  (e.g. editing a log file mutates both copies).

### Q4 — Should the writer emit a `manifest.json` in flat or nested form?

- **Default decision:** Nested per `data-model.md` §4 (matches how humans
  consume the file).
- **Re-decision trigger:** Tooling consumers (gh-artifacts pull, TUI cockpit)
  request a flat form.

### Q5 — Should `--persist-revision` materialize the synthesized revision when
running from a `--plan <file>` source?

- **Default decision:** Not in Phase 1 (flag is reserved). The synthesized
  revision lives in memory only so `--dry-run` from a file is a true read-only
  operation.
- **Re-decision trigger:** Users request persistent attribution for
  `orun run --plan` invocations.

### Q6 — Are `system.replay` and `system.api` triggers wired in Phase 1?

- **Default decision:** Constructors exist (`internal/triggerctx`), but no CLI
  flag emits them in Phase 1. Wiring lives with the feature that needs them
  (replay command in Phase 2, API server in Phase 3).
- **Re-decision trigger:** N/A — by design.

### Q7 — Should `orun state migrate` integrate into a long-running indexer?

- **Default decision:** No. Phase 1 ships a one-shot command. The
  `RebuildIndexes()` helper in `internal/statestore` is the seam for a future
  daemon.

---

## Decisions explicitly recorded (closed)

- **D1** — Folder keys are short and human-readable; full traceback lives in
  JSON. Rationale: `design.md` §10.4.
- **D2** — Single `StateStore` interface from day one, even though only the
  local driver ships. Rationale: avoid retrofitted abstraction.
- **D3** — Bridge writer instead of runner rewrite. Rationale: bounded blast
  radius, reversible.
- **D4** — Compatibility writes (`.orun/plans/<checksum>.json`,
  `.orun/plans/latest.json`) remain in Phase 1. Rationale: zero-disruption
  rollout.
- **D5** — ULID for machine IDs, separate from human folder keys. Rationale:
  monotonic lex-sort + API-friendly opacity.

---

## Process notes

- New risks discovered during implementation are appended here by the
  implementer's report, with `/ai/proposals/` opened for material changes.
- Open questions are resolved by the user or the Orchestrator. Resolutions
  move from §Open questions to §Decisions explicitly recorded.
