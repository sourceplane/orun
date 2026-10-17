# Design — orun-manual-triggers

## 1. The problem, in today's code

`orun plan --from-ci github --event-file …` normalizes the event
(`internal/trigger/normalize.go`; `workflow_dispatch` keeps the whole payload,
inputs included, in `NormalizedEvent.Raw`) and matches it against every
binding's `on:` block (`internal/trigger/match.go`). A binding contributes
three things to the plan: the environments it activates
(`environments.<env>.activation.triggerRefs`), its name to profile and
dependency rule matching (`when.triggerRef`), and its `plan` options —
`scope`, `base`, `head` (`model.TriggerPlanOptions`).

Nothing in that set can say *which components*. So a repository whose
platform dispatches a deploy (`inputs: {component, environment}`) cannot plan
it with its normal command:

- no binding claims `workflow_dispatch`, or if one does, the plan covers every
  component in every environment it activates;
- `--changed` on the CI command line stays on (a binding's `scope` can turn it
  on, never off, `cmd/orun/main.go` after trigger resolution), and a dispatch on
  `main` diffs `main` against its own tip: zero files, zero jobs;
- the binding's name matches no profile rule, so every lane falls back to its
  default profile — in practice `verify` where a merge would `deploy`;
- the plan-time catalog auto-push gates on a clean `main` checkout
  (`catalogAutoPublishScope`), which a dispatch on `main` is, so every click
  re-publishes a catalog that `main` already published.

orun-cloud works around all four with a hand-written branch in `ci.yml`
(`--trigger github-push-main --env "$E" --component "$C"`, no `--changed`,
`git checkout --detach`). This spec moves each workaround into the binding.

## 2. The model

```yaml
automation:
  triggerBindings:
    github-dispatch:
      on: { provider: github, event: workflow_dispatch, branches: [main] }
      plan:
        scope: selected
        components: inputs.component
        environments: inputs.environment
        autopush: false
        requireRule: true
```

| Option | Type | Default | Effect |
|---|---|---|---|
| `scope` | `full` \| `changed` \| `selected` | `full` | `selected`: the selection (§3) is the whole plan |
| `components` | event path | — | where the selected components are in the event (§3.2) |
| `environments` | event path | — | where the selected environments are (§3.2) |
| `autopush` | bool | unset (today) | `false`: no plan-time catalog push for this plan (§5) |
| `requireRule` | bool | `false` | a lane is planned only if a profile rule naming this binding matched it (§4) |

`base` and `head` are unchanged. Every option is per binding: a plan resolved
through `github-pull-request` or `github-push-main` sees none of them.

## 3. Selected plans

### 3.1 `scope: selected`

The plan is the selection — the named components in the named environments —
and nothing else:

- `--changed` is turned off and `--files` is ignored for this plan (each
  prints one line saying so). A dispatch has no diff to read; the request is
  the scope.
- The selection must be non-empty: no components, or no environments, is an
  error, never "everything".
- Every named component must exist. One unknown name fails the plan (unlike
  repeated `--component`, which drops a name that misses while another hits).
- Every named environment must be one the matched binding activates
  (the existing `intersectEnvs` check), else an error naming it.
