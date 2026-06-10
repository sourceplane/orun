# Design — environment selection & in-plan promotion (the "Z" model)

> **Status: design converged.** Non-breaking except one intentional,
> deprecation-windowed change: a **mutating `orun run` becomes fail-closed**. What
> began as a breaking "single environment everywhere" epic converged — through the
> alternatives recorded in §8 — onto a small feature: **selection is a *plan-time*
> concern, the plan is executed faithfully, and the only new safety rule is that a
> mutating `run` must name what it touches.**

## Read order

1. **`design.md`** (this doc) — principle, model, decisions, enforcement surfaces, gaps, alternatives.
2. **`cli-surface.md`** — exact flag/behavior changes per command.
3. **`data-model.md`** — the plan selection-metadata block + the `PrunedEdge` record.
4. **`implementation-plan.md`** — milestones ES1…ES5.
5. **`test-plan.md`** — the correctness properties (fail-closed, faithful pruning, promotion ordering).
6. **`compatibility-and-migration.md`** — the additive flags + the one fail-closed break + its deprecation window.
7. **`risks-and-open-questions.md`** — decisions, open questions, risks, deferred register.

## 1. Principle

orun's plan is already a **contextual artifact** — compiled for a specific trigger
and change-set (`TriggerOccurrence.PlanScope{mode, activationMode,
activeEnvironments, changedComponents}`, `RevSummary{scope, activeEnvironments,
changedComponents}`, `--changed`). So **selection belongs at plan time**, and
`run` **faithfully executes the plan it is given**. The plan you review is exactly
what runs.

Safety comes not from a magic default environment but from **fail-closed
execution**: the absence of an explicit selection on a *mutating* run yields *no
mutation*, never the largest blast radius.

## 2. The model

### 2.1 Plan — selection happens here; defaults to all

