---
title: orun workflow
description: Validate, digest, run, or view a standalone workflow file outside a plan.
---

`orun workflow` is the standalone authoring on-ramp for
[workflows](../concepts/workflow-actions.md) — validate, digest, run, or view an
`orun.dev/v1` workflow file directly, before wiring it into a `workflow:` plan
step or blueprint hook. Since v2.35.0 the engine is **in-process**:
there is no external engine binary, no engine pin, and no
`ORUN_TORKFLOW_ENGINE`. The binary that compiles the plan runs the workflow.

## Usage

```bash
orun workflow <subcommand> <file|remote-ref>
```

| Subcommand | What it does |
|---|---|
| `validate <file>` | **Full** compile check: DAG shape, verb exclusivity, action resolution and `with:` validation, CEL expression compilation, and reference visibility. Prints `ok: <path> (sha256:…)` |
| `digest <file>` | Loads (and therefore validates) the file, then prints the content digest (`sha256:…`) — the same value the compiler and provenance lock record. |
| `run <file\|remote-ref>` | Runs the workflow in-process and streams the step timeline. |
| `view <file>` | Renders the workflow's DAG: `workflow <name> (N steps)` followed by one `<step> [<verb>] ← deps` line per step. |

### `run` flags

| Flag | Description |
|---|---|
| `--set key=value` | Set a workflow input (repeatable). Values arrive as strings; an unknown input errors with `unknown input "x"`, a missing required input with `inputs.X is required (--set X=...)`. |
| `--connection name.field=value` | Grant a declared connection field (repeatable) — the standalone equivalent of a plan step's `connections:` grant. The value is used literally. |
| `--resume <execId>` | Re-execute only the non-succeeded steps of a prior run. Refuses to resume if the file's digest changed since the run started. |

## Remote references

`run` accepts a remote reference in place of a local path:

```
github:owner/repo[@ref]//path/to/workflow.yaml   # @ref optional (default branch)
https://example.com/path/workflow.yaml           # fetched verbatim
```

```bash
orun workflow run 'github:acme/flows@v1.4.0//phases/scaffold.yaml' --set service=api
# → fetched workflow from acme/flows@v1.4.0 (abc123456789)
```

- GitHub fetches authenticate with `GITHUB_TOKEN`, then `GH_TOKEN`. A fetch
  that fails without a token appends the hint `(private repo? set GITHUB_TOKEN)`.
- `ORUN_GITHUB_API_URL` overrides the API base (GitHub Enterprise, tests).
- The fetched body is capped at 4 MiB and written to a private temp file that is
  removed when the run exits.
- The requested ref is resolved to a commit SHA (best-effort — a resolution
  failure still runs, the workflow just cannot self-pin).
- A remote run executes from **your current directory** (a local-path run
  executes from the file's directory) — this matters for nested `workflow:`
  refs and relative `cwd:`.

Every `run:` step of the workflow receives four ambient provenance variables:
`ORUN_FLOW_SOURCE_REPO` (`owner/name`), `ORUN_FLOW_SOURCE_REF` (the ref as
requested), `ORUN_FLOW_SOURCE_SHA` (the resolved commit), and
`ORUN_FLOW_SOURCE_URL`. The self-pinning idiom: a flow fetched at a ref fetches
its own scripts and payloads at exactly `$ORUN_FLOW_SOURCE_SHA`.

## Reading the output

A running workflow streams live, line-buffered output — a long-polling step no
longer reads as a hang:

```
  ▸ build                       # step announced when it starts
    build │ compiling…          # stdout/stderr streamed line-by-line
  - build: succeeded            # terminal status
      ✕ <error>                 # printed under the status line on failure
```

Parallel steps serialize per line (lines never interleave mid-line). Captures
for `outputs:` and `poll.until` are capped at 4 MiB per stream. The run ends
with `workflow <path>: <status> (exec <execId>)` and one `<name> = <value>`
line per declared workflow output; a failed run exits non-zero.

## Run state and resume

Run state is file-backed JSON under `.orun/wfruns/<execId>/` — `metadata.json`
(workflow name, digest, start time), one `steps/<name>.json` per step (status,
outputs, exit code, captured output, attempts), and `result.json`. The default
exec id is `<workflow-name>-<unixNano>`.

`--resume <execId>` re-executes only non-succeeded steps: `succeeded` and
`skipped` steps stand, `failed`/`blocked`/in-flight states are cleared and
re-run. A resumed run must execute the file it started with — a changed digest
is refused:

```
resume ci-flow-175…: workflow digest changed (sha256:ab… → sha256:cd…) —
a resumed run must execute the file it started with
```

## Examples

```bash
# Full compile check — DAG, verbs, actions, CEL, reference visibility
orun workflow validate flows/release.yaml
# → ok: flows/release.yaml (sha256:e42f521b…)

# Just the digest (useful in scripts / to compare against a plan)
orun workflow digest flows/release.yaml
# → sha256:5b4b29ad…

# Run standalone, feeding inputs and a connection grant
orun workflow run flows/release.yaml \
  --set service=api \
  --connection gh.bearerToken="$GITHUB_TOKEN"

# Resume a failed run after fixing the environment (not the file)
orun workflow run flows/release.yaml --resume release-1753776000000000000

# Render the DAG
orun workflow view flows/release.yaml
# → workflow release (3 steps)
#     build [run]
#     open-pr [run] ← build
#     wait-for-ci [run] ← open-pr
```

A file that is not an `orun.dev/v1` workflow is rejected:

```bash
orun workflow validate intent.yaml
# → ✕ not a workflow: want apiVersion orun.dev/v1 kind Workflow, got "orun.dev/v1alpha" "Intent"
# exit 1
```

Legacy `torkflow/v1` files are rejected by name — there is no converter; see
the [workflow schema](../reference/workflow-schema.md) for the manual mapping.

## Digest parity with the plan

`orun workflow digest` returns exactly the digest orun pins into `plan.json`
(for a `workflow:` step) and `.orun/provenance.lock` (for a `workflow:` hook).
At run time the on-disk file is re-hashed and compared to the pinned digest; a
mismatch is a hard error, so a workflow cannot silently change between plan and
run. Use `digest` to confirm what a given file pins to.

## See also

- [Workflows](../concepts/workflow-actions.md) — the concept and the two surfaces
- [Workflow schema](../reference/workflow-schema.md) — the complete `kind: Workflow` field reference
- [`orun run`](./orun-run.md) — where a `workflow:` step executes
