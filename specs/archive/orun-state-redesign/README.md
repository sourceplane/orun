# Spec: orun-state-redesign

Phase 1 of the Orun local state model redesign. Trigger-first, revision-first,
remote-shaped. Local-only this phase; the storage contract is engineered for
direct portability to R2 / S3 / Orun Cloud without changing files or callers
later.

## Status

| Field | Value |
|-------|-------|
| Status | **Draft → Ready for implementation** |
| Owners | Orchestrator (planning), Implementer agents (execution), Verifier (gating) |
| Source design | `orun-state-redesign.md` at repo root |
| Phase | 1 of 3 — **local-only** |
| Target branch | `main` (PRs merged incrementally) |
| Spec layout | `specs/orun-state-redesign/` (repo root, flexible multi-doc) |

## Read order

This spec is intentionally split into focused documents. New contributors should
read in this order:

1. **`design.md`** — problem statement, goals/non-goals, architecture overview,
   on-disk layout, alternatives considered, correctness properties, risk register.
2. **`data-model.md`** — every persisted JSON schema (`TriggerOccurrence`,
   `PlanRevision`, `ExecutionRun`, manifests, refs, indexes) with field-by-field
   meaning and validation rules.
3. **`state-store.md`** — the `StateStore` interface, the local driver
   contract, path conventions, atomicity guarantees, error taxonomy. This is the
   one contract that must not move once published.
4. **`compatibility-and-migration.md`** — what existing CLI invocations and
   on-disk paths must keep working, and how the hidden `orun state migrate`
   command rehomes historical state.
5. **`cli-surface.md`** — exact behavioral changes for `orun plan`, `orun run`,
   `orun status`, `orun logs`, `orun describe`, `orun get plans`.
6. **`implementation-plan.md`** — milestones (not rigid waves) with
   dependencies, suggested PR boundaries, and "done when" criteria. Implementer
   agents have latitude to merge or split milestones as long as PRs stay
   reviewable.
7. **`test-plan.md`** — coverage targets, property-based tests,
   end-to-end harness, fixtures.
8. **`risks-and-open-questions.md`** — known unknowns and decisions still open.

## Phase boundaries

| Phase | In scope | Out of scope |
|-------|----------|--------------|
| **Phase 1 (this spec)** | TriggerOccurrence, system triggers, PlanRevision, revision-first layout, ExecutionRun under revision, StateStore interface + local driver, refs/indexes, bridge writer, compatibility reads/writes, migration command | R2/S3 driver, SaaS auth, Supabase sync, Durable Objects, cross-plan reuse, distributed locking, full runner rewrite, TUI surface changes |
| Phase 2 | R2/S3 driver, remote refs, Cloud project routing | (separate spec) |
| Phase 3 | DO-backed coordination, Supabase indexes, evidence reuse | (separate spec) |

## How agents use this spec

- **Orchestrator** reads `README.md` + `implementation-plan.md` + `design.md`
  to pick the next milestone. Task prompts cite the milestone ID and the design
  sections the implementer must respect.
- **Implementer** reads the cited milestone, decides PR scope, and may split a
  milestone into multiple PRs when natural. The constraint is reviewability and
  the goal stated in the milestone — not a fixed task count.
- **Verifier** reads `test-plan.md`, the touched milestone, and confirms the
  PR's claimed acceptance criteria match the milestone's "done when" list.

Spec changes follow the proposal protocol in `agents/orchestrator.md`. If a
milestone is found stale or wrong during implementation, write a proposal under
`/ai/proposals/` rather than silently deviating.

## Document conventions

- Code blocks use Go for interface definitions and JSON for on-disk schemas.
- Path examples are root-relative to `.orun/` and use forward slashes (the
  `StateStore` translates separators).
- Schema fields use `lowerCamelCase` to match existing Orun JSON conventions.
- Times are RFC 3339 with Z timezone.
- IDs (`triggerId`, `revisionId`, `executionId`) are monotonic ULIDs with type
  prefixes (`trg_`, `rev_`, `exec_`).
- Folder keys are short, human-readable, filesystem-safe (`trg-…`, `rev-…`,
  `run-…`). Long traceback fields live inside JSON, never in folder names.

## Out-of-band references

- Source design doc: `orun-state-redesign.md` (repo root)
- Cross-cutting feature: `specs/` may also host `github-artifacts/` and
  `orun-tui-cockpit/` in the future; cross-references are explicit when needed.
- Existing packages the spec composes: `internal/trigger`, `internal/runner`,
  `internal/state`, `internal/runbundle`.
