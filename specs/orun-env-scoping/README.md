# Feature: orun-env-scoping (the "Z" model)

> **Status: design converged — a small, almost-non-breaking feature** (was framed
> as a breaking epic; it converged to a feature). **Principle: selection is a
> *plan-time* concern; the plan is executed faithfully; a mutating `run` is
> fail-closed** — the absence of an explicit selection yields no mutation. See
> `design.md` for the full design, decisions, gaps, and the alternatives that were
> set aside.

## The model in one screen

- **`plan`** is a contextual artifact (shaped by trigger / `--changed` / env
  flags). It **defaults to all environments** (read-only, safe, good for review/CI).
- **Selection flags:** `--env <list>` (exists) + `--component <list>` (new) +
  explicit `--all` (new).
- **`run`** executes the plan **faithfully** and is **fail-closed**: a mutating run
  with no explicit selection **errors**; an all-env mutating run requires `--all`.
- **Promotion** = **in-plan `dependsOn` ordering** (Option B): within one run, a
  dependent env runs after its prerequisite, and a failed prerequisite blocks its
  dependents.
- **Scoped plans prune dangling edges with a warning**, so a narrowed plan stays
  self-contained and faithful.
- **No built-in `local` env** — a sandbox is a normally-declared environment.

## Decision ledger (converged)

| # | Decision |
|---|----------|
| Z-1 | Selection is a **plan-time** concern; `run` executes the plan faithfully. |
| Z-2 | `plan` defaults to **all** environments (read-only; safe). |
| Z-3 | `run` is **fail-closed**: mutating run with no selection errors; all-env mutating run requires `--all`. |
| Z-4 | Selection flags: `--env <list>` + `--component <list>` + explicit `--all`; trigger + `--changed` also shape the plan. |
| Z-5 | Promotion = **in-plan `dependsOn` ordering** (Option B). |
| Z-6 | Scoped plans **prune dangling edges with a warning**. |
| Z-7 | **No built-in `local` env**. |

## Gaps (see `design.md` §7 for recommendations)

- **G1** run fail-closed vs. pre-compiled plan — define the exact error/execute rule.
- **G2** cross-invocation promotion ordering — Option B is within one run; split-across-pipelines needs the deferred Option C.
- **G3** pruning is convention, not enforcement — `--all` is the CI path; escalate pruned promotion edges to an error in CI later.
- **G4** pruning semantics — uniform "dangling = endpoint not in plan"; list dropped edges.
- **G5** selector composition/precedence — define trigger ∩ `--env`, `--component`/`--changed`, `--all`.
- **G6** `--component` semantics — exact selection + pruning (no closure in v1).
- **G7** bare-`run` migration — one-release deprecation window (warn → error).
- **G8** `--all` run confirmation UX — CI explicit vs. interactive prompt.
- **G9** idempotent cross-run skip — future.

## Recommendation (implementation order)

1. **Additive, non-breaking:** add explicit `--all` + `--component`; keep `--env`
   list; `plan` stays all-by-default.
2. **Dangling-edge pruning** with a warning + surfacing (plan output + `--json`).
3. **In-plan promotion ordering** as the single mechanism (largely exists in
   `internal/planner/promotion.go`).
4. **`run` fail-closed** behind a **one-release deprecation window** (the only
   break, G7).
5. **Defer:** Option C (cross-invocation gate, G2), idempotent cross-run skip (G9),
   pruning hardening (G3).

Recommended calls within the gaps: **uniform** pruning (G4), **exact** component
selection (G6), and **`--all` as the documented CI path** (G3/G8).

## Relationship to `orun-catalog-state`

The cockpit env **selector** + component-scoped run on the existing model ships in
`orun-catalog-state` (`environments.md`, CS6). This feature is consistent with it:
the cockpit's selection is a plan-time selection like any other, and `run` stays
faithful. `orun-catalog-state` deferred-register entry **L-1** pointed at the
original epic; this feature supersedes that with the converged Z model.
