# Implementation Plan — orun-manual-triggers

Milestones MT0–MT3. Each is one reviewable pull request that leaves `orun`
releasable, and none changes behaviour for an intent that does not use the new
options. The order: **decide → select → opt in → release.**

---

## MT0 — the design

**Scope.** This spec: README, design, this plan, the status file.
**Done when.** The spec is merged and every later milestone can point at a
section of design.md.

---

## MT1 — selected plans

**Scope.**
- `model.TriggerPlanOptions` gains `Components`, `Environments` (event
  paths), `AutoPush *bool`; `PlanScopeSelected` beside full and changed.
- `trigger.ValidatePlanOptions` (design §7), called by `orun plan` and
  `orun validate`; the intent and preset JSON schemas accept the keys.
- `resolveEventFile` resolves the selection from the event (§3.2) onto
  `TriggerResolution.SelectedComponents` / `SelectedEnvironments`, environments
  intersected with the binding's activation; `resolveNamedTrigger` takes the
  selection from `--component` / `--env` for a selected binding.
- `generatePlan`: `selected` turns `--changed` off, ignores `--files`, sets the
  component filter, and fails on an empty selection, an unknown component, or a
  component that does not subscribe to a selected environment (§3.1). The
  object model's revision scope records `full` for a selected plan.
- `autopush: false` skips `maybeAutoPushCatalog` for the plan (§5).
- Docs: `concepts/trigger-bindings.md` (plan scope, the new options, an example
  for `workflow_dispatch`), `cli/orun-plan.md` (the selected scope and the
  flags under it).

**Done when.**
- Trigger tests cover: path resolution (string, comma list with spaces, array,
  missing, `null`, empty), environment intersection, named-trigger fallback.
- Validation tests cover every rule in §7.
- A plan end-to-end test with a `workflow_dispatch` event file plans exactly the
  named component in the named environment with `--changed` on the command line;
  fails on an empty input, an unknown component, an environment the binding does
  not activate, and a component that does not subscribe to it.
- An autopush test: a binding with `autopush: false` does not push; unset does.
- Every existing test and golden plan is unchanged.

---

## MT2 — opt-in lanes and the catalog field

**Scope.**
- `requireRule` on `TriggerPlanOptions`; after expansion `generatePlan` keeps a
  lane under a `requireRule` binding only if a rule naming such a binding
  matched it; under `scope: selected`, a selected lane that is not enabled fails
  the plan with the lanes and the fix (§4).
- The catalog resolver reads the binding names and `requireRule` from the
  intent and sets `spec.environments.<env>.manual` (`trigger`, `profile`) from
  the subscription's profile rules (§6); `omitempty`.
- Docs: `concepts/trigger-bindings.md` (`requireRule`), `concepts/profile-rules.md`
  (opting a lane in), the catalog manifest reference (`manual`).

**Done when.**
- A plan end-to-end test: of three lanes — one opted in to `deploy`, one opted
  in to `verify`, one with no rule — a selected plan of the first two plans
  them with those profiles; a selected plan naming the third fails with the
  message; the same intent planned by a non-`requireRule` binding is unchanged.
- Catalog tests: the field appears for opted-in environments with the rule's
  profile, is absent otherwise, and a component without it keeps its manifest
  hash; `catalog refresh` and the plan-time resolve agree.

---

## MT3 — release

**Scope.** `website/docs/release-notes/v2.84.0.md` and its sidebar entry, the
CHANGELOG row, and the release from `main` (the `release-oci` workflow with
`v2.84.0`).

**Done when.** `v2.84.0` is published with its binaries and provider image,
and orun-cloud can pin it.
