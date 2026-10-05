---
title: What is orun?
description: orun is platform discipline as code. A declarative language for your platform's structure and standards, a way to package and evolve them like code, a grounding for the coding agents that work in your repositories, and a runner that verifies, plans, and executes.
---

orun is **platform discipline as code**. It gives a platform team a declarative language
for the two things that are usually hardest to keep straight: the platform's *structure*
(which components exist, where they ship, how they depend on each other) and its
*standards* (how each kind of thing is built, what may run in which lane, in what order
environments promote). You write both down as intent, in the repository, where they can be
reviewed, versioned, and read by anyone, including a coding agent.

From there, four things follow, and they are the four pillars these docs are organised
around:

1. **Declare.** Structure and standards live in `intent.yaml`, `component.yaml`, and
   composition packages: typed, schema-checked documents instead of wiki pages and CI
   conditionals.
2. **Package and evolve.** Standards travel like code. Golden paths ship as versioned
   **Stacks**, platform rules ship as **intent presets**, a lockfile pins every digest, and
   a **baseline** packages a whole product so it can be rebuilt and upgraded rather than
   copied and forked.
3. **Ground agents.** A coding agent reads the same intent, catalog, and contracts your
   platform does, through one MCP server and a versioned base literacy. What it may touch is
   declared, so it works inside your standards instead of inferring them from the nearest
   example.
4. **Verify, plan, execute.** A runner checks the intent, compiles it into a deterministic
   plan you can review as a diff, and executes that plan on your shell, in Docker, or on
   GitHub Actions, recording everything it does.

orun is a single Go binary, and its state lives in your repository. The hosted control
plane, **Orunbase** (app.orunbase.com), adds shared state, workspaces, the task plane, and
sandboxed agents when a team needs them. orun works without it.

## The problem orun solves

A delivery system has three forces that grow independently:

- **Components** are the things you ship: services, charts, Terraform stacks, static
  sites. Tens to hundreds of them, owned by different teams.
- **Environments** are the places they ship to: dev, staging, production, per-region,
  per-tenant. Each has its own rules and promotion order.
- **Triggers** are the events that cause shipping: pull requests, merges, tags, schedules.
  Each demands different behaviour from the same components.

Most organisations encode the product of these three forces in CI configuration, and the
standards that govern it in documents and review habits. That encoding collapses **what
should happen** into **how it happens**: the environment matrix lives in `if:` expressions,
the golden path lives in whichever repository was copied last, dependency order lives in
job names. Nobody can answer "what will this change do?" without running it. A coding agent
joining the team inherits the same fog and fills the gaps by imitation, at machine speed.

orun separates those concerns into layers with stable schemas:

| Layer | Question it answers | Lives in | Owned by |
|---|---|---|---|
| **Intent** | What exists, where does it ship, under which rules? | `intent.yaml`, `component.yaml` | Platform and app teams |
| **Contract** | How is each kind of component built? What may a task touch? | Composition packages, `TaskContract` documents | Platform team, task authors |
| **Plan** | Exactly what will run, in what order, with what inputs? | `plan.json` | Compiled, never edited |
| **Record** | What actually happened? | `.orun/objectmodel/` | Written by the runtime |

Because the layers are separate, each can be reviewed, versioned, and evolved on its own.
A platform team can ship a new version of a golden path without touching any application
repository; a reviewer sees the full consequence of a YAML change as a plan diff; a task's
verdict is read off the record, not off an assertion.

## What you get, pillar by pillar

### 1 · Declare

- **A platform intent.** `intent.yaml` declares environments, groups, discovery roots,
  trigger bindings, promotion order, and the composition sources the repository uses. See
  the [intent model](../concepts/intent-model.md).
- **Component intent next to the code.** Each `component.yaml` says what a unit is, which
  golden path it follows, which environments it subscribes to with which profile, its
  typed parameters, and what it depends on.
- **Golden paths as contracts.** A [composition](../concepts/compositions.md) is the
  schema a component must satisfy, the job templates it runs, and the execution profiles
  allowed per lane. `orun plan` rejects a component whose parameters do not match its
  composition's schema.
- **Rules that adapt to the event.** [Trigger bindings](../concepts/trigger-bindings.md),
  [profile rules](../concepts/profile-rules.md),
  [dependency rules](../concepts/dependency-rules.md), and
  [environment promotion](../concepts/environment-promotion.md) say which events activate
  which environments, which profile runs in which lane, and what waits for what.
- **Secrets as references.** A secret slot holds a `secret://` reference, never a value.
  See [secrets](../concepts/secrets.md).

### 2 · Package and evolve

- **Stacks.** Compositions are packaged and published as versioned OCI artifacts with
  `orun pack` and `orun publish`, and pulled by any repository like a dependency. See
  [Stacks](../concepts/stacks.md).
- **Intent presets.** A Stack can also publish platform rules (environments, triggers,
  defaults, discovery roots) that repositories inherit with `extends:`.
  `orun intent explain` shows where every effective field came from. See
  [intent presets](../concepts/intent-presets.md).
- **Locks.** `orun compositions lock` pins every resolved source by digest in
  `compositions.lock.yaml`, so a standard changes only when you change the pin.
