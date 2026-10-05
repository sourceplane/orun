# orun-docs-discipline — Review findings

A review of every page under `website/docs/` (release notes excepted, about
98 pages and 91,000 words), the README, `context-for-ai/`, `agents/`,
`skills/`, and the binary's root help text, against the four pillars and
against the code. Line numbers are as of v2.72.1.

## Taglines in use

Seven framings, none of which mentions standards, discipline, or agents
staying within them:

- `cmd/orun/commands_root.go:77-78`: "Plan and run changes from intent" / "turns intent into deterministic plans…"
- `website/docusaurus.config.js:7,46` and `README.md:3,18`: "the intent compiler for platform engineering"
- `website/docs/intro.mdx:8-9,111`: "planner · cockpit · runtime", "Plan once. Run anywhere. Operate from one cockpit.", "a planner and a cockpit"
- `website/docs/overview/what-is-orun.md:16-22,166`: a Kubernetes-for-delivery analogy; "what a compiler is to a program"
- `website/docs/overview/how-orun-works.md:8-11`: "three artifacts, three verbs"
- `website/docs/principles.md:5-6`: "planning, observation, and execution all spoke the same language"
- `context-for-ai/00-orun-repo-philosophy.md:3`, `ai-context/orun-repositories.md:9`: "a component-native desired-state repository"

## Verdicts by page

REWRITE = framing is central and must change. REFRAME = keep the body;
change intro, positioning, and links. KEEP = minor touches.

