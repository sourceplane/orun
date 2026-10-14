---
title: Lanes
description: Lanes group a plan's jobs into the units a runner takes, so a large plan runs on a few runners instead of one per job, without changing what runs or in what order.
---

A plan of fifty jobs fanned out as fifty GitHub jobs spends most of its time
on runner setup and on waiting for runner slots, not on work. A **lane**
groups jobs into one runner: `orun run --lane <id>` runs every job of the lane
in-process, each as soon as its upstreams finish. The plan still decides what
runs and in what order. Lanes decide only where the jobs run.

Lanes are off unless the intent asks for them. A plan from an intent without
`execution.lanes` is byte-identical to one from before lanes existed.

## The rules a lane keeps

- **One environment.** A lane never mixes environments, so promotion order
  between environments is kept and an environment's credentials stay with
  its jobs.
- **One runner.** A lane never mixes `runsOn` values.
- **`isolated` is absolute.** A component marked `lane: isolated` gets a lane
  per environment to itself.
- **No cycles between lanes.** If lane A waits on lane B, B never waits on A.
  orun checks this over enforced edges and repairs or rejects a cycle (see
  [Cycles](#cycles)).
- **Deterministic.** The same inputs give the same lanes, ids and names.

## Configure

```yaml
execution:
  lanes:
    mode: auto          # job | component | env | auto (default: job)
    maxLanes: 16        # auto mode's budget (default 16; `orun plan --max-lanes` overrides it)
    concurrency: 4      # jobs in flight inside a lane (default 4), or auto: the runner's cores
    onCycle: split      # split (default) | reject
    upstreamWait: 60m   # how long a lane job waits for an upstream in another lane (default 60m)
    groups:
      fleet:
        concurrency: 8
    rules:
      - when: { triggerRef: github-pull-request }
        mode: job       # pull requests keep one GitHub job per orun job
```

On a component:

```yaml
spec:
  lane: isolated        # or the name of a group under execution.lanes.groups
```

| Mode | Lanes |
|---|---|
| `job` | One lane per job: today's layout, with lanes named by the job's check title |
| `component` | One lane per component per environment |
| `env` | One lane per environment (and per `runsOn`) |
| `auto` | Leaves small plans alone and packs larger ones into at most `maxLanes` lanes |

Resolution, in order: the first `rules` entry whose trigger fired picks the
mode; a component's `lane` annotation (`isolated` or a group) wins over the
mode for that component; everything else follows the mode.

### Size a lane to its runner and its pool

Jobs in one lane share the runner's CPUs and memory. `concurrency` above the
runner's core count makes each job slower and gives a timing-sensitive test a
reason to fail. On a 2-vCPU runner, orun-cloud's 128-job full verify measured:

| Layout | One job's time in the lane vs alone (median / p90) |
|---|---|
| `maxLanes: 16`, `concurrency: 4` | 1.83× / 2.78×, and two tests with a 10 s timeout failed |
| `maxLanes: 56`, `concurrency: 2` | 1.20× / 1.57× |

Set `concurrency` to about the runner's core count, or to `auto` to let
`orun run --lane` read the core count of the machine it runs on (the plan
then carries `concurrency: 0` for the lane; `--concurrency` still overrides
it). A pool that mixes runner sizes gets the right number on each. Set
`maxLanes` to about the number of runners your pool starts at once: a plan of
that many jobs or fewer keeps one job per runner, and a larger one fits in a
single wave instead of queueing behind it. A CI plan job can set the budget
from the pool's free capacity rather than from the intent:
`orun plan --max-lanes 24` or `ORUN_MAX_LANES=24`; the plan records the
budget it was given and where it came from in `metadata.lanes`. Mark a
component `isolated` when its jobs must not share a runner (a browser suite,
a build that needs the whole machine).

### Share a lane's setup

Jobs of a lane share one runner but, with per-job isolation on, each gets a
private copy of the tree without `node_modules`, so each runs the install
its composition declares: a lane of twelve jobs installs twelve times. Mark
the step `scope: lane` in the composition:

```yaml
steps:
  - id: install
    name: install
    scope: lane
    run: pnpm install --frozen-lockfile
  - id: build
    name: build
    run: pnpm exec turbo run build --filter=./
```

The first job of the lane to reach it runs it once in the shared source
tree; every other job whose step is the same after templating (same
command, shell, environment and working directory) waits for that run and
reuses it, recorded as `shared with <job>`. Lane-scoped steps come first in
a job, before its private tree is staged, and the staged tree then keeps the
installed dependencies: the top-level `node_modules` is linked to the source
tree's, nested ones are copied with their symlinks intact. Jobs of different
compositions in one lane each run their own install once. A lane-scoped step
is a `run` step: an action such as `setup-node` exports its environment to
the job that ran it, so it stays per job (the tool cache is shared, so a
repeat is a few seconds).

Two more things every staged job gets: `.git` is never copied — `GIT_DIR`
and `GIT_WORK_TREE` point `git` at the source repository — and
`TURBO_CACHE_DIR` names one cache per run, so the second job of a lane hits
what the first built. `orun run --isolation component` goes further for
jobs that write only under their own directory: it copies just that
directory and links everything else to the source tree.

## Auto mode

Auto mode asks one question: is the plan big enough that runner setup and
runner slots, not work, will decide its wall-clock? If not, it changes nothing.

1. **Small plans are left alone.** At most `maxLanes` unplaced jobs keep one
   lane per job.
2. **Partition** the rest by environment and `runsOn`.
3. **Share the budget.** Lanes fixed by annotations count against
   `maxLanes`. The rest is shared between partitions in proportion to their
   job counts, at least one each.
4. **Keep chains together.** Jobs joined by an enforced `dependsOn` edge stay
   in one lane, so a migration and the worker that waits for it run in the
   same process. A chain longer than a lane's share is cut by level.
5. **Pack longest-first.** By count, or by weight when the plan carries
   weights (see [Weights](#weights)). Mark an expensive component `isolated` to keep it off a shared
   runner.

## Weights

By default every job weighs one, so a lane of three Next.js builds and a
lane of three `wrangler deploy`s are the same size to the packer, and the
whole fan-out waits for the longest lane. Three ways to tell auto mode what
a job costs, in order of precedence:

1. **`orun plan --weights <file>`**: a committed JSON file of job id to
   seconds, the reviewed form. `orun plan --write-weights lanes.lock`
   produces it from history.
2. **`orun plan --weights history`**: the median duration of each job's
   successful runs over the last ten sealed executions in this
   repository's object model.
3. **`resources.estimatedSeconds`** on the job template, declared once per
   composition.

With weights, auto mode shares the lane budget by work rather than by job
count, cuts an oversized chain by weight, places the slice with the longest
chain of work first, and fills the lane with the least work so far. A job
without a weight gets the median of those that have one. Every lane then
carries `estimatedSeconds` (the sum of its jobs' weights) and, when a job
declared `resources.cpu`, `vcpu` (the most cores one of its jobs needs);
both are on the lane matrix so a pool can size a runner per lane, and
`concurrency: auto` divides the runner's cores by `vcpu`.
`metadata.lanes.weightsFrom` records which source the plan used. A plan
whose jobs carry no weights is laid out exactly as before.

## Cycles

Grouping can create a cycle between lanes: a group holds a migration (which
another lane waits on) and a job that waits on that other lane. With
`onCycle: split` (the default), orun splits the lane by level and joins the
slices back wherever the graph stays acyclic, reporting what it did:

```text
⚠ lanes: stage/data → stage/fleet → stage/data via migrate.stage → notifications.stage, notifications.stage → export.stage — split stage/data into stage/data-1, stage/data-2
```

Auto lanes in a cycle are merged instead. With `onCycle: reject`, or
`orun plan --strict-lanes`, a cycle that would need a split fails the plan.

## See the lanes

```bash
orun plan --view lanes
```

```text
Lanes  8 lanes · mode auto

  staging
  ├─ staging · auto 1/2 · api, billing, events +2  [staging/auto-1]
  │    auto · concurrency 4 · runs-on ubuntu-22.04
  │    · billing-worker.staging.deploy
  │    · …
```

The plan carries `lanes[]` (id, name, index, environment, jobs, concurrency,
runsOn, source) and each job's `lane`.

## Run a lane

```bash
orun run --lane staging/auto-1 --remote-state --exec-id "$EXEC_ID"
```

- Jobs inside the lane run as soon as their upstreams finish, up to the
  lane's `concurrency` (`--concurrency` overrides it).
- When more jobs are ready than there are slots, a free slot goes to the job
  with the longest chain of jobs waiting on it anywhere in the plan, including
  other lanes; ties keep plan order.
- An upstream in another lane is awaited through remote state before the job
  takes a slot. With local state it must already be completed, under the same
  `--exec-id`; otherwise the run refuses and names the lane to run first.
- A failed job stops only the lane jobs that depend on it; every other job
  in the lane still runs. The plan's `execution.failFast` does not apply
  inside a lane.
- A job that does not run because an upstream failed stays pending on the
  platform. A job in another lane waiting on it stops as soon as it sees the
  failure above it, rather than waiting out the 60-minute upstream limit.
- Each job in flight gets its own Terraform plugin cache
  (`TF_PLUGIN_CACHE_DIR`), because `terraform init` is not safe against a
  second `init` writing the same cache. The caches persist, so later jobs in
  the lane reuse the providers earlier ones downloaded.
- `--retry --lane` re-opens the lane's failed jobs; completed ones stay
  completed.
- An upstream in another lane whose runner was reclaimed (its heartbeat is
  older than two of the server's leases) ends the wait, naming it; rerun its
  lane with `--retry`. A `SIGTERM` to a lane drains it: running steps stop,
  the cut jobs are reported failed as interrupted, and `--retry --lane`
  re-opens them (see [Interrupting a run](../cli/orun-run.md#interrupting-a-run)).
- On a plan without lanes, `--lane <id>` is `--job <id>`.

## In GitHub Actions

`orun plan --github-output` writes `lane-matrix` and `lane-count` next to the
unchanged `matrix`. On a plan without lanes the lane matrix has one entry per
job, so a workflow can switch to it before adopting any lane configuration:

```yaml
jobs:
  plan:
    outputs:
      lanes: ${{ steps.plan.outputs.lane-matrix }}
    steps:
      - id: plan
        run: orun plan --from-ci github --event-file "$GITHUB_EVENT_PATH" --github-output

  run:
    needs: plan
    permissions:
      contents: read
      checks: write       # per-job check runs
      id-token: write
    strategy:
      fail-fast: false
      matrix: ${{ fromJson(needs.plan.outputs.lanes) }}
    name: ${{ matrix.name }}
    steps:
      - run: orun run --lane "${{ matrix.lane }}" --remote-state --exec-id "$EXEC_ID"
        env:
          GITHUB_TOKEN: ${{ github.token }}
```

### Level by level

Every lane carries its `level` in the lane graph (0 when no other lane feeds
it, else one more than the deepest lane it waits on), `upstreamLanes` and
`upstreamJobs`; `orun plan --view lanes` shows them. A matrix fans every lane
out at once, so a lane whose upstream lane is still running takes a runner
and waits on it. To never provision a runner before its upstream lanes are
done, run the levels as chained jobs: `--github-output` also writes
`lane-matrix-<k>` for each level `k`, `lane-matrix-levels` (the array of
them) and `lane-level-count`.

```yaml
jobs:
  plan:
    outputs:
      lanes-0: ${{ steps.plan.outputs.lane-matrix-0 }}
      lanes-1: ${{ steps.plan.outputs.lane-matrix-1 }}
      lanes-2: ${{ steps.plan.outputs.lane-matrix-2 }}
    steps:
      - id: plan
        run: orun plan --from-ci github --event-file "$GITHUB_EVENT_PATH" --github-output

  run-0:
    needs: plan
    if: needs.plan.outputs.lanes-0 != '' && needs.plan.outputs.lanes-0 != '{"include":[]}'
    strategy:
      fail-fast: false
      matrix: ${{ fromJson(needs.plan.outputs.lanes-0) }}
    name: ${{ matrix.name }}
    steps:
      - run: orun run --lane "${{ matrix.lane }}" --remote-state --exec-id "$EXEC_ID"

  run-1:
    needs: [plan, run-0]
    if: needs.plan.outputs.lanes-1 != '' && needs.plan.outputs.lanes-1 != '{"include":[]}'
    strategy:
      fail-fast: false
      matrix: ${{ fromJson(needs.plan.outputs.lanes-1) }}
    name: ${{ matrix.name }}
    steps:
      - run: orun run --lane "${{ matrix.lane }}" --remote-state --exec-id "$EXEC_ID"
  # run-2 likewise, needs: [plan, run-1]
```

Declare as many levels as the plan can have (a promotion chain of two
environments is usually two or three); a level the plan does not reach is
skipped by its `if`, and everything after it with it. The trade is a barrier
per level: a lane at level 1 starts only when every level-0 lane has
finished, not when its own upstreams have. A pool that reads
`upstreamLanes` (or `upstreamJobs`) from the matrix can release each runner
on exactly its own upstreams instead. Either way the runner, once started,
still waits for any upstream that is not yet done, as before.

A GitHub job named after a lane changes when the partition changes, so branch
protection must not depend on it. A lane of more than one job therefore posts
**one check run per job**, named by the job's check title (for example
`billing-worker · stage · Verify deploy`), on the commit under test. A job that
did not run because an upstream failed is reported `cancelled`, never
`skipped`. Posting is best effort: without `checks: write` orun warns once and
continues. `ORUN_CHECK_RUNS=0` turns it off.

With several jobs in flight, each job's step groups are printed together when
it ends, titled `<check title> › <step>`, and the lane writes a per-job table
to the step summary.

## Compatibility

| You have | What changes |
|---|---|
| No `execution.lanes` | Nothing: the plan, `matrix`, `--job` are unchanged |
| A workflow on `matrix` + `--job` | Keeps working with or without lanes |
| A workflow on `lane-matrix` + `--lane`, no lanes configured | One GitHub job per orun job, as before |
| Older orun reading a plan with lanes | Ignores `lanes` and `lane`; `--job` still runs every job |
