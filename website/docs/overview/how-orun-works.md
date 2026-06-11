---
title: How orun works
description: The orun mental model — declared intent is compiled into an immutable plan, the plan is executed by a runner, and everything observed is recorded in a content-addressed object store.
---

Everything orun does fits in one sentence:

> **Declared intent is compiled into an immutable plan; a runner executes the
> plan; everything observed is recorded as immutable objects.**

Three artifacts, three verbs. This page walks one example through all of them.
If you only read one page beyond [What is orun?](what-is-orun.md), read this
one.

```text
   DECLARE                COMPILE                EXECUTE              RECORD
intent.yaml ─┐
component.yaml ├──▶  orun plan  ──▶  plan.json  ──▶  orun run  ──▶  .orun/objectmodel/
compositions ─┘     (6 stages)     (immutable DAG)   (runner)      (catalog · runs · logs)
                                                                        │
                                                   orun status · logs · tui · catalog
                                                        (the cockpit reads the record)
```

## 1. Declare — say what should exist

You author two kinds of documents. Neither contains a single line of execution
logic.

The **intent** is the repository-level control document: which environments
exist, which policies and defaults bind them, where components are discovered,
and which events activate what.

```yaml
# intent.yaml
apiVersion: sourceplane.io/v1
kind: Intent
metadata:
  name: shop-platform
spec:
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
        dependsOn: [staging]
  automation:
    triggerBindings:
      github-push-main:
        on: { provider: github, event: push, branches: [main] }
        plan: { scope: changed }
      github-tag-release:
        on: { provider: github, event: push, tags: ["v*"] }
        plan: { scope: full }
```

Each **component** declares itself next to its code: a name, a *type*, the
environments it subscribes to, and typed inputs.

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
    chart: charts/web-app
    replicas: 3
  dependsOn:
    - component: checkout-api
```

The component's `type` is a contract name, not an implementation. *How* a
`helm-chart` component executes is defined once, by the platform team, in a
versioned **composition** package — an input schema, job templates, and
execution profiles. App teams never see it; they just satisfy its schema.

## 2. Compile — turn intent into a decision

`orun plan` runs the six-stage compiler:

| Stage | What it does |
|---|---|
| **Load** | Parse and schema-validate intent, discovered components, locked compositions |
| **Normalize** | Canonicalize names, expand wildcards, fill documented defaults |
| **Expand** | Materialize the environment × component matrix into instances; merge inputs; apply policies |
| **Bind** | Attach each instance to its composition's job templates; render steps |
| **Resolve** | Convert dependencies to job-level edges; detect cycles; order the DAG |
| **Materialize** | Emit `plan.json` — every reference concrete, nothing left to interpret |

The output for our example: `web-app` becomes two instances —
`web-app@staging` running the `verify` profile and `web-app@production`
running `deploy` — each with fully merged inputs, an edge to
`checkout-api`'s jobs, and a promotion edge ordering production after staging.

Three properties make the plan worth trusting:

- **It is deterministic.** The compiler is a pure function of
  `(intent, components, locked composition digests, trigger context)`.
  Identical inputs produce byte-identical plans, so a plan diff in a pull
  request is a faithful preview of behavior.
- **It is complete.** Every implicit default, policy merge, and dependency
  edge is explicit in the artifact. If a behavior isn't visible in the plan,
  that's a bug.
- **It is policy-checked.** Group and environment constraints are enforced
  here, at compile time. A violating intent never becomes a plan.

The **trigger** shapes compilation without escaping it. A push to `main`
activates staging with `scope: changed` — orun's change-detection engine
selects only affected components. A `v*` tag activates production with a full,
ordered plan. Same intent, different events, different — but always
deterministic — plans.

```bash
orun plan --env staging --output plan.json --view dag   # compile + visualize
orun plan --explain                                     # why was each job selected?
```

## 3. Execute — run the plan, anywhere

`orun run` walks the DAG and executes steps through a **runner backend**:
your local shell, Docker, or GitHub Actions. The plan is the boundary — it
carries everything the runner needs, so the same `plan.json` executes on a
laptop and in CI without recompiling.

```bash
orun run --plan plan.json                 # local shell
orun run --plan plan.json --runner docker # isolated
orun run --plan plan.json --gha           # GitHub Actions compatibility mode
```

Execution is resumable: jobs that already succeeded are skipped (with their
logs carried forward) when a run is resumed, because the record — not the
runner's memory — is the source of truth.

## 4. Record — everything observed becomes an object

orun persists what it learns the same way git persists commits: as a DAG of
**immutable, content-addressed objects** with a thin layer of named refs on
top, under `.orun/objectmodel/`.

- The **catalog** — every resolved component and derived entity (Systems,
  Environments, APIs, owners from `CODEOWNERS`, the typed relation graph) at a
  given source snapshot.
- **Plan revisions** — each compiled plan, pinned to the catalog it came from.
- **Executions** — every run, job, step, and log line, sealed when terminal.

Identical content is stored once; refs like `catalogs/current` and
`executions/latest` move cheaply. Nothing is ever rewritten.

Every read surface is a projection of this record. `orun status` and
`orun logs` read it; the `orun tui` cockpit watches it live; `orun catalog`
queries it ("who owns this?", "what depends on it?", "where is it deployed?");
`orun catalog affected` asks it what a change touches. One record, many lenses
— and all of them render through the same view-model and glyphs, so success
looks the same in a CI log and in the control room.

## The loop, end to end

For our example, a release looks like this:

1. A `v1.4.0` tag fires the `github-tag-release` binding.
2. orun compiles a full plan: staging jobs, production jobs ordered after
   them, `checkout-api` before `web-app`, every input merged and visible.
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
  [Compositions](../concepts/compositions.md) →
  [Plan DAG](../concepts/plan-dag.md) — the three core concepts, in reading
  order.
- [State model](../concepts/state-model.md) — the object store in detail.
- [Quick start](../start/quick-start.md) — do all of the above against a real
  example in ten minutes.
