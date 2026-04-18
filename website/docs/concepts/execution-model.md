---
title: Execution model
---

`arx` keeps planning and execution separate on purpose. `plan` produces an immutable DAG, and `run` consumes that DAG through an explicit execution backend.

## Dry-run is the default

`arx run` does not execute steps unless you opt into `--execute`.

```bash
arx run --plan plan.json
```

That default matters in review-heavy environments because it lets you inspect:

- job ordering
- resolved working directories
- chosen runner backend
- retries, timeouts, and step phases

## Supported runners

| Runner | What it does | When to use it |
| --- | --- | --- |
| `local` | Executes each `run:` step through `sh -c` on the host | Local development and machines that already have the required binaries |
| `docker` | Pulls the job image, mounts the workspace at `/workspace`, and executes inside a container | CI or review flows where you want stronger environment isolation |
| `github-actions` | Executes GitHub Actions-style `use:` steps and compatible workflow commands | Plans that embed Actions behavior or need compatibility with the GitHub Actions execution model |

If a step contains `use:`, the local executor fails fast and asks you to rerun with `--gha` or `--runner github-actions`.

## Runner resolution order

`run` chooses its backend in a stable order:

1. `--gha`
2. `--runner`
3. `ARX_RUNNER`, `CIZ_RUNNER`, or the deprecated `LITECI_RUNNER`
4. `GITHUB_ACTIONS=true`
5. Auto-detection when the compiled plan contains a `use:` step
6. Fallback to `local`

## Step phases and ordering

Steps can declare `phase` and `order` attributes.

- `phase`: `pre`, `main`, or `post`
- `order`: ascending integer inside a phase

Execution stays deterministic:

1. all `pre` steps
2. all `main` steps
3. all `post` steps

Within a phase, `arx` sorts by `order` and then preserves declaration order.

## State files and resumability

Executed plans track progress in a state file. The default is `.arx-state.json`, and the legacy `.ciz-state.json` and `.liteci-state.json` names are still recognized.

That state lets `run`:

- skip already completed jobs
- retry a single failed job with `--job-id` and `--retry`
- verify the compiled plan checksum before resuming

## Working directory rules

By default, each job runs in its own resolved job path. Use `--workdir` to override that behavior globally:

```bash
arx run --plan plan.json --execute --workdir ./examples
```

When the GitHub Actions backend is selected and `--workdir` is not explicitly set, `arx` uses `GITHUB_WORKSPACE` when that variable is available.

## Runtime environment variables

During execution, `arx` injects runner context into the step environment:

- `ARX_CONTEXT`
- `ARX_RUNNER`
- `CIZ_CONTEXT`
- `CIZ_RUNNER`
- `LITECI_CONTEXT`
- `LITECI_RUNNER`

That gives steps a consistent way to understand whether they are running locally, in a container, or through the GitHub Actions-compatible backend.