- **Blueprints and baselines.** `orun new` places a `kind: Blueprint` phase by phase and
  records a provenance lock so `orun new upgrade` can three-way merge a newer release.
  `orun baseline` rebuilds a registered baseline, a whole product's structure and
  standards, for a new owner. See [baselines](../concepts/baselines.md).

### 3 · Ground agents

- **One MCP server.** `orun mcp serve` gives a coding agent the platform's catalog, runs,
  logs, skills, and task plane over one connection, plus the provenance pen for opening
  pull requests. `--read-only` drops every write.
- **Base literacy and agent types.** `orun agent context` prints the versioned literacy
  every agent type extends. Agent types are `agents/*.md` files with a deny-by-default tool
  policy, sealed into snapshots; briefs are frozen; sessions are append-only logs you can
  attach to, steer, and replay. See the [agent runtime](../concepts/agent-runtime.md).
- **Contracts and derived status.** `orun task` binds work to a `TaskContract` whose
  `affects` list is the blast radius a change may touch. Status is derived from branches,
  pull requests, merges, and gates; no agent has a tool to mark anything done. See the
  [task plane](../concepts/task-plane.md).
- **Skills.** `orun skills` pulls the hosted playbooks agents follow.

[Agents in orun repositories](../ai-context/orun-repositories.md) is the starting point
for this pillar.

### 4 · Verify, plan, execute

- **Verify.** `orun validate` checks the intent and its profile and dependency rules
  without compiling a plan.
- **Plan.** `orun plan` runs a six-stage compiler (load, normalize, expand, bind,
  resolve, materialize) over your intent, discovered components, and locked compositions.
  Identical inputs produce byte-identical plans. `orun intent render` and
  `orun intent explain` show the effective intent the plan was compiled from.
- **Execute.** `orun run` executes the plan on your shell, in Docker, or on GitHub Actions
  without recompiling. `--changed` compiles only what a commit touched. Workflows
  (`kind: Workflow`) carry the glue around a plan, with `orun approve` for gated steps.
- **Record.** Everything orun persists is a graph of immutable, content-addressed objects
  under `.orun/` that `orun objects` can inspect like a git store. A typed
  [service catalog](../concepts/service-catalog.md) is derived from the same sources,
  with ownership from `CODEOWNERS` and deployments from execution history. `orun status`,
  `orun logs`, and `orun tui` render the record in the [cockpit](../cockpit/overview.md).

### Across all four

`orun workspace` chooses the tenancy every cloud command runs under; `orun secrets` moves
values up without printing them back; `orun policy` lints and tests portable
`SecretPolicy` documents; `orun integrations` mints provider-brokered secrets.

## What orun is not

- **orun is not a CI system.** It does not host runners or replace GitHub Actions. It runs
  *inside* your CI or your shell and gives it a deterministic plan. Your CI provides
  compute and credentials; orun provides the decision.
- **orun is not an IaC or deployment tool.** It does not replace Terraform, Helm, or
  wrangler. It orchestrates them through typed, versioned compositions.
- **orun is not a wiki for standards.** A standard in orun is a document the planner
  reads. If a rule is not in the intent or a composition, orun does not know it exists.
- **orun is not a hosted platform, and Orunbase is not required.** orun is a single binary
  whose state lives in your repository's `.orun/`. Orunbase adds shared state, workspaces,
  the task plane's allocator, sandboxed agents, and the console; `orun backend init`
  self-hosts remote state on Cloudflare.
- **orun is not a tracker with an agent bolted on.** The agent is a client of the same
  intent and record everyone else uses; it has no tool to mark anything done. It pushes a
  branch, opens a pull request, and the observation moves the task.
- **orun is not a catalog you curate by hand.** Entities are derived from the same
  declarative sources that drive execution.

## Where orun fits

```text
                         your repositories
     intent.yaml · component.yaml · agents/*.md · tasks/*.TaskContract.yaml
                                  ▲
        Stacks · presets ─────────┤ extends: · compositions.lock.yaml
        baselines (OCI, registry) │
                                  │
        events ───────────────────┤ PR · merge · tag · manual · a delegated task
                                  ▼
   ┌───────────────────────────────────────────────────────────────┐
   │                             orun                              │
   │  verify   ──▶ orun validate        (intent and rules)         │
   │  plan     ──▶ plan.json            (the decision)             │
   │  execute  ──▶ shell · docker · gha · workflows                │
   │  record   ──▶ .orun/               (catalog · runs · …)       │
   │  ground   ──▶ orun mcp · agents    (same intent, sealed)      │
   └──────────────────────────┬────────────────────────────────────┘
                              ▼                       ▲ optional
         terraform · helm · wrangler · …        Orunbase (workspaces ·
               (your tools, unchanged)          task plane · sandboxes)
```

orun sits between your repositories and your tools, inside whatever compute you already
trust. It is where a standard stops being a recommendation and becomes something the
platform reads, packages, shares with its agents, and checks before anything runs.

## Where to go next

- [Design principles](../principles.md): the choices that make discipline enforceable.
- [How orun works](how-orun-works.md): one worked example from intent to record.
- [The resource model](resource-model.md): every orun behaviour is declared in typed
  `apiVersion`/`kind` documents; this page is the map.
- [Quick start](../start/quick-start.md): declare, verify, plan, and run your first
  platform in ten minutes.
