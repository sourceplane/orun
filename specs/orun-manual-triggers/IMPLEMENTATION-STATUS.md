# orun-manual-triggers — implementation status

| Milestone | Status | What shipped |
|---|---|---|
| **MT0** | ✅ #781 | The design: selected plans (scope, components, environments), `autopush`, opt-in lanes (`requireRule`), the catalog's `manual` field, validation and compatibility |
| **MT1** | ✅ #782 | `scope: selected` with `components` / `environments` event paths (`trigger.ResolveList`: a comma list or a list of strings, never empty), the selection exact and strict in `generatePlan` (`selectInstances`: no wildcards, no unknown component, every component subscribes to every selected environment), `--changed` / `--files` ignored for it, the named-trigger fallback to `--env` / `--component`; `autopush: false` (`autoPushAfterPlan`); `trigger.ValidatePlanOptions` in `orun plan` and `orun validate`; the revision records a selected plan as `full`; the intent schemas; trigger-bindings and orun-plan docs |
| **MT2** | ✅ #783 | `plan.requireRule`: after expansion `generatePlan` keeps, under a requireRule binding, only lanes whose profile was chosen by a rule naming it (`requireRuleFilter`, on the expander's `ProfileRuleTriggerRef`); strict under `scope: selected` (every selected lane that did not opt in is named with the fix), dropping otherwise and failing only when nothing is left. The catalog resolver reads which bindings set `requireRule` and sets `spec.environments.<env>.manual` (`trigger`, `profile`) from the legacy `subscribe` profile rules (`applyManualLanes`), `omitempty`, before the manifest hash; the intent schemas; trigger-bindings and profile-rules docs |
| **MT3** | ✅ #784, [v2.84.0](https://github.com/sourceplane/orun/releases/tag/v2.84.0) | Release notes v2.84.0 and the sidebar entry, the CHANGELOG row; released as v2.84.0 from `main` |

## Adoption

sourceplane/orun-cloud adopted v2.84.0 in its saas-manual-runs epic:

- **MN2** (sourceplane/orun-cloud#1971): a `github-dispatch` binding with `scope: selected`, `components: inputs.component`, `environments: inputs.environment`, `autopush: false` and `requireRule: true`, plus one opt-in rule per lane. Its `ci.yml` now runs one plan command for every event.
- **Verified locally, before the merge**:
  - pull-request and merge plans were byte-identical to the previous ones (133 and 85 jobs);
  - a dispatch per environment planned exactly the opted-in lanes;
  - a lane that did not opt in was refused with the `requireRule` message.
- **Verified on production, after the merge**:
  - the merge's catalog push carries `spec.environments.<env>.manual`; for example, deploy-worker shows `dev → verify` and `stage`/`prod → deploy`;
  - the platform projects that field and gates manual runs on it (MN3 sourceplane/orun-cloud#1970, MN4 #1973).
