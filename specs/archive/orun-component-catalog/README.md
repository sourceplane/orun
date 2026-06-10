# Spec: orun-component-catalog

Phase 2 of the Orun local state model. Builds directly on
`specs/orun-state-redesign/` (Phase 1 — trigger/revision/execution lineage,
`StateStore`, refs/indexes), and adds two new parent levels above it:

```text
SourceSnapshot
  └─ CatalogSnapshot
      ├─ ComponentManifest[]
      ├─ CatalogGraph
      ├─ CatalogIndexes
      ├─ PlanRevision[]            ← reuses orun-state-redesign
      │   └─ ExecutionRun[]        ← reuses orun-state-redesign
      └─ ComponentHistory
```

The architectural move is: **catalog is no longer metadata attached to a run;
it is the parent state that owns operational history.** Source/Git state
becomes the root of every persisted lineage; main-branch catalog state is the
canonical SaaS source of truth; PR/branch/dirty workspaces produce preview
catalogs sharing the same on-disk and remote-shaped paths.

## Status

| Field | Value |
|-------|-------|
| Status | **Draft → Ready for implementation** |
| Owners | Orchestrator (planning), Implementer agents (execution), Verifier (gating) |
| Source design | `orun-catalog-full-design.md` at repo root |
| Phase | 2 of 3 — **local-only**, remote-shaped |
| Predecessor | `specs/orun-state-redesign/` (Phase 1, COMPLETE — M0–M6 merged) |
| Target branch | `main` (PRs merged incrementally) |
| Spec layout | `specs/orun-component-catalog/` (repo root, flexible multi-doc) |

## Read order

This spec is intentionally split into focused documents. Read in this order:

1. **`design.md`** — problem, goals/non-goals, lineage, on-disk layout,
   package boundaries, write order, correctness invariants, dependency on
   Phase 1.
2. **`data-model.md`** — every persisted JSON schema (`SourceSnapshot`,
   `CatalogSnapshot`, `ComponentManifest`, `CatalogGraph`,
   `ComponentHistoryEvent`, refs, indexes, authored `component.yaml`,
   `intent.yaml` catalog defaults) with field-by-field validation rules.
3. **`identity-and-keys.md`** — source/catalog/component/revision key
   construction rules, dirty-hash inputs, ULID prefixes, sanitization,
   collision handling.
4. **`resolution-pipeline.md`** — the resolver pipeline
   (discover → load → infer → inherit → validate → hash), precedence rules,
   and provenance recording.
5. **`catalog-store.md`** — path helpers, write-order contract, read-fallback
   rules, refs and indexes layout, reuse of `internal/statestore`.
6. **`compatibility-and-migration.md`** — Phase 1 layout reads/writes the new
   catalog must keep working, plan/run integration shape changes,
   backward-compatible flags, dual-write window.
7. **`cli-surface.md`** — exact behavior for new `orun catalog *` commands and
   plan/run flag additions.
8. **`sync-model.md`** — future SaaS sync seam (interface only this phase),
   future remote object layout, DB tables, dirty preview rules.
9. **`implementation-plan.md`** — milestones **C0 → C9** with goals,
   dependencies, suggested PR scope, and "done when" criteria. Implementer
   agents have latitude to merge or split milestones across PRs.
10. **`test-plan.md`** — coverage targets, property tests, E2E walk,
    compatibility gates.
11. **`risks-and-open-questions.md`** — live risk and decision register.

## Phase boundaries

| Phase | In scope | Out of scope |
|-------|----------|--------------|
| **Phase 2 (this spec)** | `SourceSnapshot`, `CatalogSnapshot`, `ComponentManifest` resolver, `CatalogGraph`, catalog-aware refs/indexes, `internal/sourcectx`, `internal/catalogmodel`, `internal/catalogresolve`, `internal/catalogstore`, `internal/catalogdiff`, catalog CLI surface, plan/run integration, `Syncer` interface (NoopSyncer impl) | R2/S3 driver, real remote sync, Supabase indexing, Durable Objects, Backstage/Datadog adapters, TUI catalog screens, distributed locking, dirty-preview remote sync |
| Phase 3 | R2/S3 catalog driver, remote refs, Cloud project routing, evidence reuse | (separate spec) |

## Relationship to predecessor spec

`specs/orun-state-redesign/` introduced:

- `TriggerOccurrence → PlanRevision → ExecutionRun` lineage (M1–M3),
- `internal/statestore` with logical paths and atomicity guarantees (M2),
- `internal/executionstate` bridge (M4),
- compatibility reads + hidden migration command (M5),
- coverage gates and E2E walk (M6).

This spec **wraps** that lineage with a `SourceSnapshot → CatalogSnapshot`
parent. It does **not** replace the Phase 1 packages; `internal/statestore`
remains the only path through which new layout files are written, and revision
keys, ExecID format, and trigger metadata are preserved verbatim. The only
change for Phase 1 callers is that revisions now live under a catalog parent
directory and carry two new identifying fields (`sourceSnapshotKey`,
`catalogSnapshotKey`).

## How agents use this spec

- **Orchestrator** reads `README.md` + `implementation-plan.md` + `design.md`
  to pick the next milestone (C0…C9). Task prompts cite the milestone ID and
  the design sections the implementer must respect.
- **Implementer** reads the cited milestone, decides PR scope, and may split a
  milestone into multiple PRs when natural. Constraints are reviewability and
  the milestone's "done when" criteria — not a fixed sub-task count.
- **Verifier** reads `test-plan.md`, the touched milestone, and confirms the
  PR's claimed acceptance criteria match the milestone's "done when" list, and
  that no Phase 1 invariant was broken.

Spec changes follow the proposal protocol in `agents/orchestrator.md`. If a
milestone is found stale or wrong during implementation, write a proposal under
`/ai/proposals/` rather than silently deviating.

## Document conventions

- Code blocks use Go for interface definitions and JSON for on-disk schemas.
- Path examples are root-relative to `.orun/` and use forward slashes (the
  `StateStore` translates separators).
- Schema fields use `lowerCamelCase` to match existing Orun JSON conventions.
- Times are RFC 3339 with Z timezone.
- IDs (`sourceSnapshotId`, `catalogSnapshotId`, `componentId`) are monotonic
  ULIDs with type prefixes (`src_`, `cat_`, `cmp_`).
- Folder keys are short, human-readable, filesystem-safe (`src-…`, `cat-…`,
  `rev-…`, `run-…`). Long fields live inside JSON, never in folder names.
- Component keys are `<namespace>/<repo>/<componentName>`. Environment is
  **never** part of component identity.

## Out-of-band references

- Source design doc: `orun-catalog-full-design.md` (repo root)
- Predecessor spec: `specs/orun-state-redesign/` (Phase 1, complete)
- Existing packages composed by this spec:
  `internal/trigger`, `internal/triggerctx`, `internal/statestore`,
  `internal/revision`, `internal/executionstate`, `internal/runner`,
  `internal/runbundle`.
- Cross-cutting downstream specs: `.kiro/specs/orun-tui-cockpit/`,
  `.kiro/specs/github-artifacts/` — both consume `CatalogSnapshot` once C5+
  lands; cross-references are explicit when needed.
