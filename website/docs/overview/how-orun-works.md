---
title: How orun works
description: The runner's walkthrough - declared intent is verified, compiled into a deterministic plan, executed on the runner you choose, and recorded as content-addressed history, with one example followed from intent.yaml to a release.
---

Everything orun does fits in one sentence:

> **Declared intent is verified and compiled into an immutable plan; a runner
> executes the plan; everything observed is recorded as immutable objects.**

This is pillar 4 of platform discipline as code, the runner, and it is what makes the
other three pillars binding. The standards you [declare](../concepts/intent-model.md),
[package](../concepts/stacks.md), and [share with agents](../ai-context/orun-repositories.md)
only matter if something checks them before anything runs. This page follows one example
through every step. If you only read one page beyond [What is orun?](what-is-orun.md),
read this one.

```text
   DECLARE           VERIFY            PLAN                    EXECUTE           RECORD
intent.yaml ─┐
component.yaml ├──▶ orun validate ──▶ orun plan ──▶ plan.json ──▶ orun run ──▶ .orun/objectmodel/
compositions ─┘    (intent, rules)   (schemas,     (immutable    (runner)     (catalog · runs · logs ·
                                      6 stages)     DAG)                       contracts · sessions)
                                                                                    │
                                              orun status · logs · tui · catalog · objects
                                                      (every surface reads the record)
                                                                                    │
                                                       Orunbase (optional): shared state,
                                                       the task plane, sandboxed agents
```

## 1. Declare — say what should exist

You author two kinds of documents. Neither contains a single line of execution
logic.

The **intent** is the repository-level control document: which environments
exist, which defaults apply to them, in what order they promote, where
components are discovered, and which events activate what.

```yaml
# intent.yaml
apiVersion: sourceplane.io/v1
kind: Intent
metadata:
  name: shop-platform
discovery:
  roots: [apps/, infra/]
environments:
  staging:
    activation:
      triggerRefs: [github-push-main]
  production:
    activation:
      triggerRefs: [github-tag-release]
    promotion:
      dependsOn:
        - environment: staging
automation:
  triggerBindings:
    github-push-main:
      on: { provider: github, event: push, branches: [main] }
      plan: { scope: changed, base: before, head: after }
    github-tag-release:
      on: { provider: github, event: push, tags: ["v*"] }
      plan: { scope: full }
```

Each **component** declares itself next to its code: a name, a *type*, the
environments it subscribes to, and typed parameters.

```yaml
# apps/web/component.yaml
apiVersion: sourceplane.io/v1
kind: Component
metadata:
  name: web-app
spec:
  type: helm-chart
  system: storefront
  lifecycle: production
  subscribe:
    environments:
      - { name: staging,    profile: verify }
      - { name: production, profile: deploy }
  parameters:
    chartPath: charts/web-app
  dependsOn:
    - component: checkout-api
```

The component's `type` is a contract name, not an implementation. *How* a
`helm-chart` component executes is defined once, by the platform team, in a
versioned **composition** package — an input schema, job templates, and
execution profiles. App teams never see it; they just satisfy its schema.

## 2. Verify — check the declarations

`orun validate` checks the intent file and the component manifests it discovers, the
reserved `ORUN_` environment prefix, and every profile and dependency rule, without
loading compositions or compiling anything. It is fast enough for a pre-commit hook and
the first step of every CI run.

```bash
orun validate
```

The heavier checks need the golden paths, so they happen as the first stage of the plan.

## 3. Plan — turn intent into a decision

`orun plan` runs the six-stage compiler:

| Stage | What it does |
|---|---|
| **Load** | Parse the intent and discovered components, resolve composition sources (verifying any `digest:` pin), and apply presets |
| **Normalize** | Canonicalize names, expand wildcards, fill documented defaults, then check every component's parameters against its composition's schema |
| **Expand** | Materialize the environment × component matrix into instances; merge parameters and `env`; check secret references |
| **Bind** | Attach each instance to its composition's job templates; render steps |
| **Resolve** | Convert dependencies to job-level edges; detect cycles; order the DAG |
| **Materialize** | Emit `plan.json` — every reference concrete, nothing left to interpret |

The output for our example: `web-app` becomes two instances, one in staging
running the `verify` profile and one in production running `deploy`, each with
fully merged parameters, an edge to
`checkout-api`'s jobs, and a promotion edge ordering production after staging.

Three properties make the plan worth trusting:

- **It is deterministic.** The compiler is a pure function of
  `(intent, components, resolved composition digests, trigger context)`.
  Identical inputs produce byte-identical plans, so a plan diff in a pull
  request is a faithful preview of behavior.
- **It is complete.** Every implicit default, merged parameter, and dependency
  edge is explicit in the artifact. If a behavior isn't visible in the plan,
  that's a bug.