| Milestone | REWRITE | REFRAME | KEEP |
|---|---|---|---|
| PD1 | `intro.mdx`, `overview/what-is-orun.md`, README header and "Why orun", `cli/orun.md` opening | `principles.md` | `overview/glossary.md` |
| PD2 | `concepts/intent-model.md`, `concepts/compositions.md` | trigger-bindings, profile-rules, dependency-rules, environment-promotion, `compositions/*` | runtime-environment, secrets, `reference/*` |
| PD3 | `concepts/intent-presets.md` | stacks, baselines, `compositions/writing-compositions.md`, CLI baseline/new/compositions intros | bootstrap guide, pack, publish, fetch, login |
| PD4 | `ai-context/orun-repositories.md`, `concepts/agent-runtime.md` (intro) | task-plane, service-catalog, CLI mcp/agent/work | `context-for-ai/` (fixes), skills, task, spec, pr, githooks |
| PD5 | `examples/remote-state-matrix.md` | how-orun-works, plan-dag, execution-model, workflow-actions, change-detection (+ change-watches), cockpit/overview, execute/runners, quick-start, review-pull-request | state-model, architecture/*, cockpit/architecture, run/plan/validate and the observe commands |
| PD6 | — | — | everything else: front matter, versions, jargon |

## Factual errors and stale claims

### Declare (PD2)

1. **Policies are described as enforced; the code does not enforce them.**
   `concepts/intent-model.md:96,217`, `concepts/compositions.md:145-155`,
   `concepts/intent-presets.md:149,201`, `principles.md` (policy at compile
   time), README:87. Group and environment `policies` are resolved onto each
   instance (`internal/expand/expander.go:124,446`) and merged by presets
   (`internal/preset/merge.go:155,204`); profile `policies` are copied
   (`internal/composition/registry.go:941`). Nothing in the planner, plan
   model, `validate`, or runner reads either.
2. **Cross-plan promotion gates are recorded, not enforced**
   (`environment-promotion.md:105-107` says so), but `plan-dag.md:91` and
   `environment-promotion.md:223-224` read as enforced.
3. Job ids appear as `component@env.job` (`plan-dag.md:35`,
   `dependency-rules.md:149`, `environment-promotion.md:70`) and
   `component.env.job` (`plan-dag.md:77`, `runtime-environment.md:134`); the
   planner emits `component.env.job` (`internal/planner/planner.go`).
4. `overview/how-orun-works.md:39-64` and `overview/resource-model.md:52-63`
   wrap intent fields in `spec:`; the model has them at the top level
   (`internal/model/intent.go:12-32`). `promotion.dependsOn: [staging]` must
   be `[{environment: staging}]` (`intent.go:141`).
5. API groups are unexplained: `sourceplane.io/v1` (Intent, Component),
   `sourceplane.io/v1alpha1` (Composition, IntentPreset), `orun.io/v1`
   (Stack, Plan, TaskContract, agent types), `orun.dev/v1` (Workflow,
   Blueprint); `change-detection.md:155` uses `orun.io/v1alpha1` for a
   Component.
6. `compositions/terraform/compositions.yaml` (plural) in `stacks.md:223`
   and `intent-presets.md:35-36`.
7. `dependency-rules.md:137` omits the `edge` and `edge-rule` sources
   (`internal/composition/dependency.go:102,149`); `:212` uses an undefined
   `condition` field.
8. Several pages say `orun validate` checks components against their
   schemas. It checks the intent and its profile and dependency rules
   (`validateFiles`, `cmd/orun/main.go`); component parameters are
   validated against the composition schema during `orun plan`
   (`ValidateAllComponents`, `cmd/orun/main.go:102`).
9. Title Case titles with duplicate H1s: `environment-promotion.md:5`,
   `intent-presets.md:5`.

### Package & evolve (PD3)

- **The composition lock records; it does not pin.** `.orun/compositions.lock.yaml`
  is written on every plan (`internal/composition/registry.go:338`) and by
  `orun compositions lock`, but nothing reads it back, and `.orun/` is
  gitignored. What pins a standard is the source in `intent.yaml`: its `ref`
  tag, or strictly a `digest:` field, which resolution verifies and fails on
  mismatch (`internal/composition/registry.go:626,651,672`). The resolved
  digests are recorded in `plan.json` (`spec.compositionSources`).
  PD1 (v2.73.0) repeated the "a lockfile pins every digest" claim on the
  landing page, principles, what-is-orun, and README; PD2 corrects them, PD3
  the stacks and compositions pages.

10. `intent-presets.md:140`: "later presets take precedence … for
   non-conflicting fields" contradicts itself; `:146-147` uses
   `groups.defaults`/`environments.defaults` where the schema has
   `parameterDefaults`.
11. "Baseline" is used for both a product rebuild (`concepts/baselines.md`,
    `glossary.md:69`) and a preset (`intent-presets.md:11`). Decision 2
    settles it.
12. No page covers versioning and locking end to end; fragments at
    `stacks.md:143` and `compositions.md:161-189`.
13. `orun-pack.md:6-7` says `stack.yaml` or `orun.yaml`; the code's help says
    `orun.yaml` (`cmd/orun/command_publish.go:53`).
14. `examples/bootstrap-a-product-from-a-baseline.md:72-77` shows "Orgs:" and
    an `oruncloud.workers.dev` URL. (PD3 check: this output matches what `orun auth status` prints today, `cmd/orun/command_auth.go:123-134` and `internal/remotestate/defaults.go:27`; not a docs error.)

### Ground agents (PD4)

15. `context-for-ai/02:71-74,88` and `04:23` say `spec.inputs`/"inputs"; the
    field is `parameters`. `04:52` and `05:24` say `defaults`; the field is
    `parameterDefaults`.
16. Parameter precedence: the code applies environment `parameterDefaults`,
    then group `parameterDefaults`, then the component's own `parameters`
    (`internal/expand/expander.go:204-260`). `context-for-ai/02:121-123`
    agrees; README:239-241 puts group before environment.
17. `cli/orun-mcp.md:10,17,127` hard-codes tool counts the code computes
    (`cmd/orun/mcp.go:67`); `orun mcp doctor` (`cmd/orun/mcp_doctor.go`) is
    undocumented.
18. `concepts/agent-runtime.md:47` hard-codes a model id.
19. `agents/bootstrapper.md`, `agents/implementer.md` and `cmd/orun/pr.go:20-31`
    say the work plane is gone; `orun work check/sync` is live
    (`cmd/orun/work.go`).
20. `ai-context/orun-repositories.md` and `context-for-ai/00`, `05` repeat
    the same layer table and rules.

### Verify → plan → execute (PD5)

21. `execute/runners.md:84` describes `.orun/runs/<id>/state.json` and
    `orun run --resume <id>`; state is in `.orun/objectmodel/` and `run` has
    no `--resume`.
22. `cockpit/overview.md:249-267` describes the old `.orun/runs/` layout as
    "the only place runtime state lives".
23. `context-discovery.md:25` says `.orun/` holds "plans, executions, logs".
24. `state-model.md:11,164-166` says only the local driver ships; remote
    coordination (`internal/remotestate`, `orun run --remote-state`) exists.
25. `change-watches.md:175,185` says dependents of a changed component are
    included; `dependency-rules.md:222` and `change-detection.md:138` say
    selection propagates only over `input: true` edges.
26. `examples/remote-state-matrix.md:5,18,30,65-66,167` describes a
    self-hosted "orun-backend" as required and hard-codes an API URL; the
    binary has a managed default (`internal/remotestate/defaults.go:27`).
27. `orun run init` (`cmd/orun/command_run_init.go`) is undocumented.
28. `workflow-actions.md:147` links "blueprint" to the compositions page.

### Reference sweep (PD6)

29. 21 CLI pages and most pages under `compositions/`, `examples/`,
    `reference/` and `execute/` lack a `description`.
30. `v2.58.15` pinned in `start/installation.md`, README, and the skill;
    `docusaurus.config.js:58` links Releases to v2.58.0; `cli/orun-work.md:162`
    pins `v2.66.0`.
31. `cli/orun-catalog.md:166` shows `catalog diff <a> <b>`; the command is
    `diff [component] --base --head` (`cmd/orun/catalog_diff.go:64`).
32. `cli/orun-describe.md:9,119` treats `execution` as its own resource; it is
    an alias of `run` (`cmd/orun/command_describe.go:29`).
33. `glossary.md:80` reads "Formerly 'Orunbase'" (a broken rename).
34. `architecture/internals.md` misses `internal/inputglob` and
    `internal/workfile`.
