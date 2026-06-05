# Design (captured points — to be finalized)

> A holding record of the env-scoping direction and the open points, captured
> from the `orun-catalog-state` review so they are not lost. **This is not a
> finalized design** — the resolution order, schema, enforcement surfaces, and
> migration all need a dedicated pass.

## 1. Current model (as-built — what we change)

- `intent.environments` is a `map[string]Environment`. There is **no
  `defaultEnvironment`** concept anywhere.
- The expander produces a `ComponentInstance` for **each environment × component
  pair** (`internal/expand/expander.go`).
- `--env` (`-e`) is a **filter** that accepts a **comma-separated** list.
- Therefore today: **no `--env` → all environments**; **`--env a,b` → multiple**.
- Trigger resolution (`trigger.ResolveActiveEnvironments`) can activate multiple
  environments; `Plan.ActiveEnvironments` is a list; promotion
  (`EnvironmentPromotion`) models env→env promotion.

## 2. Target model

- **Every plan / run is scoped to exactly one environment.** No all-env, no
  multi-env.
- **`intent.yaml` gains a default environment**, used when none is explicitly
  specified.
- **Resolution order (exactly one):** explicit (`--env <one>` / TUI selection /
  trigger-bound env) → `intent.defaultEnvironment` → *(proposed)* the sole env if
  exactly one is defined → else **error**.
- Multi-env workflows (CI matrix, promotion) become **N single-env runs**, not
  one multi-env plan.

## 3. Enforcement surfaces (everything that must change)

This is why it is a breaking, run-path-wide change:

| Surface | Change |
|---------|--------|
| `intent.yaml` schema + validation | add `defaultEnvironment` (or per-env `default`); validate it names a real env |
| `--env` flag (`plan`/`run`) | single value only; `a,b` → error; absence → default, not all-env |
| `internal/expand` expander | scope expansion to one env, not env × components |
| `trigger.ResolveActiveEnvironments` | resolve to exactly one env (or fan out to N single-env runs) |
| `Plan.ActiveEnvironments` | always length 1 |
| promotion (`EnvironmentPromotion`) | promotion = a gated run *in the target env*, not a multi-env plan |
| CI workflows / docs | multi-env pipelines become N single-env invocations |

## 4. Migration / deprecation

- The no-`--env`=all-env default and `--env a,b` are **breaking** to remove.
  Needs a deprecation window (warn → error) and a clear upgrade note (set
  `defaultEnvironment`; split multi-env pipelines into N runs).
- Existing `intent.yaml` files without a default must either get one or have every
  invocation specify `--env` — define the transition.

## 5. Open points (to finalize)

| # | Point | Options / note |
|---|-------|----------------|
| **G-5** | No-default resolution | hard error vs single-env fallback when exactly one env exists. Propose: fallback-then-error. |
| **G-6** | Multi-env CI / promotion ergonomics | N single-env runs is the model; offer a `--env each-of a,b` fan-out (still N *separate* single-env runs) or require N invocations? |
| **G-7** | Triggers → one env | trigger resolution must produce exactly one env, or fan out; interacts with promotion gates. |
| **G-9** | `defaultEnvironment` placement | top-level `intent.defaultEnvironment` (recommended — one source of truth, easily validated) vs per-env `default: true`. |
| **G-10** | Promotion model | confirm staging→prod is a gated run *in the target env*, not a multi-env plan. |

## 6. What ships before this epic (in `orun-catalog-state`)

The cockpit gets an **env selector** (key `e`) over the **existing** env model
and runs **component-scoped for one selected env** via the current run path — no
schema change, no removal of the all-env path. This epic supersedes that with the
finalized semantics when it is promoted. See `orun-catalog-state/environments.md`.