- **It is checked.** Every component's parameters are validated against its
  composition's schema, secret slots must hold references, dependency cycles
  are refused, and a pinned source must match its digest. A violating intent
  never becomes a plan. See [standards](../concepts/standards.md) for what is
  enforced and what is only recorded.

The **trigger** shapes compilation without escaping it. A push to `main`
activates staging with `scope: changed` — orun's change-detection engine
selects only affected components. A `v*` tag activates production with a full,
ordered plan. Same intent, different events, different — but always
deterministic — plans.

```bash
orun plan --env staging --output plan.json --view dag   # compile + visualize
orun plan --explain                                     # why was each job selected?
```

## 4. Execute — run the plan, anywhere

`orun run` walks the DAG and executes steps through a **runner backend**:
your local shell, Docker, or GitHub Actions. The plan is the boundary — it
carries everything the runner needs, so the same `plan.json` executes on a
laptop and in CI without recompiling.

```bash
orun run --plan plan.json                 # local shell
orun run --plan plan.json --runner docker # isolated
orun run --plan plan.json --gha           # GitHub Actions compatibility mode
```

Execution is resumable: run again with the same `--exec-id` and jobs that
already succeeded are skipped (with their logs carried forward), because the
record, not the runner's memory, is the source of truth.

## 5. Record — everything observed becomes an object

orun persists what it learns the same way git persists commits: as a DAG of
**immutable, content-addressed objects** with a thin layer of named refs on
top, under `.orun/objectmodel/`.

- The **catalog** — every resolved component and derived entity (Systems,
  Environments, APIs, owners from `CODEOWNERS`, the typed relation graph) at a
  given source snapshot.
- **Plan revisions** — each compiled plan, pinned to the catalog it came from.
- **Executions** — every run, job, step, and log line, sealed when terminal.

- **Contracts, agent types, and sessions** — a task's sealed contract
  (`refs/tasks/<KEY>`), each `agents/*.md` sealed as an agent type, and every
  agent session's event log, sealed when it ends.

Identical content is stored once; refs like `catalogs/current` and
`executions/latest` move cheaply. Nothing is ever rewritten. The whole store
is the **object graph**, and `orun objects` inspects it the way you would a
git repository: `log` lists executions, `cat` prints an object, `ls-tree`
walks a tree, `rev-parse` resolves a ref, `fsck` verifies integrity, and
`push`/`pull` move a ref's closure between stores.

Every read surface is a projection of this record. `orun status` and
`orun logs` read it; the `orun tui` cockpit watches it live; `orun catalog`
queries it ("who owns this?", "what depends on it?", "where is it deployed?");
`orun catalog affected` asks it what a change touches. One record, many lenses
— and all of them render through the same view-model and glyphs, so success
looks the same in a CI log and in the control room.

## Around the loop — Orunbase and the task plane

Everything above runs from a single binary against a local `.orun/`. Connect
a workspace on **Orunbase**, the hosted control plane, and the same record
becomes shared: `orun catalog push` advances a workspace-wide catalog head,
`orun run --remote-state` records runs there, and secrets, integrations, and
policy resolve from the workspace.

Two planes sit on top of that shared record. The **task plane** binds work to
a contract: `orun task create` takes a key from the platform's allocator,
`tasks/<KEY>.TaskContract.yaml` says what the work may touch and when it is
done, and the task's verdict — `draft → ready → in_progress → in_review →
done → released` — is derived from the branch, PR, merge, and gates the
platform observes, never typed. The **agent runtime** lets a coding agent do
that work from a frozen brief, with `orun mcp serve` as its hands and a
sealed session as its proof. Neither plane changes the loop; both read and
write the same objects it does.

## The loop, end to end

For our example, a release looks like this:

1. A `v1.4.0` tag fires the `github-tag-release` binding.
2. orun verifies and compiles a full plan: staging jobs, production jobs ordered
   after them, `checkout-api` before `web-app`, every parameter merged and visible.
3. The runner executes the DAG; `orun status --watch` shows it live.
4. The sealed execution joins the object model. The catalog's live plane now
   answers "what is deployed in production?" with `web-app @ v1.4.0` —
   derived from the run that actually happened, not from a wiki.

Change the intent, and the next plan reflects it. Nothing else to update: the
catalog, the cockpit, and the history are all projections of the same
declared sources and recorded facts.

## Where to go next

- [The resource model](resource-model.md) — the typed documents behind each
  layer, and how they version.
- [Intent model](../concepts/intent-model.md) →
  [Standards](../concepts/standards.md) →
  [Compositions](../concepts/compositions.md) →
  [Plan DAG](../concepts/plan-dag.md) — the core concepts, in reading order.
- [State model](../concepts/state-model.md) — the object store in detail.
- [The task plane](../concepts/task-plane.md) and
  [the agent runtime](../concepts/agent-runtime.md) — the planes around the
  loop.
- [Quick start](../start/quick-start.md) — do all of the above against a real
  example in ten minutes.
