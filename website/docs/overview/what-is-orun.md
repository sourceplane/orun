---
title: What is orun?
description: orun is a control plane for software delivery — it compiles declarative intent into deterministic plans, runs them anywhere, and records everything it learns as a typed, queryable catalog.
---

orun is a **control plane for software delivery**. You describe the desired
shape of your delivery system — which components exist, which environments they
ship to, what policies bind them — and orun continuously turns that description
into something executable, observable, and queryable:

- a **deterministic execution plan** (`plan.json`) compiled from your intent,
- a **typed service catalog** derived from the same sources,
- an **immutable record** of every run, sealed in a content-addressed object
  store under `.orun/`.

If Kubernetes is a control plane for *running* software — you declare desired
state, controllers reconcile reality toward it — orun is the equivalent for
*delivering* software. You declare what should ship where and under which
rules; orun compiles, executes, and records it. The difference in mechanism is
deliberate: delivery is an event-driven domain, so instead of a reconciling
loop, orun gives you a **compiler**. Every event (a PR, a merge, a tag, a
manual run) produces a complete, reviewable plan *before* anything executes.

## The problem orun solves

A delivery system has three forces that grow independently:

- **Components** — the things you ship: services, charts, Terraform stacks,
  static sites. Tens to hundreds of them, owned by different teams.
- **Environments** — the places they ship to: dev, staging, production,
  per-region, per-tenant. Each with its own policies and promotion rules.
- **Triggers** — the events that cause shipping: pull requests, merges,
  tags, schedules. Each demanding different behavior from the same components.

Most organizations encode the product of these three forces in CI
configuration: workflow files, templates, shared actions, and shell scripts.
That encoding has a structural flaw — it collapses **what should happen** into
**how it happens**. The environment matrix lives in `if:` expressions. Policy
lives in code review vigilance. Dependency order lives in job names. Nobody
can answer "what will this change actually do?" without running it.

orun separates those concerns into layers with stable schemas:

| Layer | Question it answers | Lives in | Owned by |
|---|---|---|---|
| **Intent** | What exists, where does it ship, under which policies? | `intent.yaml`, `component.yaml` | Platform & app teams |
| **Contract** | How does each component type execute? | Composition packages | Platform team |
| **Plan** | Exactly what will run, in what order, with what inputs? | `plan.json` | Compiled — never edited |
| **Record** | What actually happened? | `.orun/objectmodel/` | Written by the runtime |

Because the layers are separate, each one can be reviewed, versioned, and
evolved independently. A platform team can change *how* Terraform components
deploy without touching a single app repo. An app team can add an environment
without reading runner code. A reviewer can see the full consequence of a YAML
change as a plan diff in the pull request.

## What you get

**A planner.** `orun plan` runs a six-stage compiler — load, normalize,
expand, bind, resolve, materialize — over your intent, discovered components,
and locked composition packages. The output is an immutable DAG of jobs in
which every default, policy merge, and dependency edge is explicit. Identical
inputs produce byte-identical plans.

**A policy engine that runs at compile time.** Group and environment policies
are enforced when the plan is built, not when it runs. A non-compliant intent
fails `orun validate` with a structured error — not a half-deployed
environment at 2 a.m.

**A runtime with swappable backends.** The plan is the boundary. Execute it on
your local shell, in Docker, or on GitHub Actions without recompiling. Trigger
bindings adapt the same intent to the event that fired it: a PR gets parallel
verification, a tag gets an ordered release.

**A service catalog you don't have to maintain.** Because orun already
resolves every component, its owner, its dependencies, and its golden path, it
derives a typed catalog — Components, Systems, Domains, APIs, Resources,
Environments, Groups, Compositions — as a *projection of the sources*, not a
separate database that drifts. Ownership comes from `CODEOWNERS`; deployments
and health come from actual execution history.

**A cockpit.** `orun status`, `orun logs`, and the `orun tui` terminal cockpit
render the same state through the same view-model and design tokens. What you
see in CI logs is what you see in the control room.

## What orun is not

Knowing the boundaries is as important as knowing the features:

- **orun is not a CI system.** It does not host runners, manage secrets at
  rest, or replace GitHub Actions. It runs *inside* your CI (or your shell)
  and gives it a deterministic plan to execute. Your CI provides compute and
  credentials; orun provides the decision.
- **orun is not an IaC or deployment tool.** It does not replace Terraform,
  Helm, or wrangler — it orchestrates them. Compositions wrap your existing
  tools in typed, versioned execution contracts.
- **orun is not a hosted platform.** It is a single binary. Its state lives in
  your repository's `.orun/` directory as content-addressed objects — there is
  no server to operate and no database to back up. (Optional remote-state and
  cloud backends exist for teams that want shared state.)
- **orun is not a catalog you curate by hand.** Catalog entities are derived
  from the same declarative sources that drive execution. If it ships, it's in
  the catalog; if it's in the catalog, it's because the sources say so.

## Where orun fits

```text
                 your repositories
   intent.yaml · component.yaml · CODEOWNERS · code
                        │
        events ─────────┤  PR · merge · tag · manual
                        ▼
   ┌────────────────────────────────────────────┐
   │                   orun                     │
   │  compile ──▶ plan.json  (the decision)     │
   │  execute ──▶ runners: shell · docker · gha │
   │  record  ──▶ .orun/  (catalog + history)   │
   └────────────────────────────────────────────┘
                        │
                        ▼
        terraform · helm · turbo · wrangler · …
              (your tools, unchanged)
```

orun sits between your repositories and your tools, inside whatever compute
you already trust. It is to the delivery layer what a compiler is to a
program: the place where intent becomes an artifact you can inspect before it
becomes behavior you have to debug.

## Where to go next

- [How orun works](how-orun-works.md) — the mental model: three artifacts,
  one loop, one worked example.
- [The resource model](resource-model.md) — every orun behavior is declared
  in typed `apiVersion`/`kind` documents; this page is the map.
- [Design principles](/principles) — the five principles every feature traces
  back to.
- [Quick start](../start/quick-start.md) — compile and run your first plan in
  ten minutes.
