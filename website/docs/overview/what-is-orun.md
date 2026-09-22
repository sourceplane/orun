---
title: What is orun?
description: orun is an intent compiler for platform engineering — it compiles declarative intent into deterministic plans, converges them on any runner, records everything it learns as a content-addressed object graph, and gives humans and coding agents one truthful surface over all of it.
---

orun is an **intent compiler for platform engineering**. You describe the
desired shape of your delivery platform — which components exist, which
environments they ship to, what policies bind them — and orun turns that
description into something executable, observable, and queryable:

- a **deterministic execution plan** (`plan.json`) compiled from your intent,
- a **typed service catalog** derived from the same sources,
- an **immutable object graph** under `.orun/` recording every run, plan,
  catalog, contract, agent type, and session, addressed by content hash.

If Kubernetes is a control plane for *running* software — you declare desired
state, controllers reconcile reality toward it — orun is the equivalent for
*delivering* software. The difference in mechanism is deliberate: delivery is
an event-driven domain, so instead of a reconciling loop, orun gives you a
**compiler**. Every event (a PR, a merge, a tag, a manual run) produces a
complete, reviewable plan *before* anything executes, and `orun run`
converges that plan on whichever runner you trust.

The same binary is also where work and the agents that do it are governed:
a task plane whose status is derived, never typed; an agent runtime whose
inputs and outputs are sealed content; an MCP server that gives a coding
agent hands on all of it; and a scaffold engine that rebuilds an entire
product from a registered baseline. The hosted control plane behind those
surfaces is **Orunbase** (app.orunbase.com). orun works without it; Orunbase
makes it shared.

## The problem orun solves

A delivery system has three forces that grow independently:

- **Components** — the things you ship: services, charts, Terraform stacks,
  static sites. Tens to hundreds of them, owned by different teams.
- **Environments** — the places they ship to: dev, staging, production,
  per-region, per-tenant. Each with its own policies and promotion rules.
- **Triggers** — the events that cause shipping: pull requests, merges,
  tags, schedules. Each demanding different behavior from the same components.

Most organizations encode the product of these three forces in CI
configuration. That encoding collapses **what should happen** into **how it
happens**: the environment matrix lives in `if:` expressions, policy in code
review vigilance, dependency order in job names. Nobody can answer "what will
this change actually do?" without running it — and a coding agent joining the
team inherits the same fog, plus a tracker it can update by typing.

orun separates those concerns into layers with stable schemas:

| Layer | Question it answers | Lives in | Owned by |
|---|---|---|---|
| **Intent** | What exists, where does it ship, under which policies? | `intent.yaml`, `component.yaml` | Platform & app teams |
| **Contract** | How does each component type execute? What does a task require? | Composition packages, `TaskContract` documents | Platform team, task authors |
| **Plan** | Exactly what will run, in what order, with what inputs? | `plan.json` | Compiled — never edited |
| **Record** | What actually happened? | `.orun/objectmodel/` | Written by the runtime |

Because the layers are separate, each can be reviewed, versioned, and evolved
independently: a reviewer sees the full consequence of a YAML change as a plan
diff, and a task's verdict is read off the record, not off an assertion.

## What you get

**A planner.** `orun plan` runs a six-stage compiler — load, normalize,
expand, bind, resolve, materialize — over your intent, discovered components,
and locked composition packages. Identical inputs produce byte-identical
plans; `orun intent render` and `orun intent explain` show the effective intent.

**A policy engine that runs at compile time.** Group and environment policies
are enforced when the plan is built; a non-compliant intent fails
`orun validate` with a structured error, not a half-deployed environment.

**A runtime with swappable backends.** The plan is the boundary. Execute it on
your local shell, in Docker, or on GitHub Actions without recompiling.
Workflows (`kind: Workflow`) use the same step verbs as job templates for the
glue around a plan, with `orun approve` for gated steps.

**A service catalog you don't have to maintain.** orun derives a typed
catalog — Components, Systems, Domains, APIs, Resources, Environments, Groups,
Compositions, and the Repo itself — as a projection of the sources.
Ownership comes from `CODEOWNERS`; deployments and health from execution
history.

**An object graph you can inspect.** Everything orun persists is a DAG of
immutable, content-addressed objects with named refs on top; `orun objects`
lets you `cat`, `ls-tree`, `log`, `fsck`, `push`, and `pull` it like a git
store.

