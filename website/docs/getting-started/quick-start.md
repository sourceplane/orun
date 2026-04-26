---
title: Quick start
---

This walkthrough uses the repository's example intent, discovered components, and packaged composition sources to compile a plan and execute it.

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
./gluon validate --intent examples/intent.yaml
```

This loads `examples/intent.yaml`, scans the discovery roots declared there, and validates each component against its matching composition schema.

## 5. Inspect the merged component model

```bash
./gluon component web-app --intent examples/intent.yaml --long
```

Use this view when you want to verify labels, overrides, subscriptions, inputs, and dependency edges before you render the final plan.

## 6. Compile a deterministic plan

```bash
./gluon plan --intent examples/intent.yaml --view dag
```

The plan is saved to `.gluon/plans/` and linked as `latest`. It is the execution boundary: a fully expanded DAG with explicit jobs, steps, and dependencies.

## 7. Preview execution

```bash
./gluon run --dry-run
```

`--dry-run` prints the execution order, working directories, runner choice, and resolved steps without mutating state. `run` resolves the latest plan automatically.

## 8. Execute the plan

```bash
./gluon run --runner local
```

Swap `local` for `docker` when you want containerized execution, or use `--gha` when your plan includes GitHub Actions `use:` steps.

## 9. Inspect the result

```bash
./gluon status
./gluon get jobs
./gluon logs
```

`status` shows a compact execution summary. `get jobs` shows the grouped job tree with status icons. `logs` streams the raw step output.

## 10. Run from a component subdirectory

```bash
cd examples/services/web-app/
./gluon plan
```

`gluon` walks up the directory tree, finds `intent.yaml`, detects that you are in the `web-app` component, and generates a scoped plan containing only `web-app` and its dependencies. Use `--all` to generate a full plan instead.

## What happened

1. `compositions lock` resolved the declared composition sources and wrote a reproducible lock file beside the intent.
2. `validate` loaded the intent, discovered component manifests, and enforced schema constraints.
3. `component` showed the merged component view that feeds the compiler.
4. `plan` expanded environment and component subscriptions into concrete jobs and dependency edges, then stored the result in `.gluon/plans/`.
5. `run --dry-run` previewed the immutable plan artifact.
6. `run` executed it; progress was recorded in `.gluon/executions/`.

## Next steps

1. Read [context-aware discovery](../concepts/context-discovery.md) to learn how `gluon` auto-discovers the intent file and scopes to your current component.
2. Read [execution model](../concepts/execution-model.md) to understand dry-run, concurrency, retries, phases, and execution records.
3. Explore [GitHub Actions](../examples/run-github-actions.md) and [Docker](../examples/run-with-docker.md) runtime examples.
4. Use `gluon get`, `gluon status`, and `gluon logs` to inspect and debug ongoing or past runs.