- `orun plan` compiles a plan scoped by the selection flags + trigger + `--changed`.
- Selection flags (**all already exist** except `--all-envs`):
  - `--env <list>` — comma-separated environment filter (`command_plan.go`).
  - `--component <list>` — repeatable component filter (`command_plan.go`).
  - `--changed` — only changed components.
  - `--trigger <name>` — named trigger binding for env activation.
  - **`--all-envs`** — **NEW**, explicit "all environments". (`--all` is unavailable:
    it is a root persistent flag meaning "process all components / disable CWD
    scoping", `commands_root.go` — see §6.)
- **No selection → all environments** (a full plan). `plan` is read-only, so an
  all-envs default is safe and convenient for review/CI. `--all-envs` is the
  explicit, self-documenting synonym.
- The plan is faithful and self-contained (see §3 pruning).

### 2.2 Run — faithful execution, fail-closed (grounded in `--dry-run`)

`orun run` already carries `--env`, `--component`, `--changed`, and **`--dry-run`**
(`command_run.go`). The dry-run path is read-only and is the escape hatch for the
fail-closed rule.

- `orun run <plan>` (a pre-compiled plan) executes its scope as-is — the conscious
  choice was made at plan time.
- `orun run --dry-run` is **read-only**; it defaults to all environments and is
  always allowed (preview).
- A **mutating** `orun run` (no `--dry-run`) that would compile-and-execute with
  **no explicit selection** is **fail-closed**: it **errors** —
  `specify --env / --component, or --all-envs (or --dry-run to preview)`.
- A mutating run over **all** environments requires the explicit **`--all-envs`**
  acknowledgement; an interactive terminal MAY additionally prompt unless `--yes`.
- Net: a bare mutating `run` does **nothing** until you say what to touch.

### 2.3 No built-in `local` environment

A local sandbox is just a normally-declared environment (`--env dev`). No reserved
name, no auto-subscribe, no per-developer state machinery. Rationale in §8: the
`local` env was a solution to a *default-safety* problem that fail-closed `run`
solves more simply and more safely (it depends on nothing being correctly
sandboxed).

## 3. Selection semantics

- **Composition / precedence.** Envs = (trigger-activated envs) ∩ (`--env` /
  `--all-envs`); components = (`--component` / `--changed` / all). The narrowest
  explicit signal wins; an `--env` outside the trigger's activated set is an
  **error** (consistent with today's trigger ∩ explicit behavior in
  `trigger.ResolveActiveEnvironments`).
- **Component selection is exact** — selecting a component runs that component, not
  its dependency closure; dangling dependency edges are pruned (below). A
  `--with-deps` closure mode is a possible future affordance, not v1.
- **Dangling-edge pruning.** When a scoped plan contains an edge — a promotion
  `dependsOn` to another env, or a component `dependsOn` to another component —
  whose **endpoint is not in the expanded plan**, that edge is **dropped with a
  warning**, recorded as a `PrunedEdge` (`data-model.md` §3), so the scoped plan is
  self-contained and faithfully describes what `run` will do. Pruning applies
  **uniformly** to any dangling edge; dropped edges are printed at plan time and
  included in `--json`.

## 4. Promotion — in-plan ordering (Option B)

- An environment's `promotion.dependsOn` produces **ordering edges inside the
  plan** among the envs the plan contains: a dependent env's jobs run after its
  prerequisite env's, and a **failed prerequisite blocks its dependents** in the
  same run. This is the existing `internal/planner/promotion.go` same-plan
  behavior, now the **single** promotion mechanism.
- Promotion gating is therefore **within one `orun run`** of a plan that contains
  both envs. Cross-invocation / cross-pipeline gating is deferred (§9, Option C).

## 5. Decisions (converged)

| # | Decision |
|---|----------|
| **Z-1** | Selection is a **plan-time** concern; `run` executes the plan faithfully. |
| **Z-2** | `plan` (and `run --dry-run`) default to **all** environments (read-only; safe). |
| **Z-3** | A **mutating `run` is fail-closed**: no explicit selection → error; all-env mutating run requires `--all-envs`; `--dry-run` is the read-only escape. |
| **Z-4** | Selection flags: existing `--env <list>` + `--component <list>` + `--changed` + `--trigger`, plus new `--all-envs`. |
| **Z-5** | Promotion = **in-plan `dependsOn` ordering** (Option B); failed prerequisite blocks dependents. |
| **Z-6** | Scoped plans **prune dangling edges with a warning** (faithful, self-contained). |
| **Z-7** | **No built-in `local` env**; a sandbox is a normally-declared environment. |

## 6. Enforcement surfaces (what changes)

| Surface | Change |
|---------|--------|
| `--env` (`command_plan.go`, `command_run.go`) | unchanged behavior (comma-list); documented. Optionally also accept repeated `--env`. |
| `--component` (both commands) | **already exists** (repeatable); define its interaction with pruning (§3). |
| `--all-envs` | **NEW** explicit "all environments" on `plan`/`run`. Cannot reuse `--all` (taken by `commands_root.go` for component/CWD scoping). |
| `run` mutating path (`command_run.go`, `cmd/orun/main.go`, `internal/objrun`) | **fail-closed**: require selection or `--all-envs` when not `--dry-run`; the only breaking change (§10). |
| `internal/expand` + planner | prune dangling edges (endpoint not in expanded set), record `PrunedEdge`, warn; surface in plan output + `--json`. |
| `internal/planner/promotion.go` | keep same-plan ordering as the single promotion mechanism; cross-plan `Satisfy` modes go inert/removed for now (§9). |
| docs / CI | document `--all-envs` as the CI path; mutating `run` examples become explicit. |

## 7. Gaps & recommendations

| # | Gap | Recommendation |
|---|-----|----------------|
| **G1** | Run fail-closed vs. pre-compiled plan — exact error/execute rule. | Decision tree: `--dry-run` → run (read-only, all-default); mutating + no selection → error; mutating + selection/`--all-envs` → run; pre-scoped plan → run. |
| **G2** | Cross-invocation promotion ordering — Option B only orders within one `orun run`. | Document "one plan / one run for ordered envs"; the source-status gate (Option C) is the future upgrade (§9). |
| **G3** | Pruning is convention, not enforcement (warning only). | `--all-envs` is the documented CI path (never prunes). Later: escalate a pruned *promotion* edge to an **error in CI** / require explicit `--prune-deps`. |
| **G4** | Pruning semantics — what's "dangling," and does it cover component edges. | Uniform: any edge whose endpoint isn't in the expanded plan; record + list `PrunedEdge`s. |
| **G5** | Selector composition/precedence. | envs = trigger ∩ (`--env`/`--all-envs`); components = `--component`/`--changed`/all; `--env` outside trigger envs errors. |
| **G6** | `--component` exact vs. closure. | Exact + pruning in v1; `--with-deps` closure is a later affordance. |
| **G7** | Bare-mutating-`run` migration. | One-release **deprecation window**: warn ("future: a mutating `run` will require a selection or `--all-envs`") then error. |
| **G8** | `--all-envs` run confirmation UX. | CI/non-interactive: `--all-envs` is the ack; interactive: prompt unless `--yes`. |
| **G9** | Idempotent cross-run skip. | Future; reuse the revision-dedup + resume substrate (`objrun.go`, runner resume). |

## 8. Why Z — alternatives considered (rationale preserved)

- **Single-environment-everywhere epic** (one env per plan, mandatory
  `defaultEnvironment`, breaking) — over-engineered; broke the useful multi-env
  plan and forced N invocations.
- **Source-status-as-truth + idempotent skip + env-as-filter multi-env** — powerful
  but concentrated risk in a job-fingerprint key (cross-env vs same-env reads);
  too much machinery for the need.
- **Always-full plan, select-at-run** — clean, but **opposite to orun's
  plan-execute philosophy**: the plan must stay a contextual,
  trigger/changed-shaped artifact and `run` must faithfully execute it. Rejected.
- **Default `local` env + explicit `--all`** vs **all-by-default** — `local` is a
  *safe default* mechanism, but its safety depends on `local` actually being a
  sandbox (deferred to composition authors), i.e. *fail-to-sandbox*. **Z is
  fail-to-nothing**: equally scalable (flat, zero default blast radius), strictly
  safer (depends on nothing being sandboxed), and simpler (no `local` machinery).

## 9. Deferred / future

- **Option C — cross-invocation source-status promotion gate.** Component-level,
  keyed by source head: a dependent env's run reads prior executions (under the
  same revision / source head) to confirm the prerequisite env completed. Pulls in
  when split-across-pipelines promotion is needed (G2).
- **Idempotent cross-run job skip** (G9).
- **Pruning hardening** — pruned promotion edges → error in CI (G3).

## 10. Migration

- `plan --env` / `--component` / `--changed` / no-flag: **unchanged** (non-breaking).
- `--all-envs`: **additive**.
- **Mutating `orun run` fail-closed: the one break.** A bare mutating `orun run`
  that previously ran all environments now errors. Ship a one-release deprecation
  window (warn → error). `--dry-run` is unaffected. Full detail in
  `compatibility-and-migration.md`.
