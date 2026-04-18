---
title: Quick start
---

This walkthrough uses the repository's example intent, discovered components, and built-in compositions to compile a plan and preview execution.

## 1. Build the CLI

```bash
make build
```

The commands below assume you are running them from the repository root and using the freshly built `./arx` binary.

## 2. Inspect the shipped compositions

```bash
./arx compositions --config-dir assets/config/compositions
```

The built-in composition set currently includes `charts`, `helm`, `helmCommon`, and `terraform`.

## 3. Validate the example intent and discovery tree

```bash
./arx validate \
  --intent examples/intent.yaml \
  --config-dir assets/config/compositions
```

This loads `examples/intent.yaml`, scans the discovery roots declared there, and validates each component against its matching composition schema.

## 4. Inspect the merged component model

```bash
./arx component web-app \
  --intent examples/intent.yaml \
  --config-dir assets/config/compositions \
  --long
```

Use this view when you want to verify labels, overrides, subscriptions, inputs, and dependency edges before you render the final plan.

## 5. Compile a deterministic plan

```bash
./arx plan \
  --intent examples/intent.yaml \
  --config-dir assets/config/compositions \
  --output /tmp/arx-plan.json \
  --view dag
```

The generated file is the execution boundary: a fully expanded DAG with explicit jobs, steps, and dependencies.

## 6. Preview execution

```bash
./arx run --plan /tmp/arx-plan.json
```

`run` defaults to dry-run mode, which prints the execution order, working directories, runner choice, and resolved steps without mutating state.

## 7. Execute the plan

```bash
./arx run \
  --plan /tmp/arx-plan.json \
  --execute \
  --runner local
```

Swap `local` for `docker` when you want containerized execution, or use `--gha` when your plan includes GitHub Actions `use:` steps.

## What happened

1. `validate` loaded the intent, discovered component manifests, and enforced schema constraints.
2. `component` showed the merged component view that feeds the compiler.
3. `plan` expanded environment and component subscriptions into concrete jobs and dependency edges.
4. `run` previewed or executed the immutable plan artifact.

## Next steps

1. Read [execution model](../concepts/execution-model.md) to understand dry-run, retries, phases, and state files.
2. Explore [GitHub Actions](../examples/run-github-actions.md) and [Docker](../examples/run-with-docker.md) runtime examples.