# Backward Compatibility and Migration

This feature lands **additively**, with a single exception that ships behind a
deprecation window: a *mutating* `orun run` with no selection (§3). `--dry-run` and
all `plan` behavior are unaffected.

---

## 1. What keeps working (unchanged)

| Workflow | Status | Notes |
|----------|--------|-------|
| `orun plan` (no flags) | preserved | Compiles a full, all-envs plan (read-only). Now also embeds `metadata.selection`. |
| `orun plan --env a,b` | preserved | Same filtering as today; populates `selection`. |
| `orun plan --component x` | preserved | Repeatable component filter, unchanged. |
| `orun plan --changed` / `--trigger n` | preserved | Unchanged. |
| `orun run --dry-run` | preserved | Read-only preview; defaults to all envs; never warns/errors under the new guard. |
| `orun run --env a` / `--component x` | preserved | Runs the selection. |
| `orun run <plan>` | preserved | Executes the pre-compiled plan's scope faithfully. |
| `orun run --job <id>` | preserved | Targeted execution affordance, unchanged. |
| existing `promotion.dependsOn` intents | preserved | Realized as in-plan ordering; cross-plan `Satisfy` modes become inert (no error). |

---

## 2. Additive surface

- **`--all-envs`** on `plan`/`run` — explicit "all environments". (`--all` is not
  reused; it is the root component/CWD-scoping flag, `commands_root.go`.)
- **`--yes`** on `run` — pre-acknowledge the `--all-envs` interactive prompt.
- **`metadata.selection`** + **`prunedEdges`** in plan output and `--json` — new
  fields; consumers that ignore unknown fields are unaffected.
- **Pruning warnings** — emitted to stderr; do not change exit codes.

---

## 3. The one break: mutating `run` fail-closed

A bare **mutating** `orun run` (no `--dry-run`, no selection, no `--all-envs`)
today runs **all environments**. Under this feature that becomes an error. It ships
in two phases:

| Phase | Behavior of bare mutating `orun run` | Ships |
|-------|--------------------------------------|-------|
| **A — warn** | Prints `deprecated: a mutating 'orun run' will soon require --env/--component or --all-envs; running all environments for now`, then **runs all** (today's behavior). | this feature (ES4) |
| **B — error** | Errors: `refusing to run all environments implicitly; specify --env/--component, or --all-envs (or --dry-run to preview)`. Exit non-zero. | one release later |

Upgrade note for users: add a selection (`--env`/`--component`), or `--all-envs`
for a deliberate full run, or `--dry-run` to preview. `--dry-run` is exempt in both
phases.

CI guidance: the recommended automated path is **one plan / one run** with
`--all-envs` (full, ordered, never prunes) — see `design.md` §4.

---

## 4. Intent-file compatibility

- No `intent.yaml` change is required. `promotion.dependsOn` keeps working as
  in-plan ordering.
- The cross-plan `Satisfy` fields (`previous-success`, `same-plan`) are **not
  removed** from the schema (avoids an intent break); they become **inert** until
  Option C (`risks-and-open-questions.md` L-1) revives cross-plan semantics. A
  one-line notice is emitted if a plan would have relied on them across
  invocations.

---

## 5. When does this delete or rewrite anything?

Never. No on-disk state, no intent field, and no legacy flag is removed. The only
behavioral removal is "bare mutating `run` = run everything", and only after the
Phase A → Phase B window.
