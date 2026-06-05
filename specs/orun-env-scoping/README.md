# Epic: orun-env-scoping

> ⚠️ **STATUS: EPIC — TO BE DESIGNED / FINALIZED. Not ready for implementation.**
> This is a holding epic that captures a product direction and the open points
> behind it. The design, schema, migration, and deprecation window are **not yet
> settled** and will be worked in a later revision. Nothing here is built until
> this epic is promoted to a ready spec.

**Direction: every plan / run is scoped to exactly one environment — never
all-env, never multi-env — resolved from an explicit selection else an
`intent.yaml` default environment.** This is a **breaking run-path change** that
touches planning, triggers, promotion, and CI, which is why it is its own epic
rather than a rider on `specs/orun-catalog-state/`.

## Why an epic (not part of the cockpit spec)

The cockpit work (`specs/orun-catalog-state/`) needs to let a user pick an
environment and run a component in it. That much it does **on the existing env
model** (select one of `intent.environments`, run via the current `--env <one>`
path) — no schema change, no behavior change to `orun plan`/`run`. The *system-
wide* change — making single-env mandatory everywhere and adding
`defaultEnvironment` — is larger and breaking, so it is captured here to be
designed and finalized separately.

## Relationship to `orun-catalog-state`

| Concern | Where |
|---------|-------|
| Cockpit env **selector** + component-scoped run on the **existing** model | `orun-catalog-state` (`environments.md`, CS6) — ships now |
| `intent.defaultEnvironment` schema + validation | **here** (epic) |
| Removing the no-`--env`=all-env default; deprecating `--env a,b` | **here** (epic) |
| Single-env enforcement in `plan`/`run`/triggers/CI/promotion | **here** (epic) |
| Migration + deprecation window | **here** (epic) |

`orun-catalog-state` deferred-register entry **L-1** points here.

## Status

| Field | Value |
|-------|-------|
| Status | **Epic — to be designed/finalized; do not implement** |
| Type | Breaking run-path change (planning, triggers, promotion, CI) |
| Builds on / relates to | `specs/orun-catalog-state/` (cockpit selector on the existing model) |
| Read | `design.md` — the captured points: current model, target model, enforcement surfaces, gaps, migration concerns |

## Open points (to finalize in the revision)

Carried verbatim from the cockpit-spec review so nothing is lost (see
`design.md` for detail):

- **G-5** no-default resolution — hard error vs single-env fallback.
- **G-6** multi-env CI / promotion → N single-env runs; want a `--env each-of`
  fan-out (still N single-env runs) or are N invocations fine?
- **G-7** triggers must resolve to one env (or fan out) — interacts with promotion
  gates.
- **G-9** `defaultEnvironment` placement — top-level field vs per-env
  `default: true`.
- **G-10** promotion model — staging→prod becomes a gated run *in the target
  env*, confirm.

(G-8, the cockpit affected-overlay env filter, stays in `orun-catalog-state` — it
is a cockpit UX detail, not a run-path concern.)
