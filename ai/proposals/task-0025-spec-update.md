# Spec Proposal — Task 0025 (C2 PR-Boundary wording)

**Origin:** Task 0025 implementer + verifier pass.
**Status:** Accepted by verifier; folds into the C2 PR-Boundary text used
by Tasks 0026 / 0027 / C5 / C8 prompts.
**Scope:** Wording only — no behavioural change.

## Background

Task 0025 (`internal/catalogresolve` discover/load/inherit) needed access
to the canonical `component-yaml.schema.json` artifact owned by
`internal/catalogmodel/`. Go's `//go:embed` directive cannot escape its
package directory, so a downstream package cannot embed the schema
without one of:

1. **Additive sibling file** in `internal/catalogmodel/` exposing
   `var ComponentYAMLSchema []byte` via `//go:embed`.
2. Vendoring a duplicate copy of the schema into the consumer package
   (forbidden by `data-model.md` "single source of truth").
3. Reading the schema at runtime via absolute path (fragile under
   `go install` / cross-repo embedding).

The Task 0025 prompt's PR-Boundary §3 said:

> No edits to `internal/catalogmodel/` or `internal/sourcectx/`.

Read literally, this also forbids option (1). The implementer adopted
option (1) (`internal/catalogmodel/schema_embed.go`, 18 lines, `//go:embed`-only,
**no edits to any pre-existing source file**) as the narrowest reading,
flagged as Assumption #1 for verifier review.

## Verifier adjudication

ACCEPT option (1) as the conventional pattern for cross-package contract
surfaces in `internal/catalogmodel/`. The accepted convention, going
forward in Phase 2:

> One additive file per cross-package contract surface in
> `internal/catalogmodel/`. No edits to existing source files. Each
> additive file is `//go:embed`-only or a small read-only typed view —
> no logic, no validation, no mutation helpers.

This is now load-bearing for:

- **Task 0026** (C2 PR-2 validate / inference / `manifestHash`) — will
  reuse `ComponentYAMLSchema` for runtime validation.
- **C5 CLI** — needs schema bytes to power `orun lint`.
- **C8 catalogdiff** — needs the same authored shape contract.

## Proposed wording change

For all C2+ task prompts (Tasks 0026, 0027, …) and the C2 milestone
acceptance text in `specs/orun-component-catalog/implementation-plan.md`,
replace:

> No edits to `internal/catalogmodel/` or `internal/sourcectx/`.

with:

> No edits to **existing source files in** `internal/catalogmodel/` or
> `internal/sourcectx/`. Additive sibling files (embed-only exports,
> small read-only typed views) needed by dependent packages are
> permitted; one additive file per cross-package contract surface, no
> logic.

Apply the same wording to the `internal/sourcectx/` clause for symmetry.

## Non-changes

- The "single source of truth" rule in `data-model.md` §6 stays as-is —
  no vendored duplicates, no schema re-derivation from Go types.
- Phase 1 packages (`internal/statestore`, `internal/revision`,
  `internal/executionstate`, `internal/triggerctx`) remain off-limits
  for any edits, additive or otherwise, until Phase 1 is officially
  reopened.

## Action

Orchestrator should fold the wording into Task 0026's prompt at scope
time and into `specs/orun-component-catalog/implementation-plan.md` §C2
in the next bookkeeping pass.
