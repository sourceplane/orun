# orun-manual-triggers — implementation status

| Milestone | Status | What shipped |
|---|---|---|
| **MT0** | ✅ #781 | The design: selected plans (scope, components, environments), `autopush`, opt-in lanes (`requireRule`), the catalog's `manual` field, validation and compatibility |
| **MT1** | ⏳ | `scope: selected` with `components` / `environments` event paths (`trigger.ResolveList`: a comma list or a list of strings, never empty), the selection exact and strict in `generatePlan` (`selectInstances`: no wildcards, no unknown component, every component subscribes to every selected environment), `--changed` / `--files` ignored for it, the named-trigger fallback to `--env` / `--component`; `autopush: false` (`autoPushAfterPlan`); `trigger.ValidatePlanOptions` in `orun plan` and `orun validate`; the revision records a selected plan as `full`; the intent schemas; trigger-bindings and orun-plan docs |
| **MT2** | — | |
| **MT3** | — | |
