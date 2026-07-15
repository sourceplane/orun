---
title: orun workflow
---

`orun workflow` is the standalone authoring on-ramp for
[workflow actions](../concepts/workflow-actions.md) — validate, digest, run, or
view a [torkflow](https://github.com/sourceplane/torkflow) workflow file directly,
before wiring it into a `workflow:` plan step or blueprint hook.

## Usage

```bash
orun workflow <subcommand> <file>
```

| Subcommand | What it does |
|---|---|
| `validate <file>` | Structural check: the file is readable and declares a `torkflow/...` apiVersion. A full schema check is the engine's job — orun stays engine-agnostic. |
| `digest <file>` | Prints the content digest (`sha256:…`) orun would pin for the file — the same value the compiler and provenance lock record. |
| `run <file>` | Runs the workflow through the pinned engine and prints its status and step timeline. |
| `view <file>` | Renders the workflow's DAG by fronting the engine's own `view`. |

### `run` flags

| Flag | Description |
|---|---|
| `--set key=value` | Set a Trigger input (repeatable). |

## The pinned engine

`run` and `view` need the workflow engine, resolved from the
`ORUN_TORKFLOW_ENGINE` environment variable (the path to the torkflow engine
binary). `validate` and `digest` are engine-free and always work. If no engine is
configured, `run`/`view` fail with a clear message rather than a crash.

## Examples

```bash
# Structural check — prints the file and the digest orun would pin
orun workflow validate examples/workflows/notify-oncall.yaml
# → ok: examples/workflows/notify-oncall.yaml (sha256:e42f521b…)

# Just the digest (useful in scripts / to compare against a plan)
orun workflow digest examples/workflows/open-pr.yaml
# → sha256:5b4b29ad…

# Run standalone, feeding the Trigger context
export ORUN_TORKFLOW_ENGINE=/usr/local/bin/torkflow
orun workflow run examples/workflows/notify-oncall.yaml \
  --set channel=ops --set component=web-api

# Render the DAG
orun workflow view examples/workflows/open-pr.yaml
```

A file that is not a torkflow workflow is rejected:

```bash
orun workflow validate intent.yaml
# → ✕ intent.yaml does not look like a torkflow workflow (no 'apiVersion: torkflow/...')
# exit 1
```

## Digest parity with the plan

`orun workflow digest` returns exactly the digest orun pins into `plan.json` (for
a `workflow:` step) and `.orun/provenance.lock` (for a `workflow:` hook). At run
time the on-disk file is re-hashed and compared to the pinned digest; a mismatch
is a hard error, so a workflow cannot silently change between plan and run. Use
`digest` to confirm what a given file pins to.

## See also

- [Workflow actions](../concepts/workflow-actions.md) — the concept and the two surfaces
- [`orun run`](./orun-run.md) — where a `workflow:` step executes
- [`orun new`](../concepts/compositions.md) — where a `workflow:` hook runs
