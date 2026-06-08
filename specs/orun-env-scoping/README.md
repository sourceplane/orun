# Feature: orun-env-scoping (the "Z" model)

> **Status: design converged — a small, almost-non-breaking feature** (was framed
> as a breaking epic; it converged to a feature). **Principle: selection is a
> *plan-time* concern; the plan is executed faithfully; a *mutating* `run` is
> fail-closed** — the absence of an explicit selection yields no mutation.

## The model in one screen

- **`plan`** is a contextual artifact (shaped by trigger / `--changed` / env / component
  flags). It **defaults to all environments** (read-only, safe, good for review/CI).
- **Selection flags (mostly already exist):** `--env <list>`, `--component <list>`,
  `--changed`, `--trigger` — plus the one new flag **`--all-envs`** (explicit "all";
  `--all` is taken by the root component/CWD-scoping flag).
- **`run`** executes the plan **faithfully** and a mutating run is **fail-closed**:
  no explicit selection → error; all-env mutating run requires `--all-envs`;
  `--dry-run` is the read-only escape.
- **Promotion** = **in-plan `dependsOn` ordering** (Option B): within one run, a
  dependent env runs after its prerequisite, and a failed prerequisite blocks its
  dependents.
- **Scoped plans prune dangling edges with a warning**, so a narrowed plan stays
  self-contained and faithful.
- **No built-in `local` env** — a sandbox is a normally-declared environment.

## Status

| Field | Value |
|-------|-------|
| Status | **Design converged → ready for implementation planning** |
| Type | Feature; additive + one deprecation-windowed break (mutating `run` fail-closed) |
| Builds on / relates to | `specs/orun-catalog-state/` (cockpit env selector); `specs/orun-state-redesign/` (`PlanScope`/`RevSummary` carry the selection) |
| Read order | `design.md` → `cli-surface.md` → `data-model.md` → `implementation-plan.md` → `test-plan.md` → `compatibility-and-migration.md` → `risks-and-open-questions.md` |

## Decision ledger (converged)

| # | Decision |
|---|----------|
| Z-1 | Selection is a **plan-time** concern; `run` executes the plan faithfully. |
| Z-2 | `plan` (and `run --dry-run`) default to **all** environments (read-only; safe). |
| Z-3 | A **mutating `run` is fail-closed**: no selection → error; all-env mutating run requires `--all-envs`; `--dry-run` is the read-only escape. |
| Z-4 | Flags: existing `--env <list>` + `--component <list>` + `--changed` + `--trigger`, plus new `--all-envs`. |
| Z-5 | Promotion = **in-plan `dependsOn` ordering** (Option B); failed prerequisite blocks dependents. |
| Z-6 | Scoped plans **prune dangling edges with a warning**. |
| Z-7 | **No built-in `local` env**. |

## Gaps (resolutions in `design.md` §7 + `risks-and-open-questions.md`)

G1 fail-closed vs. pre-compiled plan · G2 cross-invocation promotion ordering (→ Option C) · G3 pruning is convention not enforcement · G4 pruning semantics · G5 selector composition · G6 `--component` exact vs. closure · G7 bare-`run` migration · G8 `--all-envs` confirmation UX · G9 idempotent cross-run skip.

## Phase boundaries

| In scope | Out of scope |
|----------|--------------|
| `--all-envs` flag; selection composition rules; dangling-edge pruning + `PrunedEdge` surfacing; in-plan promotion ordering as the single mechanism; mutating-`run` fail-closed behind a deprecation window | Cross-invocation source-status promotion gate (Option C); idempotent cross-run job skip; pruning-hardening (error-in-CI); a `--with-deps` closure mode; any built-in `local` environment |

## Relationship to `orun-catalog-state`

The cockpit env **selector** + component-scoped run ships in `orun-catalog-state`
(`environments.md`, CS6). This feature is consistent with it: the cockpit's
selection is a plan-time selection like any other, and `run` stays faithful.
`orun-catalog-state` deferred-register entry **L-1** pointed at the original epic;
this feature supersedes it with the converged Z model.
