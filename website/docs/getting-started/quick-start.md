---
title: Quick start
---

This walkthrough uses the repository's example intent, discovered components, and packaged composition sources to compile a plan and preview execution.

## 1. Build the CLI

```bash
make build
```

The commands below assume you are running them from the repository root and using the freshly built `./gluon` binary.

## 2. Inspect the shipped compositions

```bash
./gluon compositions --intent examples/intent.yaml
```

The example package currently exports `charts`, `helm`, `helmCommon`, and `terraform`.

## 3. Lock the resolved composition sources

```bash
./gluon compositions lock --intent examples/intent.yaml
```

This writes `examples/.gluon/compositions.lock.yaml` so future plans can reuse the same resolved source digests.

## 4. Validate the example intent and discovery tree

```bash
./gluon validate \
  --intent examples/intent.yaml
```

This loads `examples/intent.yaml`, scans the discovery roots declared there, and validates each component against its matching composition schema.

## 5. Inspect the merged component model

```bash
./gluon component web-app \
  --intent examples/intent.yaml \
  --long
```

Use this view when you want to verify labels, overrides, subscriptions, inputs, and dependency edges before you render the final plan.

## 6. Compile a deterministic plan

```bash
./gluon plan \
  --intent examples/intent.yaml \
  --output /tmp/gluon-plan.json \
  --view dag
```

The generated file is the execution boundary: a fully expanded DAG with explicit jobs, steps, and dependencies.

## 7. Preview execution

```bash
./gluon run --plan /tmp/gluon-plan.json
```

`run` defaults to dry-run mode, which prints the execution order, working directories, runner choice, and resolved steps without mutating state.

## 8. Execute the plan

```bash
./gluon run \
  --plan /tmp/gluon-plan.json \
  --execute \
  --runner local
```

Swap `local` for `docker` when you want containerized execution, or use `--gha` when your plan includes GitHub Actions `use:` steps.

## What happened

1. `compositions lock` resolved the declared composition sources and wrote a reproducible lock file beside the intent.
2. `validate` loaded the intent, discovered component manifests, and enforced schema constraints.
3. `component` showed the merged component view that feeds the compiler.
4. `plan` expanded environment and component subscriptions into concrete jobs and dependency edges.
5. `run` previewed or executed the immutable plan artifact.

## Next steps

1. Read [execution model](../concepts/execution-model.md) to understand dry-run, retries, phases, and state files.
2. Explore [GitHub Actions](../examples/run-github-actions.md) and [Docker](../examples/run-with-docker.md) runtime examples.