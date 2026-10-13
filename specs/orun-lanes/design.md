# Design — orun-lanes

## 1. The problem, measured

A plan job is the unit of the DAG, of the remote-state lease, of the log and of
the check a reviewer reads. Since the CI pattern was settled (one matrix entry
per job, `orun run --job <id>`), it has also been the unit of the **runner**,
and the runner is where the time goes:

| Cirrus `04-workers` convergence (49 jobs) | Time |
|---|---|
| Per job: checkout, plan download, registry login, orun install, toolchain download, workspace install | ~70s |
| Per job: build, typecheck, deploy (the work) | ~20s |
| Runner slots (`max-parallel`) | 16 |
| Wall-clock | 6m59s — 49 jobs through 16 slots is three to four rounds of ~90s |

`orun run` without `--job` already runs a whole plan in one process: the
dependency scheduler, `--concurrency`, per-job workspace isolation, per-job
claims and heartbeats against remote state. What is missing is a unit between
"one job" and "everything" that the CI matrix can fan out over. This design
adds it.

## 2. The lane

A **lane** is a set of plan jobs that one runner executes in one `orun run`
process.

- A lane holds jobs of **exactly one environment**.
- A job belongs to **exactly one lane**.
- The smallest lane is one job; the largest is one environment.

The plan records the partition:

```json
{
  "jobs": [
    { "id": "billing-worker.stage.verify-deploy", "index": 7, "lane": "stage/fleet", "...": "..." }
  ],
  "lanes": [
    {
      "id": "stage/fleet",
      "name": "stage · fleet · 12 jobs",
      "index": 3,
      "environment": "stage",
      "jobs": ["admin-worker.stage.verify-deploy", "..."],
      "concurrency": 8,
      "runsOn": "",
      "source": "group",
      "group": "fleet"
    }
  ]
}
```

`source` is one of `job`, `component`, `env`, `group`, `isolated`, `auto`.
A lane produced by a cycle repair also carries `splitFrom` and `reason`
(§7). Job `index` keeps its meaning (the job's position in `jobs`); lane
`index` is the lane's position in `lanes`.

## 3. Configuration

### 3.1 `intent.yaml`

```yaml
execution:
  lanes:
    mode: auto            # job | component | env | auto       (default: job)
    maxLanes: 16          # auto: never emit more lanes than this (default 16)
    concurrency: 4        # jobs in flight inside one lane     (default 4)
    onCycle: split        # split | reject                     (default split)
    groups:               # named lanes a component can join
      fleet:
        concurrency: 8
    rules:                # first match on the active trigger wins
      - mode: job
        when:
          triggerRef: github-pull-request
```

- `rules[].when.triggerRef` must name a binding in `automation.triggerBindings`
  (the same check `dependencyRules` and `profileRules` get).
- `groups` names must be lowercase `[a-z0-9-]+`; `job`, `isolated` and `auto`
  are reserved.
- Each group's `concurrency` defaults to the lanes default.

### 3.2 `component.yaml`

```yaml
spec:
  lane: fleet         # a group from execution.lanes.groups
  # lane: isolated    # this component's jobs never share a runner
```

`spec.lane` naming an undeclared group is a plan-time error. The field is also
accepted on an inline `components:` entry in `intent.yaml`.

### 3.3 Resolution

1. The **effective mode** is the first `rules` entry whose trigger is active,
   else `mode`.
2. **`job` means no grouping at all.** Every job is its own lane, and `spec.lane`
   annotations are ignored. This makes a `rules` entry for pull requests a
   one-line way to keep one check per job where reviewers want it.
3. In every other mode, annotations are honoured first:
   - `isolated`: one lane per (component, environment).
   - A group: one lane per (group, environment).
4. The remaining jobs are laid out by the mode:
   - `component`: one lane per (component, environment).
   - `env`: one lane per environment.
   - `auto`: §6.

## 4. Invariants the planner enforces

1. **Environment boundary.** A lane never holds jobs of two environments. One
   credential scope, one blast radius; promotion between environments stays an
   edge between lanes.
2. **Runner boundary.** Jobs with different `runsOn` never share a lane, in
   every mode. A group that spans two `runsOn` values is split per value.
3. **`isolated` is absolute.** No mode, group or repair puts another job into
   an isolated component's lane.
4. **No cycles between lanes** (§7). The lane graph is the job graph's
   *enforced* `dependsOn` edges contracted by lane. `advisoryDependsOn`
   creates no lane edges.
5. **Determinism.** The same plan inputs produce the same lanes, ids, names
   and indexes, so `orun plan` stays byte-identical for identical inputs.
6. **Absence is invisible.** With no `execution.lanes`, the plan has no
   `lanes` key and no job has a `lane` field, whatever `spec.lane`
   annotations exist (they only take effect under a mode that groups). The
   plan, its checksum and its revision key are exactly what they are today.

## 5. Ids, names and order

| Source | Lane id | Lane name (the CI job title) |
|---|---|---|
| `job` | the job id | the job's `checkName` (today's title) |
| `component` | `<env>/<component>` | `<env> · <component>` |
| `group` | `<env>/<group>` | `<env> · <group> · <n> jobs` |
| `env` | `<env>` | `<env> · all · <n> jobs` |
| `isolated` | `<env>/<component>` | `<env> · <component> · isolated` |
| `auto` | `<env>/auto-<k>` | `<env> · <label> <k>/<n> · <a>, <b>, <c> +<m>` |
| split (§7) | `<base>-<k>` | the base name with `·<k>` after the group |

