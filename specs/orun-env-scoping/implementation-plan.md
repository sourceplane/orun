# Implementation Plan

Milestone-based, like the sibling specs. Each milestone states its **goal**,
**dependencies**, **suggested PR scope**, and **"done when"** criteria. Milestones
ES1–ES3 are **additive and non-breaking**; ES4 carries the one behavior change
(fail-closed `run`) and ships behind a deprecation window; ES5 is the gate.

Ordering rationale (`design.md` §7 recommendation): make the new capability
available additively first, surface pruning, consolidate promotion, and only then
flip the safety guard.

---

## Milestone ES1 — `--all-envs` + selection plumbing (additive)

**Goal:** explicit all-environments selection, and a single internal
representation of "what was selected", with no behavior change to existing flags.

- Add `--all-envs` (bool) to `plan` and `run` (`command_plan.go`,
  `command_run.go`). Reject combining `--all-envs` with `--env` (mutually
  exclusive) with a clear error.
- Introduce an internal `Selection{Envs, Components, AllEnvs, Mode}` value computed
  once from the flags + trigger + `--changed`, threaded into the expander.
- Populate `metadata.selection` (`data-model.md` §2) in the compiled plan;
  `envs`/`components`/`mode`/`allEnvs` only (no pruning yet).
- Document `--all-envs` in help text and the README quickstart.

**Dependencies:** none.

**Suggested PR scope:** 1 PR.

**Done when:**
- `orun plan` / `orun run --dry-run` behave identically to today when no new flag
  is passed.
- `orun plan --all-envs` and `orun plan --env a,b` both populate
  `metadata.selection` correctly; `--all-envs --env x` errors.
- Unit tests for `Selection` composition (`design.md` §3 precedence).

**Design sections:** `design.md` §2–§3, `data-model.md` §2.

---

## Milestone ES2 — dangling-edge pruning + surfacing

**Goal:** a scoped plan is self-contained; dropped edges are visible.

- In `internal/expand` / the planner, after expansion, drop any edge whose
  endpoint is not in the expanded set (uniformly: promotion `dependsOn` to an
  unselected env, and component `dependsOn` to an unselected component).
- Record each as a `PrunedEdge` (`data-model.md` §3); attach to
  `metadata.selection.prunedEdges` (sorted, deterministic).
- Print the warning block at plan time (`cli-surface.md` §1.3); include
  `prunedEdges` in `orun plan --json`.

**Dependencies:** ES1.

**Suggested PR scope:** 1 PR.

**Done when:**
- A `plan --env staging` over a `dev→staging→prod` intent drops the `staging→dev`
  promotion edge with a warning, and the plan runs standalone.
- Golden test on the warning text + `--json prunedEdges` ordering.
- A full plan emits `mode:"full"`, `prunedEdges: []`.

**Design sections:** `design.md` §3, `data-model.md` §3, `cli-surface.md` §1.3.

---

## Milestone ES3 — in-plan promotion as the single mechanism

**Goal:** promotion ordering works for any plan that contains the related envs;
cross-plan `Satisfy` modes are made inert.

- Confirm/consolidate `internal/planner/promotion.go` so `promotion.dependsOn`
  produces in-plan ordering edges for the envs present, and a **failed
  prerequisite blocks its dependents** within the run.
- Make the cross-plan `Satisfy` modes (`previous-success`, `same-plan`) inert with
  a one-line notice (they are superseded; Option C will revive cross-plan
  semantics). Do not delete the schema fields (avoids an intent break).

**Dependencies:** ES1 (selection), ES2 (pruning interacts with promotion edges).

**Suggested PR scope:** 1 PR.

**Done when:**
- `plan --all-envs` over `dev→staging→prod` orders jobs dev→staging→prod; a
  failing `dev` job blocks `staging`/`prod` dependents in the run.
- A scoped `plan --env prod` prunes the promotion edge (ES2) and warns; prod runs
  without dev/staging.
- Existing promotion tests updated; no `Satisfy`-mode regressions surface as
  errors (inert, not failing).

**Design sections:** `design.md` §4, `data-model.md` §4.

---

## Milestone ES4 — mutating `run` fail-closed (deprecation-windowed)

**Goal:** the safety guarantee — a mutating `run` never touches all envs
implicitly.

- In the compile-and-run path (`command_run.go` / `cmd/orun/main.go` /
  `internal/objrun`), implement the §2.3 decision tree.
- **Phase A (this milestone):** when a mutating run has no selection and no
  `--all-envs`, **warn** and proceed (preserves today's behavior) — the
  deprecation window.
- Add `--yes`; interactive confirmation for `--all-envs`
  (`cli-surface.md` §2.4).
- `--dry-run` path is explicitly exempt (read-only, all-default).

**Dependencies:** ES1.

**Suggested PR scope:** 1 PR (Phase A). A follow-up PR flips warn → error
(Phase B) after one release (`compatibility-and-migration.md` §3).

**Done when:**
- `orun run` (mutating, no flags) prints the deprecation warning and still runs
  all envs.
- `orun run --all-envs` runs all (with confirmation/`--yes` rules).
- `orun run --env staging` runs only staging.
- `orun run --dry-run` is unchanged and never warns.

**Design sections:** `design.md` §2.2, `cli-surface.md` §2.3–§2.4,
`compatibility-and-migration.md` §3.

---

## Milestone ES5 — end-to-end + docs

**Goal:** lock the properties as regression tests; update user docs.

- `cmd/orun/envscoping_e2e_test.go` — full walk: `plan` (full, scoped, `--all-envs`)
  → assert `metadata.selection` + warnings → `run` (each fail-closed branch).
- Promotion ordering e2e on `dev→staging→prod`.
- Update README / docs / CI examples to use `--all-envs` as the CI path and
  explicit `run` selections.

**Dependencies:** ES1–ES4.

**Suggested PR scope:** 1 PR.

**Done when:**
- `make test` green; the e2e covers every `test-plan.md` property.
- Docs/CI examples updated; no example relies on bare mutating `run`.

**Design sections:** `test-plan.md` (entire).

---

## Cross-cutting requirements

- New flags are additive; existing invocations unchanged except ES4 Phase B.
- Warnings go to stderr; `--json` output stays stable-key-ordered with a trailing
  newline.
- No `panic()` in production paths; errors flow through `errors.Is`/`As`.
- `prunedEdges` and `selection` are byte-deterministic (sorted).

## Out of scope (all milestones)

- Cross-invocation source-status promotion gate (Option C).
- Idempotent cross-run job skip.
- Pruning hardening (error-in-CI on pruned promotion edges).
- A `--with-deps` closure mode.
- Any built-in `local` environment.
