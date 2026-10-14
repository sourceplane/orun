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
    maxLanes: 16        # auto mode's budget (default 16)
    concurrency: 4      # jobs in flight inside a lane (default 4)
    onCycle: split      # split (default) | reject
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
5. **Pack longest-first.** Every job weighs one; there are no per-job
   weights. Mark an expensive component `isolated` to keep it off a shared
   runner.

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
- A failed job stops only the lane jobs that depend on it.
- `--retry --lane` re-opens the lane's failed jobs; completed ones stay
  completed.
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