- **The auto `<label>`** is the lane's composition when all of its jobs share
  one, else `auto`.
- **Listed components** are the first three by name, with a common `-worker`,
  `-service` or `-app` suffix dropped.
- **Names are capped at 80 characters**, the width GitHub shows in a run's
  sidebar.

**Order and index.** Lanes are ordered by:
1. environment, in the order environments first appear in the plan's
   topological job order;
2. then the lowest topological level of the lane's jobs;
3. then lane id.

`index` is the position in that order. A lane name is cosmetic: it can change
when the partition changes. **Per-job identity never does** (§9).

## 6. Auto mode

Auto mode answers one question: is this plan big enough that runner setup and
runner slots, not work, will decide its wall-clock? If not, it changes nothing.

1. **Leave small plans alone.** If the plan has at most `maxLanes` jobs that
   no annotation already placed, auto lays them out exactly as `job` mode
   does. A typical pull request touching a few components keeps one lane per
   job.
2. **Partition** the unplaced jobs by (environment, `runsOn`).
3. **Budget.**
   - Lanes already fixed by annotations count against `maxLanes`.
   - The rest is shared between partitions in proportion to their job counts.
   - Every partition gets at least one lane, and a partition never gets more
     lanes than jobs.
4. **Keep chains together.** Within a partition, join jobs connected by an
   enforced `dependsOn` edge into clusters (union-find). A migration and the
   worker that waits for it land in the same lane, so the wait is in-process.
   A cluster larger than `ceil(jobs / lanes)` is cut by topological level
   into consecutive slices, each within that size.
5. **Pack longest-first.**
   - Sort the clusters by size (descending), then lowest level, then the earliest
     job in plan order.
   - Place each into the currently smallest lane; ties go to the lowest lane
     number.
   - Every job weighs one. There are **no per-job weights** in this epic
     (README, out of scope); an expensive component that must not share a
     runner is marked `isolated`.
6. **Repair** whatever cycles remain (§7). Auto lanes are merged rather than
   split when they share a partition.

## 7. Cycles between lanes

A cycle between lanes does not deadlock on its own: jobs wait on jobs, and if
every lane in the cycle is running, every wait resolves. It deadlocks when one
of those lanes cannot get a runner (`maxLanes`, `max-parallel`, a pool limit)
while another lane holds a slot waiting on it. An acyclic lane graph can always
run in some order under any slot limit; that is the guarantee this rule buys.

The planner computes the lane graph's strongly connected components (SCCs).
For every SCC with more than one lane:

| Lanes in the SCC | Repair |
|---|---|
| All `auto`, same partition | **Merge** them into one lane. Silent: auto chose the layout, so a cycle from auto is auto's to fix. |
| Any `group`, `component` or `env` lane | **Split** each such lane in the SCC by topological level (§7.1). The lane keeps its group and runner and gets a `-<k>` suffix. A warning names the edges. |
| An `isolated` lane | Never merged with anything. Split by level only if splitting the other lanes cannot break the cycle; no other job ever joins it. |

With `onCycle: reject` (or `orun plan --strict-lanes`), a cycle that would need
a split is a plan error naming it:
`lanes stage/data → stage/fleet → stage/data via notifications-worker.stage.deploy → analytics-export.stage.deploy`.

### 7.1 Why splitting always works

- Give every job a **level**: the length of the longest chain of enforced
  dependencies that ends at it.
- Every enforced edge points from a lower level to a strictly higher one.
- A lane split into slices of consecutive levels therefore has edges only
  from earlier slices to later ones.
- The worst case — one slice per level — is acyclic by construction.

The planner splits at the fewest level cuts that break the SCC: it tries one
cut, then two, and stops at the first that works. It never needs to reject.

## 8. Execution: `orun run --lane <id>`

