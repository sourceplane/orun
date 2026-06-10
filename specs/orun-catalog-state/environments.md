# Environment selection (cockpit — on the existing model)

> The cockpit lets a user pick an environment and run a component in it, **using
> the env model that exists today** — no schema change, no change to
> `orun plan`/`run` behavior. The larger direction (single-env everywhere, an
> `intent.yaml` default environment, removing the all-env path) is a **breaking
> run-path change captured in its own epic: `specs/archive/orun-env-scoping/`** (deferred
> register L-1). This doc covers only what the TUI does now.

## 1. What the cockpit does now

- **Selected-env context.** The cockpit holds one **selected environment**,
  chosen from the workspace's existing `intent.environments`. A key (propose `e`)
  selects/cycles it; the choice is remembered in TUI prefs (`internal/tui/prefs.go`).
- **Default selection (pragmatic, no schema).** On first open the selected env
  defaults to the first environment (deterministic, sorted) or the last-used from
  prefs. *(The real `defaultEnvironment` lives in the env epic — not introduced
  here.)*
- **Env-scoped run (the action seam).** Running from the component/job level is
  **component-scoped for the selected env**, via the **existing** run path
  (`internal/objrun`, the same as `orun run` with a single `--env`). Only
  components active in the selected env (`Subscribe` / `ComponentEnvironment.Active`)
  are offered to run; others are disabled for that env.

## 2. What this spec does NOT do (→ `orun-env-scoping` epic)

- Add `intent.defaultEnvironment` (schema/validation).
- Remove the no-`--env`=all-env default or deprecate `--env a,b`.
- Enforce single-env in `orun plan`/`run`/triggers/CI/promotion.
- Any env migration / deprecation window.

The cockpit run action simply passes **one** selected env to the existing run
path — it does not police what `plan`/`run` accept elsewhere. When the env epic
lands, the cockpit's selection feeds the finalized resolution order with no UI
change.

## 3. Affected overlay × env (cockpit UX)

The `internal/affected` engine stays **env-agnostic** (change detection is
env-independent — catalog identity is never an env). The selected env is only a
**downstream filter on what runs**. The cockpit MAY also filter/grey the affected
overlay to the selected env (`affected ∩ active-in-env`) — a UX call (G-8, kept in
this spec). This needs no env-model change.
