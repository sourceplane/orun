# Spec: orun-manual-triggers (a trigger that carries its own selection)

**A person asks for one component in one environment, and the plan should be
exactly that — chosen by the event, not by a branch of shell in CI.** Today a
GitHub `workflow_dispatch` event matches no trigger binding, so a repository
that dispatches deploys writes a second plan command for it: borrow another
trigger by name (`--trigger github-push-main`), pass the component and the
environment as flags, leave out `--changed`, and detach `HEAD` so the plan does
not re-push the catalog. The selection lives in shell, the profiles are
borrowed from a trigger that means something else, and nothing in a component
says whether it may be run by hand at all.

This spec gives a trigger binding four new `plan` options and the catalog one
new field:

```yaml
# intent.yaml
automation:
  triggerBindings:
    github-dispatch:
      description: A deploy or a run asked for by a person
      on: { provider: github, event: workflow_dispatch, branches: [main] }
      plan:
        scope: selected                    # the event's inputs are the whole plan
        components: inputs.component       # where the components are in the event
        environments: inputs.environment   # where the environment is
        autopush: false                    # no plan-time catalog push
        requireRule: true                  # a lane runs only if it opts in
```

```yaml
# component.yaml — the lane opts in, and says what a manual run does
subscribe:
  environments:
    - name: stage
      profile: verify
      profileRules:
        - profile: deploy
          when: { triggerRef: github-push-main }
        - profile: deploy                  # a person may deploy this to stage
          when: { triggerRef: github-dispatch }
```

```yaml
# .github/workflows/ci.yml — one plan command for every event, dispatch included
- run: orun plan --from-ci github --event-file "$GITHUB_EVENT_PATH" --changed --output plan.json
```

The catalog then says, per component and environment, what a manual run does:
`spec.environments.stage.manual: { trigger: github-dispatch, profile: deploy }`,
so a backend can offer a button only where a lane opted in.

## Backward compatibility is the first requirement

- A binding without the new options plans exactly as today: no option has an
  effect unless it is written. Every existing golden plan, checksum and
  revision key is unchanged.
- A component with no rule naming a `requireRule` binding gets no `manual`
  field, so its catalog manifest hash is unchanged.
- `--trigger`, `--component`, `--env`, `--changed` and `--files` keep their
  meaning for every other binding.

## Read order

1. `design.md` — the options, the selection, opt-in lanes, the catalog field,
   validation, compatibility, what is not built.
2. `implementation-plan.md` — MT0–MT3 with "done when".
3. `IMPLEMENTATION-STATUS.md` — as built.

## Milestones at a glance

| Milestone | One line |
|---|---|
| MT0 | This spec. |
| MT1 | Selected plans: `scope: selected`, `components`, `environments`, `autopush`; their validation and the intent schema; docs. |
| MT2 | Opt-in lanes: `requireRule`, and the catalog's `manual` field; docs. |
| MT3 | Release notes, CHANGELOG, and the release (v2.84.0). |

The first adopter is orun-cloud (its `saas-manual-runs` epic): Orunbase's
Deploy button dispatches `ci.yml`, and with this spec that workflow keeps one
plan command and each component decides which of its lanes a person may run.
