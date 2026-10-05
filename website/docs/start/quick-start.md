---
title: Quick start
description: Walk the example platform through the four pillars - read its declared intent, see its golden paths, give an agent the same view, then verify, plan, run, and read the record.
---

This walkthrough uses the repository's `examples/` directory: a complete
platform intent with discovered components and packaged compositions. It
takes about ten minutes and never touches a cloud account.

## 1. Get the CLI

Either [install a release](./installation.md) and use `orun`, or build from
the repository and use `./orun`. The commands below assume the repository
checkout, because the example intent lives there:

```bash
git clone https://github.com/sourceplane/orun.git
cd orun
make build
```

The steps follow the four pillars: **declare** what the platform is, see how its
standards are **packaged**, give an **agent** the same view, then **verify, plan, and
execute**.

## 2. Declare: read the platform's intent

Open `examples/intent.yaml`. It declares the platform's structure and rules: discovery
roots, environments and what activates them, trigger bindings, and the composition source
the golden paths come from. Each component declares itself in a `component.yaml` next to
its code. See one, merged with every default that applies to it:

```bash
./orun component network-foundation --intent examples/intent.yaml --long
```

This is the component after normalisation: labels, subscriptions, merged parameters, and
dependency edges, before any job exists. See the [intent model](../concepts/intent-model.md).

## 3. Package: see the golden paths

```bash
./orun compositions --intent examples/intent.yaml
```

The example Stack in `examples/compositions` exports Terraform, Helm, Cloudflare, Turbo,
and workspace compositions. A composition is how a *type* of component is validated and
built: its schema, its jobs, and the profiles allowed in each lane. In a real platform it
is published as a versioned [Stack](../concepts/stacks.md) and pinned by reference.

## 4. Ground an agent: give it the same view

```bash
./orun agent context | head -20     # the versioned base literacy every agent reads
./orun mcp tools                    # the tools an agent gets from `orun mcp serve`
```

A coding agent working in this repository reads the same intent, catalog, and contracts
through `orun mcp serve`. See [agents in orun repositories](../ai-context/orun-repositories.md).

## 5. Verify the intent

```bash
./orun validate --intent examples/intent.yaml
```

`validate` loads `examples/intent.yaml`, scans the discovery roots it declares, and checks
the component manifests, the reserved `ORUN_` environment prefix, and every profile and
dependency rule. It does not load compositions, so it is fast.

## 6. Plan

```bash
./orun plan --intent examples/intent.yaml --view dag
```

Compiling the plan is the heavier check: every component's parameters are validated
against its composition's schema, secret slots must hold references, and dependency
cycles are refused. The result is the execution boundary: a fully expanded DAG of jobs,
steps, and dependencies, with every default and merged parameter made explicit. It is
sealed into the content-addressed object model under `.orun/objectmodel/`, where
`orun status` and the TUI read it as the latest plan, and it records the digest each
composition source resolved to. Identical inputs produce a byte-identical plan; see
[the plan DAG](../concepts/plan-dag.md) and [standards](../concepts/standards.md).

## 7. Execute: preview and run

Compile a narrower plan for one component in one environment, then dry-run it with the
GitHub Actions-compatible runner:

```bash
./orun plan --intent examples/intent.yaml --component network-foundation --env development \
  --output /tmp/orun-example-plan.json
./orun run --plan /tmp/orun-example-plan.json --workdir examples --gha --dry-run
```

Drop `--dry-run` to execute. `--gha` selects the runner that understands `use:` steps;
`--runner docker` runs every step in a fresh container. See [runners](../execute/runners.md).

## 8. Read the record

```bash
./orun status
./orun get jobs
./orun logs --failed
./orun tui
```

`status`, `get`, `logs`, and the TUI render the same record through the same cockpit
view-model. Bare `./orun` on a terminal opens the TUI.

## 9. Re-verify only what a commit changed

```bash
./orun catalog refresh --intent examples/intent.yaml
./orun catalog affected --base main --json
./orun run --changed --base main --dry-run
```

The catalog is the content-addressed record of every component. `affected` reports the
directly changed, dependent, and selected sets that `--changed` uses to compile the
minimal plan. See [change detection](../concepts/change-detection.md).

## 10. Connect a workspace

Everything so far is local; state lives in `.orun/`. To share state, run
builds on the platform, or bootstrap a product from a baseline, sign in:

```bash
orun auth login
orun workspace create "Acme Cloud" --slug acme
orun workspace use acme
orun baseline list
```

Continue with
[Create a workspace and build it from a baseline](../examples/bootstrap-a-product-from-a-baseline.md).

## Where to go next

- [What is orun?](../overview/what-is-orun.md) and [how orun works](../overview/how-orun-works.md)
- [The intent model](../concepts/intent-model.md), then [writing compositions](../compositions/writing-compositions.md)
- [The CLI reference](../cli/orun.md)
