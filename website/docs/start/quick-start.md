---
title: Quick start
description: Compile the example platform intent into a deterministic plan, preview it, run it, and read the result in the cockpit — then connect a workspace.
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

## 2. See what the compositions export

```bash
./orun compositions --intent examples/intent.yaml
```

The example package exports Terraform, Helm, Cloudflare, Turbo, and
workspace compositions from `examples/compositions`. A composition is how a
*type* of component is validated and built; see
[compositions](../concepts/compositions.md).

## 3. Lock the composition sources

```bash
./orun compositions lock --intent examples/intent.yaml
```

This writes `examples/.orun/compositions.lock.yaml` with the resolved source
digests, so every later plan resolves the same golden paths.

## 4. Validate the intent and the discovered components

```bash
./orun validate --intent examples/intent.yaml
```

`validate` loads `examples/intent.yaml`, scans the discovery roots it
declares, and checks each `component.yaml` against its composition's schema.
A non-compliant component fails here, with a structured error, not at deploy
time.

## 5. Inspect one merged component

```bash
./orun component network-foundation --intent examples/intent.yaml --long
```

This is the component after normalisation: labels, subscriptions, parameter
overrides, and dependency edges, before any job exists.

## 6. Compile a plan

```bash
./orun plan --intent examples/intent.yaml --view dag
```

The plan is the execution boundary: a fully expanded DAG of jobs, steps, and
dependencies, with every default and policy merge made explicit. It is written to
`.orun/plans/` and sealed into the content-addressed object model under
`.orun/objectmodel/`, where `orun status` and the TUI read it as the latest
plan. Identical inputs produce a byte-identical plan; see
[the plan DAG](../concepts/plan-dag.md).

## 7. Preview and run

Compile a narrower plan for one component in one environment, then dry-run it
with the GitHub Actions-compatible runner:

```bash
./orun plan --intent examples/intent.yaml --component network-foundation --env development \
  --output /tmp/orun-example-plan.json
./orun run --plan /tmp/orun-example-plan.json --workdir examples --gha --dry-run
```

Drop `--dry-run` to execute. `--gha` selects the runner that understands
`use:` steps; `--runner docker` runs every step in a fresh container. See
[runners](../execute/runners.md).

## 8. Read the result

```bash
./orun status
./orun get jobs
./orun logs --failed
./orun tui
```

`status`, `get`, `logs`, and the TUI render the same state through the same
cockpit view-model. Bare `./orun` on a terminal opens the TUI.

## 9. Converge only what a commit changed

```bash
./orun catalog refresh --intent examples/intent.yaml
./orun catalog affected --base main --json
./orun run --changed --base main --dry-run
```

The catalog is the content-addressed record of every component. `affected`
reports the directly changed, dependent, and selected sets that `--changed`
uses to compile the minimal plan. See
[change detection](../concepts/change-detection.md).

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