- Every selected component × environment must be a lane: a component that does
  not subscribe to a named environment fails the plan ("web-docs does not
  subscribe to stage").

Where it applies: in `generatePlan` (`cmd/orun/main.go`), the block that today
turns `changedOnly` on for `scope: changed` also handles `selected` — it sets
`changedOnly = false`, clears `changedFiles`, and sets `planComponents` from
the resolution, so the existing component filter does the pruning. The
environments come back from `ResolveActiveEnvironments` already narrowed.

The plan's trigger occurrence records scope `selected`. The object model's
revision scope accepts `full | changed` (`internal/nodes/validate.go`); a
selected plan records `full` there — it is not diff-based — so no object-model
schema changes.

### 3.2 `components` and `environments`

Each is a dot path into the event payload, resolved by the same
`ResolvePath` that resolves `base` and `head`:

- a string is split on commas, items trimmed, empty items dropped
  (`"web-console-next, api-edge"` → two components);
- a JSON array of strings is taken as is;
- a missing path, `null`, or an empty result is an error naming the path
  (`ResolvePath` renders a non-string leaf with `%v`, so `null` would read
  `"<nil>"`; that is caught).

They are carried on `model.TriggerResolution` as `SelectedComponents` and
`SelectedEnvironments`. Under `--trigger <name>` there is no event: the
selection comes from `--component` and `--env`, with the same rules (both
required, every name must exist). That makes the dispatch plan reproducible
on a laptop:

```bash
orun plan --trigger github-dispatch --component deploy-worker --env stage
```

## 4. Opt-in lanes: `requireRule`

With `requireRule: true`, a lane — one component instance in one environment —
is planned under this binding only if one of its `profileRules` with
`when.triggerRef: <this binding>` matched it. Today's expander already records
that fact on the instance (`ProfileRuleTriggerRef`, set when a rule matched,
`internal/expand/expander.go` `resolveProfile`).

- The filter runs in `generatePlan` right after expansion, before the
  component filter, so dropped lanes leave the DAG the way any filtered
  instance does (`computePrunedEdges`).
- Under `scope: selected` it is strict: every selected lane must be enabled.
  One that is not fails the plan, naming each lane and the fix:
  `not enabled for github-dispatch: web-console-perf-budgets.stage — add a
  profileRule with when.triggerRef: github-dispatch`.
- Under another scope it only drops: the binding plans the lanes that opted in.
- When several bindings matched one event, a lane is kept if the rule that
  matched it names a binding with `requireRule`.

What a lane does when it opts in is the profile its rule names — `deploy` in
stage, `verify` in dev — so the component file says both *whether* and *what*.
A lane with no such rule is never planned by this binding, whatever its default
profile is. With `requireRule` unset or `false`, today's behaviour holds: a lane
with no matching rule runs its default profile.

## 5. `autopush: false`

`maybeAutoPushCatalog` (`cmd/orun/catalog_autosync.go`) runs after every
successful plan when `execution.state.autopushCatalog` is on and the checkout
is a clean `main`. A binding with `autopush: false` turns it off for plans it
resolved: `generatePlan` records the resolved binding's choice in a package
variable (the precedent is `lastPlanCounts`), and `orun plan` checks it before
calling `maybeAutoPushCatalog`. `--push-catalog` (an explicit request) still
pushes. Unset keeps today's behaviour. (The plan-time work sync that also
gated on `main` was removed in v2.82.0; there is nothing else to gate.)

## 6. The catalog's `manual` field

A component manifest's `spec.environments.<env>` gains an optional entry:

```json
"environments": {
  "dev":   { "profile": "verify", "active": true,
             "manual": { "trigger": "github-dispatch", "profile": "verify" } },
  "stage": { "profile": "verify", "active": true,
             "manual": { "trigger": "github-dispatch", "profile": "deploy" } },
  "prod":  { "profile": "verify", "active": true }
}
```

It is set for an environment whose subscription has a profile rule naming a
binding with `requireRule: true`; its `profile` is that rule's profile. With
several such bindings, the first by name. The catalog resolver
(`internal/catalogresolve`) learns to read `automation.triggerBindings`
(only the binding names and their `requireRule` flag) from the intent; the
subscription's `profileRules` are already parsed
(`catalogmodel.ComponentYAMLSubscribeEnvironment`). `orun catalog refresh` and
the plan-time push compute the same field, so the catalog does not flip
between them.

- `manual` is `omitempty`, so a component without it keeps its manifest hash.
- The field carries no job id: the resolver has no composition registry. A
  backend that needs the job reads it from runs (`<component>.<env>.<job>`).
- Inline intent components do not carry `profileRules` into the catalog
  today, so they get no `manual` field (§8).

## 7. Validation

A new `trigger.ValidatePlanOptions(intent)`, called where `orun plan` and
`orun validate` already call `ValidateProfileRules`:

- `scope` is one of `full`, `changed`, `selected`;
- `components` and `environments` are both set when `scope: selected`, and
  neither is set otherwise;
- an event path is non-empty and has no empty segment;
- `requireRule` and `autopush` are valid with any scope.

Errors name the binding and the field
(`trigger binding "github-dispatch": plan.components is required with scope: selected`).
The intent JSON schema (`assets/config/schemas/intent.schema.yaml` and the
preset schema) accepts the new keys and the `selected` scope.

## 8. Not built

- **Job-level selection inside a lane.** Every execution profile in practice
  selects one job, so component × environment already names a job. A profile
  with several jobs runs them all.
- **The TUI's plan pipeline** (`internal/tui/services/plan_service.go`) keeps
  named-trigger planning only.
- **Inline intent components** in the catalog's `manual` field.
- **Several environments in one dispatch** work (`environments` may name more
  than one), but a platform that records one deploy per environment should
  dispatch once per environment.
