---
title: Runners
description: The plan is the boundary. Execute it on the local shell, in Docker, or on GitHub Actions without recompiling.
---

The runner is the last checkpoint between your standards and your infrastructure, and it
adds no decisions of its own: it executes the plan that was verified and compiled, on the
backend you choose.

orun separates compilation from execution. `orun plan` produces an immutable
`plan.json` — a DAG of jobs and steps with every reference resolved. `orun run`
consumes that plan through an **execution backend** (a "runner") of your choice.

> Same plan, different runners. The plan is the boundary.

<div className="pillarGrid">
  <article className="pillarCard">
    <span className="glyph">→</span>
    <strong>local</strong>
    <h3>Run on the host shell</h3>
    <p>Each <code>run:</code> step executes through <code>sh -c</code> on your machine.
    Fast feedback loop for development and any environment that already has the required
    binaries on <code>$PATH</code>.</p>
  </article>
  <article className="pillarCard">
    <span className="glyph">▣</span>
    <strong>docker</strong>
    <h3>Run in a container</h3>
    <p>Pulls the job's image, mounts the workspace at <code>/workspace</code>, and
    executes inside an isolated container. Use for CI parity and stronger isolation when
    composition steps shouldn't touch the host.</p>
  </article>
  <article className="pillarCard">
    <span className="glyph">⚙</span>
    <strong>github-actions</strong>
    <h3>Run Actions-compatible steps</h3>
    <p>Executes GitHub Actions-style <code>use:</code> steps and compatible workflow
    commands. Auto-selected when the plan contains any <code>use:</code> step; can be
    forced with <code>--gha</code>.</p>
  </article>
</div>

## Selection order

`orun run` resolves the runner in a stable, predictable order:

```text
1. --gha                       ← shorthand for --runner github-actions
2. --runner <name>             ← explicit flag
3. $ORUN_RUNNER                ← environment variable
4. auto: github-actions        ← when GITHUB_ACTIONS=true
                                 or any step in plan uses use:
5. local                       ← default
```

The chosen runner is recorded in the run metadata, so the cockpit always tells you
which backend executed which step.

## Choosing a runner

| Need | Runner |
|---|---|
| Fast local iteration on a workstation | `local` |
| Reproducible execution that matches CI | `docker` |
| Plans that embed GitHub Actions | `github-actions` |
| Mixed plans (shell + actions) | `github-actions` (handles both) |
| CI environment, vendor-agnostic | `docker` |
| CI environment, on GitHub Actions | auto-detected |

The runner does not affect the plan. Switching runners is a deploy-time choice, not a
compile-time one — you can validate against `docker` and ship with `github-actions`
from the same `plan.json`.

## Preview before execute

`--dry-run` is the universal preview. It runs every stage *up to* shell execution:
resolves working directories, prepares the runner, picks images — and stops.

```bash
orun run --plan plan.json --dry-run
```

Use it when you want to inspect job ordering, step phases, runtime hints, and the
selected runner without actually executing anything.

## Resumption

Runs are durable. Every run is recorded as an execution in the object model under
`.orun/objectmodel/` (see [state model](/concepts/state-model)), so running again with
the same `--exec-id` skips the jobs that already succeeded and carries their logs
forward. `orun run --job <id> --retry` re-runs a single job. In CI, pin the ID with
`--exec-id` or `ORUN_EXEC_ID` so a rerun resumes the same execution; see
[resume-aware CI reruns](/cli/orun-run#resume-aware-ci-reruns).

## GitHub Actions steps

### Action isolation

When multiple jobs run concurrently and both use the same remote action (e.g., `actions/setup-node@v4`), each job works from its own isolated copy of that action's directory. The shared on-disk cache is never modified during execution, so jobs cannot race on the same files.

Isolation is zero-cost by default: files are hardlinked from the cache into each job's temp directory. A full copy only happens when the job temp directory is on a different filesystem than the cache.

Local actions (paths starting with `./`) are not isolated — they are used directly from the workspace.

### Action reference caching

Resolving a non-SHA action reference (e.g., `v4`) to a commit SHA requires a GitHub API call. orun avoids redundant API calls through a three-tier cache:

| Tier | Scope | What it stores |
| --- | --- | --- |
| In-memory | Current process | `repo@ref` → SHA map, deduplicated via singleflight |
| On-disk | Persistent across runs | SHA written to `~/.orun/actions/<repo>/refs/<ref>` |
| API | Fallback | GitHub REST API `/repos/<owner>/<repo>/commits/<ref>` |

This ensures that `--concurrency 4` with many jobs using the same action versions never triggers API rate limits, even with large platform repositories containing dozens of concurrent jobs.


## Trigger-aware execution

The plan already encodes how the trigger should shape execution:

- **[Profile rules](/concepts/profile-rules)** select which job profile runs — e.g.
  `plan-only` on pull requests, `apply` on merge to main.
- **[Dependency rules](/concepts/dependency-rules)** mark edges as `enforced`,
  `advisory`, or `disabled` — PR validation runs in parallel; releases enforce order.

Both are baked into the plan at compile time. The runner just executes what it's given.

## Next

- **[`orun run`](/cli/orun-run)** — full flag reference.
- **[Execution model](/concepts/execution-model)** — phases, fail-fast, concurrency.
- **[Trigger bindings](/concepts/trigger-bindings)** — how CI events shape the plan.
- **[Docker example](/examples/run-with-docker)** — end-to-end Docker workflow.
- **[GitHub Actions example](/examples/run-github-actions)** — end-to-end GHA workflow.