**A task plane.** `orun task` creates tasks whose keys come from the platform
allocator, whose contracts live in the repository sealed by content hash,
and whose verdict — `draft → ready → in_progress → in_review → done →
released` — is derived from branches, PRs, merges, and gates. Epics and
milestones are the containers; `orun spec` pushes design docs beside them.

**A provenance pen.** `orun pr open` writes a PR's lineage into the branch
name (`orun/<KEY>-<slug>`), the `Orun-Task` commit trailer `orun githooks
install` stamps, and a manifest in the body; `check` preflights, `land` merges.

**An agent runtime.** `orun agent` delegates a task to a coding agent behind
a driver seam. Agent types are `agents/*.md` files sealed into snapshots;
briefs are frozen; sessions are append-only logs you can attach to, steer,
approve, detach from, and replay. `orun agent serve` runs the identical loop
in an Orunbase sandbox, grounded in a real checkout of a linked repository.

**One MCP server.** `orun mcp serve` composes the pen and the platform — 34
tools over one connection; `--read-only` drops every platform write.
`orun skills` pulls the hosted playbooks agents follow.

**A scaffold engine and baselines.** `orun new` places a `kind: Blueprint`
phase by phase and writes a provenance lock so `orun new upgrade` can
three-way merge a newer release. `orun baseline` builds a registered
baseline — a complete product such as `cirrus` or `lumen` — with `--local`
or `--via-platform`, and authors the build into the task plane as it goes.

**Workspaces, secrets, policy, integrations.** `orun workspace` chooses the
tenancy every cloud command runs under; `orun secrets` moves values up
without printing them back; `orun policy` lints and tests portable
`SecretPolicy` documents; `orun integrations` mints provider-brokered secrets.

**A cockpit.** `orun status`, `orun logs`, and `orun tui` render the same
record through one view-model. `orun tui-next` previews cockpit v2 — Home,
Agents, Activity, Catalog, and Events — the terminal head of Orunbase.

## What orun is not

- **orun is not a CI system.** It does not host runners or replace GitHub
  Actions. It runs *inside* your CI or your shell and gives it a
  deterministic plan. Your CI provides compute and credentials; orun provides
  the decision.
- **orun is not an IaC or deployment tool.** It does not replace Terraform,
  Helm, or wrangler — it orchestrates them through typed, versioned
  compositions.
- **orun is not a hosted platform, and Orunbase is not required.** orun is a
  single binary whose state lives in your repository's `.orun/`. Orunbase
  adds shared state, workspaces, the task plane's allocator, sandboxed agents,
  and the console; `orun backend init` self-hosts remote state on Cloudflare.
- **orun is not a tracker with an agent bolted on.** The agent is a client of
  a truth engine that already exists; it has no tool to mark anything done.
  It pushes a branch, opens a PR, and the observation moves the rung.
- **orun is not a catalog you curate by hand.** Entities are derived from the
  same declarative sources that drive execution. If it ships, it's in the
  catalog; if it's in the catalog, the sources say so.

## Where orun fits

```text
                 your repositories
   intent.yaml · component.yaml · agents/*.md · tasks/*.TaskContract.yaml
                        │
        events ─────────┤  PR · merge · tag · manual · a delegated task
                        ▼
   ┌────────────────────────────────────────────────────────┐
   │                        orun                            │
   │  compile  ──▶ plan.json         (the decision)         │
   │  converge ──▶ shell · docker · gha · workflows         │
   │  record   ──▶ .orun/            (catalog · runs · …)   │
   │  delegate ──▶ agent runtime + MCP  (sealed sessions)   │
   └──────────────────────┬─────────────────────────────────┘
                          ▼                     ▲ optional
        terraform · helm · wrangler · …     Orunbase (workspaces ·
              (your tools, unchanged)        task plane · sandboxes)
```

orun sits between your repositories and your tools, inside whatever compute
you already trust. It is to the delivery layer what a compiler is to a
program: the place where intent becomes an artifact you can inspect before it
becomes behavior you have to debug — and where an agent's work becomes
evidence before it becomes a claim.

## Where to go next

- [How orun works](how-orun-works.md) — the mental model: three artifacts,
  one loop, one worked example.
- [The resource model](resource-model.md) — every orun behavior is declared
  in typed `apiVersion`/`kind` documents; this page is the map.
- [The task plane](../concepts/task-plane.md), [the agent runtime](../concepts/agent-runtime.md),
  and [baselines](../concepts/baselines.md) — work, the agents that do it,
  and whole products rebuilt under your name.
- [Quick start](../start/quick-start.md) — compile and run your first plan in
  ten minutes.
