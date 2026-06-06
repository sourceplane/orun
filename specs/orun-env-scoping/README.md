# Epic: orun-env-scoping

> ✅ **STATUS: CORE MODEL LOCKED — mechanics partially open; not yet greenlit for
> implementation.** The single-environment invariant, the resolution order, the
> `intent.defaultEnvironment` placement, and the built-in `local` environment are
> **decided** (see the decision ledger). The remaining run-path mechanics
> (promotion rework, CI fan-out, `local` state isolation, auto-subscribe, the
> "am-I-local" contract) and the supporting spec docs are **still open** — see
> `design.md` §5. `local`-env **safety enforcement is intentionally deferred**
> (`design.md` §6, decision B).

**Direction: every plan / run is scoped to exactly one environment — never
all-env, never multi-env — resolved as `explicit → intent.defaultEnvironment →
built-in local (interactive) → error (CI)`.** This is a **breaking run-path
change** that touches planning, triggers, promotion, and CI, which is why it is
its own epic rather than a rider on `specs/orun-catalog-state/`.

## Decision ledger (LOCKED)

See `design.md` §3 for the authoritative table.

| # | Decision |
|---|----------|
| ENV-1 | One environment per plan/run; no all-env / multi-env plan. |
| ENV-2 | No sole-env special case — one declared env resolves like many. |
| ENV-3 | Resolution order: explicit → `defaultEnvironment` → `local` (interactive) → error (CI). |
| ENV-4 | `intent.defaultEnvironment` is an **optional** top-level scalar, validated to name a real env. |
| ENV-5 | Trigger-bound env is authoritative; a conflicting `--env` is an error. |
| ENV-6 | Built-in `local` env: reserved, auto-subscribed, interactive-only fallback, excluded from promotion + activation. |
| ENV-7 | `local` uses composition `DefaultProfile`; **safety enforcement deferred** — composition author's duty (option B). |

These supersede the original open points carried from the cockpit review: **G-5**
(no-default resolution) and **G-9** (`defaultEnvironment` placement) are
**resolved**; **G-7** (triggers → one env) is resolved for *precedence* (ENV-5)
but its *fan-out* mechanics remain open (see G-old-2 below).

## Why an epic (not part of the cockpit spec)

The cockpit work (`specs/orun-catalog-state/`) needs to let a user pick an
environment and run a component in it. That much it does **on the existing env
model** (select one of `intent.environments`, run via the current `--env <one>`
path) — no schema change, no behavior change to `orun plan`/`run`. The *system-
wide* change — making single-env mandatory everywhere, adding the optional
`defaultEnvironment`, and introducing the built-in `local` env — is larger and
breaking, so it lives here.

## Relationship to `orun-catalog-state`

| Concern | Where |
|---------|-------|
| Cockpit env **selector** + component-scoped run on the **existing** model | `orun-catalog-state` (`environments.md`, CS6) — ships now |
| `intent.defaultEnvironment` schema + validation | **here** (locked: ENV-4) |
| Built-in `local` environment | **here** (locked: ENV-6/7; mechanics open) |
| Removing the no-`--env`=all-env default; deprecating `--env a,b` | **here** |
| Single-env enforcement in `plan`/`run`/triggers/CI/promotion | **here** |
| Migration + deprecation window | **here** (open — G-doc) |

`orun-catalog-state` deferred-register entry **L-1** points here. When this epic
lands, the cockpit's env selection feeds the locked resolution order (as the
explicit/TUI tier) with no UI change.

## Status

| Field | Value |
|-------|-------|
| Status | **Core model locked; mechanics + spec docs open; not yet greenlit to implement** |
| Type | Breaking run-path change (planning, triggers, promotion, CI) |
| Builds on / relates to | `specs/orun-catalog-state/` (cockpit selector on the existing model) |
| Read | `design.md` — current model, the locked target model, the decision ledger, enforcement surfaces, the remaining open gaps, the deferred `local`-safety options, and migration |

## Open gaps (remaining — `design.md` §5)

Ranked; the top three constrain the rest.

- **G-old-1** — promotion `same-plan` removal + how cross-plan gates read
  prior-env success, and migration of existing configs. *Largest.*
- **G-old-2** — trigger fan-out ownership: who runs the N single-env plans,
  per-plan provenance, ordering with promotion gates.
- **G-new-5** — `local` state isolation (no per-env state keying exists today; must
  not pollute shared state or promotion evidence). Now also a safety mechanism
  under decision B.
- **G-new-4** — auto-subscribe-all to `local` + opt-out + selector interaction.
- **G-new-6** — `local` env config/target (`parameterDefaults`, backend,
  `dependencyMode`, reserved-name collision, optional `environments.local`
  override).
- **G-new-3** — the "am I local?" contract (interactive vs CI; no TTY/CI signal
  exists today).
- **G-doc** — the missing spec docs (`data-model`, `cli-surface`,
  `implementation-plan`, `test-plan`, `compatibility-and-migration`) incl. the
  concrete deprecation window.

(G-8, the cockpit affected-overlay env filter, stays in `orun-catalog-state` — it
is a cockpit UX detail, not a run-path concern.)
