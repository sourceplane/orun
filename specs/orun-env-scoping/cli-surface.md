# CLI Surface

Exact behavioral changes for every affected `orun` command. The constraint: every
existing invocation keeps working, with **one** exception that ships behind a
deprecation window — a *mutating* `orun run` with no selection (§2.3). New surface
is otherwise additive.

Grounding: `--env`, `--component`, `--changed`, `--trigger`, and `--dry-run` are
**already implemented** (`cmd/orun/command_plan.go`, `cmd/orun/command_run.go`).
`--all` is **unavailable** — it is a root persistent flag meaning "process all
components / disable CWD scoping" (`cmd/orun/commands_root.go`). The
all-environments selector is therefore **`--all-envs`**.

---

## 1. `orun plan`

### 1.1 Selection flags

| Flag | Status | Meaning |
|------|--------|---------|
| `--env <list>` (`-e`) | exists | Comma-separated environment filter. |
| `--component <list>` | exists | Repeatable component filter. |
| `--changed` | exists | Only changed components. |
| `--trigger <name>` | exists | Named trigger binding for env activation. |
| **`--all-envs`** | **new** | Explicit "all environments" (self-documenting synonym of the default). |

### 1.2 Default

No env selection → **all environments** (a full plan). `plan` is read-only, so the
all-envs default is safe. `--all-envs` makes that explicit.

### 1.3 Pruning output

When the selection makes an edge dangling (its endpoint is not in the expanded
plan), `plan` **drops it with a warning** and lists it:

```
⚠ Dropped 2 dependency edges not in this plan's selection:
    promotion  staging → dev      (env "dev" not selected)
    component  api → shared-libs   (component "shared-libs" not selected)

✓ Plan compiled — envs: staging · components: api, web · jobs: 6
```

`--json` includes a `selection` block with `prunedEdges` (`data-model.md` §2–§3).

---

## 2. `orun run`

### 2.1 Selection flags

`--env <list>` (`-e`), `--component <list>`, `--changed`, and `--dry-run` already
exist. **`--all-envs`** is added (same meaning as on `plan`). `--yes` is added to
pre-acknowledge the interactive confirmation (§2.4).

### 2.2 Faithful execution

`orun run <plan>` executes a pre-compiled plan's scope as-is — selection was the
conscious choice made at plan time. No run-time re-selection beyond the existing
`--job` targeting (a debugging affordance, unchanged).

### 2.3 Fail-closed (the one guarded change)

The decision tree for the **compile-and-run** path:

```
--dry-run set?              → run (READ-ONLY; defaults to all envs)        [always allowed]
explicit selection given?   → run the selection                            (--env / --component)
--all-envs given?           → run all environments (mutating ack)
pre-compiled scoped plan?   → run the plan's scope
otherwise (mutating, none)  → ERROR:
    "refusing to run all environments implicitly.
     specify --env/--component, or --all-envs (or --dry-run to preview)."
```

During the deprecation window (`compatibility-and-migration.md` §3) the final
branch **warns and proceeds** instead of erroring.

### 2.4 `--all-envs` confirmation

- Non-interactive (CI): `--all-envs` is the explicit acknowledgement; proceeds.
- Interactive terminal: prompt `Run all N environments? [y/N]` unless `--yes`.

### 2.5 Promotion ordering

When the plan contains envs with `promotion.dependsOn` relationships, the run
orders dependents after prerequisites and **a failed prerequisite blocks its
dependents** (`design.md` §4). This holds within the single `orun run`.

---

## 3. Other commands

| Command | Change |
|---------|--------|
| `orun catalog affected` | unchanged (env-agnostic change detection). |
| cockpit run action | unchanged — passes one selected env (a plan-time selection); see `orun-catalog-state/environments.md`. |
| everything else | unchanged. |

---

## 4. New surface summary

| Surface | Status |
|---------|--------|
| `--all-envs` (`plan`, `run`) | new |
| `--yes` (`run`) | new |
| mutating `run` fail-closed | new behavior, deprecation-windowed |
| dangling-edge pruning + `--json prunedEdges` | new behavior |

No new top-level commands.

---

## 5. What NOT to add this feature

- Reusing `--all` for environments (collides with the root component-scoping flag).
- A `--with-deps` closure mode for `--component` (future; v1 is exact selection).
- A cross-invocation `--require-upstream` promotion gate (that is Option C,
  `risks-and-open-questions.md` L-1).
- Any `local`/built-in environment flag.
