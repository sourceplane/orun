# Design — environment selection & in-plan promotion (the "Z" model)

> **Status: design converged.** Non-breaking except one intentional,
> deprecation-windowed change: `orun run` becomes **fail-closed**. What began as a
> breaking "single environment everywhere" epic converged — through the
> alternatives recorded in §8 — onto a small feature: **selection is a *plan-time*
> concern, the plan is executed faithfully, and the only new safety rule is that a
> mutating `run` must name what it touches.**

## 1. Principle

orun's plan is already a **contextual artifact** — compiled for a specific trigger
and change-set (`TriggerOccurrence.PlanScope{mode, activationMode,
activeEnvironments, changedComponents}`, `RevSummary{scope, activeEnvironments,
changedComponents}`, `--changed`). So **selection belongs at plan time**, and
`run` **faithfully executes the plan it is given**. The plan you review is exactly
what runs.

Safety comes not from a magic default environment but from **fail-closed
execution**: the absence of an explicit selection yields *no mutation*, never the
largest blast radius. Default blast radius of a bare mutating `run` is **zero**.

## 2. The model

### 2.1 Plan — selection happens here; defaults to all

- `orun plan` compiles a plan scoped by the selection flags + trigger + `--changed`.
- Selection flags: **`--env <list>`** (comma-separated; already supported),
  **`--component <list>`** (new), and explicit **`--all`** (new).
- **No selection → all environments** (a full plan). `plan` is read-only, so an
  all-envs default is safe and convenient for review/CI. `--all` is the explicit,
  self-documenting synonym.
- The plan is faithful and self-contained (see §3 pruning).

### 2.2 Run — faithful execution, fail-closed

- `orun run <plan>` executes a **pre-compiled** plan as-is; its scope was the
  conscious choice made at plan time.
- The **convenience plan-and-run path** (an `orun run` that compiles implicitly)
  is **fail-closed**: with no explicit selection it **errors** —
  `specify --env, --component, or --all`.
- Running a **full (all-env) plan that mutates** requires explicit **`--all`** (the
  conscious acknowledgement); an interactive terminal MAY additionally prompt.
- Net: a bare mutating `run` does **nothing** until you say what to touch.

### 2.3 No built-in `local` environment

A local sandbox is just a normally-declared environment (`--env dev`). No reserved
name, no auto-subscribe, no per-developer state machinery. Rationale in §8: the
`local` env was a solution to a *default-safety* problem that fail-closed `run`
solves more simply and more safely (it depends on nothing being correctly
sandboxed).

## 3. Selection semantics

- **Composition.** Envs = (trigger-activated envs) ∩ (`--env` / `--all`);
  components = (`--component` / `--changed` / all). The narrowest explicit signal
  wins; an `--env` outside the trigger's activated set is an **error** (consistent
  with today's trigger ∩ explicit behavior).
- **Component selection is exact** — selecting a component runs that component, not
  its dependency closure; dangling dependency edges are pruned (below). (A
  `--with-deps` closure mode is a possible future affordance, not v1.)
- **Dangling-edge pruning.** When a scoped plan contains an edge — a promotion
  `dependsOn` to another env, or a component `dependsOn` to another component —
  whose **endpoint is not in the expanded plan**, that edge is **dropped with a
  warning**, so the scoped plan is self-contained and faithfully describes what
  `run` will do. Pruning applies **uniformly** to any dangling edge. Dropped edges
  are listed at plan time and in `--json`.

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
| **Z-2** | `plan` defaults to **all** environments (read-only; safe). |
| **Z-3** | `run` is **fail-closed**: a mutating run with no explicit selection errors; an all-env mutating run requires `--all`. |
| **Z-4** | Selection flags: `--env <list>` + `--component <list>` + explicit `--all`; trigger + `--changed` also shape the plan. |
| **Z-5** | Promotion = **in-plan `dependsOn` ordering** (Option B); failed prerequisite blocks dependents. |
| **Z-6** | Scoped plans **prune dangling edges with a warning** (faithful, self-contained). |
| **Z-7** | **No built-in `local` env**; a sandbox is a normally-declared environment. |

## 6. Enforcement surfaces (what changes)

