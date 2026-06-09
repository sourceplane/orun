# Implementation Status — orun-env-scoping ("Z" model)

> Live record of what shipped. The design is in `design.md`; milestones in
> `implementation-plan.md`. All milestones merged to `main` incrementally.

## Milestones

| # | Milestone | Status | PR | What shipped |
|---|-----------|--------|----|--------------|
| **ES1** | `--all-envs` flag + plan selection metadata | ✅ merged | #306 | `--all-envs` on `plan`/`run` (mutually exclusive with `--env`); `model.PlanSelection` + `PrunedEdge` types; `metadata.selection` stamped on every plan via `computePlanSelection`. Non-breaking. |
| **ES2** | Prune dangling edges with a warning | ✅ merged | #307 | A `same-plan` promotion dep to an unselected env is pruned (not fatal); `computePrunedEdges` records component + promotion prunes; `metadata.selection.prunedEdges` + stderr warning. |
| **ES3** | In-plan promotion as the single enforced mechanism | ✅ merged | #308 | Documented the promotion contract (in-plan ordering = enforced; cross-plan gates = advisory metadata, never read by the runner); one-line notice when inert gates are recorded. |
| **ES4** | Mutating `run` fail-closed (deprecation Phase A) | ✅ merged | #309 | A mutating `orun run` with no selection warns and proceeds (Phase A); `--dry-run` is the read-only escape; `--all-envs`/any selection is exempt. `runSelectionPresent` predicate. |
| **ES5** | End-to-end + docs | ✅ this PR | — | `cmd/orun/envscoping_e2e_test.go` (selection metadata: full / scoped / `--all-envs`); this status doc. |

## Behavior summary (as shipped)

- `plan` defaults to **all** environments (read-only). Selection flags:
  `--env <list>` · `--component <list>` · `--changed` · `--trigger` (all
  pre-existing) + **`--all-envs`** (new).
- Scoped plans are **self-contained**: dangling promotion/component edges are
  pruned, recorded in `metadata.selection.prunedEdges`, and warned at plan time.
- **Promotion** = in-plan `dependsOn` ordering (the only enforced mechanism);
  a failed prerequisite blocks its dependents within the run. Cross-plan gates
  are recorded but **not** enforced.
- A **mutating `orun run` with no selection** is fail-closed — **Phase A**
  (shipped): warns and proceeds; **Phase B** (future release): hard error.
  `--dry-run` is exempt.

## Decisions

The `Z-1…Z-7` decision ledger (`design.md` §5) and `D-1…D-10`
(`risks-and-open-questions.md`) are unchanged and fully reflected in the code.

## Deferred (not implemented — see `risks-and-open-questions.md`)

- **L-1 / Option C** — cross-invocation source-status promotion gate. Today
  cross-plan gates are advisory only.
- **L-2** — pruning hardening (pruned promotion edge → error in CI).
- **L-3** — idempotent cross-run job skip.
- **L-4** — `--with-deps` closure selection.
- **Phase B** — flipping the fail-closed `run` warning to a hard error.