- **Selection.** `--lane <id>` runs the jobs whose `lane` is `<id>`. On a
  plan without a `lanes` block, `--lane <id>` is `--job <id>`. A CI workflow
  that switches to `--lane` therefore works against old plans and new ones
  alike. `--lane` and `--job` are mutually exclusive.
- **Concurrency** is the lane's `concurrency`; `--concurrency` overrides it.
  Each job runs in its own isolated workspace (the existing
  `--isolation auto` behaviour when concurrency > 1).
- **Dependencies inside the lane** go through the existing in-process
  scheduler.
- **Dependencies outside the lane:**
  - **With remote state**, a job whose upstream is in another lane waits for
    it through the same claim loop `--job` uses today. A waiting job does not
    occupy a concurrency slot.
  - **With local state**, an outside upstream must already be completed in the
    run's state. Otherwise the run fails, naming the lane to run first.
- **Claims and the platform.** Every job is claimed, heartbeated and finished
  individually, exactly as today. The platform keeps one row per job; the
  plan it receives carries each job's `lane`.
- **Resume.** `--retry` with `--lane` re-opens the lane's failed jobs and
  leaves its completed ones completed. `gh run rerun --failed` on a lane is
  therefore a true resume, as it is for a job today.
- **Failure.** A failed job stops the jobs in its lane that depend on it; the
  rest of the lane continues unless the plan sets `failFast`.

## 9. The GitHub surface

### 9.1 The lane matrix

`orun plan --github-output` keeps writing `matrix` (one entry per job,
unchanged) and adds:

- `lane-matrix`: `{"include":[{"lane":…,"name":…,"index":…,"env":…,"jobs":…,"runsOn":…}]}`
- `lane-count`: the number of lanes.

On a plan without lanes, `lane-matrix` has one entry per job, `lane` = job id
and `name` = `checkName`. A workflow can switch to it before it adopts any
lane configuration.

### 9.2 Per-job check runs

With grouping, a GitHub job is a lane, and lane names change when the partition
changes. Branch protection must not depend on them. So:

- When a lane holds **more than one job** and runs in GitHub Actions with a
  `GITHUB_TOKEN` that can write checks (`permissions: checks: write`), orun
  creates **one check run per job**.
  - Named with the job's `checkName`: today's title, e.g.
    `billing-worker · stage · Verify deploy`.
  - On the commit under test (the PR head for `pull_request`, else
    `GITHUB_SHA`).
  - Set to in progress when the job starts, completed with its conclusion when
    it ends, linking to the lane's job log.
- Single-job lanes post nothing: the GitHub job already is the check, exactly
  as today.
- **Best effort.** A failure to post is a warning, never a job failure.
  `ORUN_CHECK_RUNS=0` turns posting off.

### 9.3 Logs and summary

- With more than one job in flight, each job's output is held until the job
  ends and then printed as one contiguous block, so concurrent jobs never
  interleave. GitHub cannot nest groups, so the block keeps one
  `::group::` per step, each titled `<checkName> › <step>`.
- The lane writes a table to `$GITHUB_STEP_SUMMARY`: job, status, duration,
  and the upstream it waited on.

## 10. `orun plan --view lanes`

`orun plan --view lanes` prints, per environment:
- each lane's id, name, source, concurrency and jobs;
- every cycle repair, with the edges that forced it.

The default plan summary adds one line when lanes are present:
`lanes  stage 3 · prod 3 · dev 2  (auto)`.

## 11. Compatibility

| Surface | Before | After, with no lane configuration | After, with lanes |
|---|---|---|---|
| plan.json | jobs only | byte-identical | `lanes[]` + `jobs[].lane` |
| `--github-output` `matrix` | per job | unchanged | unchanged |
| `--github-output` `lane-matrix` | — | one entry per job | one entry per lane |
| `orun run --job` | runs one job | unchanged | unchanged (any job, any lane) |
| `orun run --lane` | — | same as `--job` | runs the lane |
| Check runs | one per GitHub job | unchanged | one per plan job, posted by orun |
| Platform job rows | one per job | unchanged | unchanged, each with its `lane` |

Older orun binaries reading a plan with `lanes` ignore the unknown fields; a
lane-unaware CI keeps fanning out per job from `matrix`.

## 12. Platform notes (orun-cloud, out of this repo)

- The run detail page can group job rows by `lane`.
- Managed runners key their label on the matrix `index`
  (`ghr-orun-job:<run>-<index>`). For `lane-matrix` that is the lane's index.
  Mapping a lane index back to its jobs is a platform follow-up. Until it
  ships, managed-runner pools keep `mode: job`; GitHub-hosted runners need
  nothing.