| Surface | Change |
|---------|--------|
| `--env` flag (`cmd/orun/command_plan.go`) | keep comma-list; document; (optional) also accept repeated `--env`. |
| `--all` flag | **NEW** explicit "all environments" on `plan`/`run`. |
| `--component` flag | **NEW** plan-time component filter (alongside `--changed`). |
| `cmd/orun` run path (`main.go`, `objrun`) | make the implicit plan-and-run **fail-closed**; require selection / `--all` for mutating all-env runs. |
| `internal/expand` + planner | prune dangling edges (endpoint not in expanded set) with a warning; surface dropped edges in plan output + `--json`. |
| `internal/planner/promotion.go` | keep same-plan ordering as the single promotion mechanism; the cross-plan `Satisfy` modes go inert/removed for now (§9). |
| docs / CI | document `--all` as the CI path; `run` examples become explicit. |

## 7. Gaps & recommendations

| # | Gap | Recommendation |
|---|-----|----------------|
| **G1** | **Run fail-closed vs. pre-compiled plan** — exact rule for when `run` errors vs. executes. | Spec the decision tree: bare implicit run + no selection → error; pre-scoped plan → run; full mutating plan → require `--all`. |
| **G2** | **Cross-invocation promotion ordering** — Option B only orders within one `orun run`; split-across-pipelines isn't gated. | Document "one plan / one run for envs you want ordered" as the supported path; the source-status gate (Option C) is the future upgrade (§9). |
| **G3** | **Pruning is convention, not enforcement** — a scoped plan drops promotion gate edges with only a warning → unguarded if scoped plans are used in automation. | `--all` is the documented CI path (never prunes). Later: escalate a pruned *promotion* edge to an **error in CI** / require explicit `--prune-deps`. Warning now. |
| **G4** | **Pruning semantics** — what exactly is "dangling," and does it cover component edges too. | Uniform: any edge whose endpoint isn't in the expanded plan; list dropped edges at plan time + `--json`. |
| **G5** | **Selector composition / precedence** — how trigger + `--env` + `--component` + `--changed` + `--all` combine. | Define: envs = trigger ∩ (`--env`/`--all`), components = `--component`/`--changed`/all; `--env` outside trigger envs errors. |
| **G6** | **`--component` semantics** — new flag; closure vs. exact. | Exact selection + dangling-edge pruning (consistent with `--env`). `--with-deps` closure is a possible later affordance. |
| **G7** | **Bare-`run` migration** — behavior changes from "run all" to "error." | One-release **deprecation window**: warn ("future: `run` will require `--env`/`--component`/`--all`") then error. |
| **G8** | **`--all` run confirmation UX** — interactive prompt vs. CI explicit. | In CI / non-interactive, `--all` is the explicit ack; in an interactive terminal, prompt unless `--yes`. |
| **G9** | **Idempotent cross-run skip** — resume already skips completed jobs *within* a plan; skip across re-runs of the same plan is not in scope. | Future; reuse the revision-dedup + resume substrate (`objrun.go`, runner resume) when needed. |

## 8. Why Z — alternatives considered (rationale preserved)

Recorded so the path isn't re-litigated:

- **Single-environment-everywhere epic** (one env per plan, mandatory
  `defaultEnvironment`, breaking) — over-engineered; broke the useful multi-env
  plan and forced N invocations.
- **Source-status-as-truth + idempotent skip + env-as-filter multi-env** — powerful
  but concentrated risk in a job-fingerprint key (cross-env vs same-env reads); too
  much machinery for the need.
- **Always-full plan, select-at-run** — architecturally clean but **opposite to
  orun's plan-execute philosophy**: the plan must stay a contextual,
  trigger/changed-shaped artifact, and `run` must faithfully execute it. Rejected.
- **Default `local` env + explicit `--all`** vs **all-by-default** — `local` is a
  *safe default* mechanism, but its safety depends on `local` actually being a
  sandbox (deferred to composition authors), i.e. *fail-to-sandbox*. **Z is
  fail-to-nothing**: equally scalable (flat, zero default blast radius), strictly
  safer (depends on nothing being sandboxed), and simpler (no `local` machinery).

## 9. Deferred / future

- **Option C — cross-invocation source-status promotion gate.** Component-level,
  keyed by source head: a dependent env's run reads prior executions (under the
  same revision/source head) to confirm the prerequisite env completed. Pulls in
  when split-across-pipelines promotion is needed (G2).
- **Idempotent cross-run job skip** (G9).
- **Pruning hardening** — pruned promotion edges → error in CI (G3).

## 10. Migration

- `plan --env` / `--changed` / no-flag: **unchanged** (non-breaking).
- `--all`, `--component`: **additive**.
- **`orun run` fail-closed: the one break.** A bare `orun run` that previously ran
  all environments now errors. Ship a one-release deprecation window (warn → error)
  with a clear note: name the target (`--env`/`--component`) or `--all`.
