# Spec: orun-runner-efficiency (work per provisioned vCPU)

**When a plan fans out over a fleet of spot runners, most of the vCPU-seconds
orun provisions are spent not running jobs.** A 200-vCPU AWS spot quota is a
budget of *concurrent* vCPUs; orun today spends that budget on runner setup
that every job repeats, on runners that sit polling for an upstream lane, on
duplicated installs and builds inside a lane, and on lane shapes chosen by job
count rather than job weight. This document is the analysis — each finding
with the code that causes it and, where it could be measured in this
repository, the number — and the proposed changes, ordered by effect on a
quota-bound fleet.

It builds on `specs/orun-lanes/`, which solved the first half of the problem
(one runner per job) and deliberately left the second half (weights, fleet
awareness) for later. This is that later.

## Status

| Field | Value |
|-------|-------|
| Status | **Analysis and proposal — not started.** |
| Builds on | `specs/orun-lanes/` (the lane, auto mode, `--lane`), `specs/orun-native-coordination/` (claim/heartbeat/complete, the runnable frontier), `internal/runner`, `internal/lanes`, `cmd/orun/run_lane.go` |
| Pairs with | `orun-managed-runners` (the gate that releases a lane only when its dependencies are done — v2.71.0 notes); the platform owns the pool, this spec owns what the plan tells the pool |
| Constraint | A plan without `execution.lanes` stays byte-identical. Nothing here changes a job's identity, its check name, or the per-job claim/heartbeat/complete contract. |

## 1. The one-paragraph thesis

A quota-bound fleet is efficient when every provisioned vCPU-second is spent
on a job's own work. orun's execution model has four places where that fails,
in decreasing order of cost: **(1)** a runner is provisioned before its work is
runnable and idles on the VM, polling; **(2)** per-runner setup (checkout,
toolchains, install) is paid once per lane, but the lane count is capped at a
constant and the lane's internal concurrency is a constant too, neither of
them a function of the machine or the fleet; **(3)** inside a lane, per-job
workspace isolation re-does the install and the build for every job and copies
`.git` per job; **(4)** auto mode packs by job count, so lanes have very
different lengths and the whole fan-out waits for the longest. Two further
defects are in the full-plan scheduler (`orun run` without `--lane`), measured
below at 4× and 30% on trivial plans. The fixes are mostly small: tell the pool
when a lane may start, derive concurrency from the machine, declare job weight
in the composition, share the lane's setup, and route the full-plan path
through the scheduler the lane path already has.

## 2. Where the time goes today

The lane spec measured a Cirrus `04-workers` convergence (`specs/orun-lanes/design.md`
§1): per job ~70 s of runner setup for ~20 s of work, 49 jobs through 16
slots, 6m59s wall-clock. [`model.py`](model.py) turns that into a
discrete-time model of a Cirrus-shaped plan on a 200-vCPU pool — 88 jobs in
two environments with promotion, migration → worker chains, a 20 s / 60 s /
180 s mix of work with the heavy jobs sharing one migration — and runs the
regimes orun supports today:

| Regime (4-vCPU runners) | Runners | Wall | vCPU·h per run | Peak vCPU | vCPU·s on job work | Runs/h the quota sustains |
|---|---|---|---|---|---|---|
| `mode: job`, one runner per job | 88 | 7.9 min | 13.4 | 200 | 11.9 % | 7.6 |
| `mode: auto`, 16 lanes, concurrency 4, each job re-installs | 16 | 15.2 min | 6.5 | 64 | 24.5 % | 12.3 |

Three things the model makes visible. **Lanes already halve the vCPU-hours**
and free two thirds of the quota, which is why they were the right first
step. **Lanes alone do not raise the useful share much, because the install
CI did once per runner moved inside the lane and is now done once per job.**
And **count-based packing doubles the wall-clock** on this shape: the four
heavy jobs share a migration, so they share a lane, and the whole run waits
for it. §8 runs the same model with each proposal switched on.

## 3. Findings

Each finding names the code, says what it costs on a spot fleet, and marks
whether the cost was measured here (**M**) or reasoned from the code and the
lane spec's measurements (**R**).

### F1 — Runners are provisioned before their work is runnable, then idle on the VM (R)

A GitHub matrix starts every entry as soon as a slot is free. A lane whose
upstream lane has not finished takes a runner and waits *on that runner*:

- `orun run --lane`: `AwaitUpstream` polls `LoadRunState` every 2 s backing
  off to 15 s, for up to **60 minutes** (`cmd/orun/run_lane.go:19-22`,
  `internal/runner/lane.go:186-195`).
- `orun run --job`: the claim loop polls `/runnable` from 2 s to 60 s for up to
  **30 minutes** (`cmd/orun/command_run.go:882-884`, `waitForJobRunnable`
  at `:995`).

Lanes are ordered by environment, then topological level
(`specs/orun-lanes/design.md` §5), which helps when `max-parallel` is small
and the pool starts entries in order. It does not help when the pool has room
for every lane at once, which is exactly the 200-vCPU case: every lane is
provisioned at t = 0 and every lane at level ≥ 1 idles until level 0 finishes.
With promotion (`production` depends on `staging`), the production lanes are
100 % idle for the whole staging phase. A 4-vCPU runner idling for 5 minutes
is 1,200 vCPU·s, more than the whole run's job work in the table above.

The `lane-matrix` entry carries nothing a pool could gate on: `lane`, `name`,
`index`, `env`, `jobs`, `runsOn` (`cmd/orun/github_matrix.go:27-34`). The
level and the upstream lanes are known at plan time and not emitted.

### F2 — Setup amortisation is capped by constants, not by the fleet or the machine (R)

- `maxLanes` defaults to **16** (`internal/model/intent.go:59`) whatever the
  plan or the pool. On 50 free slots, 16 lanes leave 34 slots unused *and*
  make each lane longer than it needs to be.
- A lane's `concurrency` defaults to **4** (`internal/model/intent.go:60`,
  `internal/lanes/lanes.go:106`) whatever the runner. On a 16-vCPU runner, 4
  jobs in flight idle 12 vCPUs during the work phase; on a 2-vCPU runner, 4
  builds thrash. Nothing in `internal/runner` or `cmd/orun` reads
  `runtime.NumCPU()` (grep: no hits outside the GHA engine's `runtime` import
  for `GOOS`).
- `runsOn` is the only runner descriptor a composition can set
  (`internal/model/composition.go:104`); there is no CPU or memory
  declaration, so a pool cannot right-size an instance for a lane and the
  planner cannot keep two 8-vCPU builds out of one 8-vCPU lane.

### F3 — Inside a lane, every job re-installs, re-builds, and copies `.git` (M for the copy, R for the install)

With concurrency > 1 the runner stages a private copy of the whole source tree
per job (`shouldIsolate`, `internal/runner/runner.go:1539`;
`stageJobWorkspace`, `internal/runner/workspace.go:63`):

- The copy skips `node_modules`, `.turbo`, `dist`, `.terraform`, … by name
  (`workspace.go:40-55`), so the job's tree has **no dependencies and no build
  cache**. The compositions then install and build from scratch: the turbo
  compositions run `pnpm install --no-frozen-lockfile` and
  `pnpm exec turbo run build` as plan steps
  (`examples/compositions/compositions/cloudflare-pages-turbo/jobs/…-verify-deploy.yaml`).
  A 12-job lane does 12 installs and 12 cold builds of the same workspace, in
  parallel, on one runner. This is the install CI used to do once per job
  runner, relocated — the lane saved the VM but not the work.
- The copy does **not** skip `.git` (only `.orun` is skipped by name,
  `workspace.go:116`). CI checks out with `fetch-depth: 0`
  (`.github/workflows/orun-default-workflow.yaml`, `examples/github-actions/remote-state-matrix.yml`),
  so the full history is byte-copied per job.
- On ext4 — the default root filesystem of an AWS AMI — the reflink attempt
  fails per file and falls back to a byte copy, after having already created
  and removed the destination once (`materializeFile`, `workspace.go:163-168`;
  `workspace_clone_linux.go`): two opens, one failed ioctl, one unlink and a
  full copy per file.

**Measured** on this checkout (1,780 files, 17.4 MB of which `.git` is 5 MB,
ext4): 130–160 ms to stage and ~95 ms to remove, per job. Small here; it
scales with the tree, and a product monorepo with full history is one to two
orders of magnitude larger. The install and the build are the dominant cost
and were not measured here (no toolchain in the probe), but they are the
`~40 s` of "workspace install" the lane spec attributes to the runner.

### F4 — Auto mode packs by count, so lanes differ in length and the fan-out waits for the longest (R)

`packPartition` weighs every job one (`internal/lanes/auto.go:22`, bins by
`len`), slices chains at `capacity = ceil(jobs / lanes)` (`auto.go:183`) and
packs longest-cluster-first. The spec says so (§6, "no per-job weights"). On
a real plan a lane of three Next.js builds and a lane of three
`wrangler deploy`s are the same size to the packer and differ by minutes in
length. Because the GitHub job downstream (`needs:` the matrix) and the
promotion edge both wait for the last lane, the whole fleet's wall-clock is
the longest lane's, and every shorter lane's runner is released early only to
be idle in the quota sense (the run is not done; the next run is not started).

orun already records what it would need: every sealed execution carries per
job and per step `StartedAt`/`FinishedAt`
(`internal/runworktree/worktree.go`, `Project`). Nothing reads them back at
plan time.

### F5 — The full-plan scheduler is batch-synchronous and serialises components (M)

`orun run` without `--lane` goes through `runConcurrent`
(`internal/runner/runner.go:967`). Two defects, measured with a fake executor
that sleeps for the step's declared duration (`--isolation none`, concurrency
4; the probe is in §7):

1. **Batch-synchronous.** Each pass computes the ready set, launches it, then
   `wg.Wait()`s for *all* of it before the next pass (`runner.go:1180`). A job
   that becomes ready when one job of the batch finishes waits for the whole
   batch. A 1,000 ms job next to a chain of four 100 ms jobs: **1,303 ms**
   through `runConcurrent`, **1,002 ms** through `runLane` (which is
   event-driven: one goroutine per job, each waiting on its upstreams' `done`
   channels, `internal/runner/lane.go:160-173`).
2. **`--component-concurrency` defaults to 1** (`cmd/orun/command_run.go:106`,
   `runner.go:390`, "keeps the dashboard component-grouped"). With the batch
   loop emptying `activeComps` every pass, the pick loop
   (`runner.go:1063-1076`) admits one new component per pass. Four
   independent 300 ms jobs on four components at `--concurrency 4`:
   **1,204 ms** (serial) with the default, **302 ms** with
   `--component-concurrency 0`, **301 ms** as a lane.

Neither affects `--lane` or `--job`, which is why Cirrus did not see them;
both affect the local `orun run`, `--env` fan-out
(`examples/github-actions/remote-state-matrix.yml`, `run-env-fanout`), and
`--background`, and the README's "orun run executes all four jobs locally".

### F6 — A spot reclaim is a silent 5-minute hole, then a manual rerun (R)

`orun run` installs no signal handler (`signal.` appears only in
`cmd/orun/command_logs.go`). On a spot interruption the instance is
terminated; the job's lease lapses on the server after 60 s
(`coordination-api.md`: lease 60 s, heartbeat 20 s). The *client-side* dependency
wait, however, treats a running upstream as live until its heartbeat is
**5 minutes** old (`heartbeatTimeout`, `cmd/orun/command_run.go:1001`), so a
downstream lane on another runner polls for four more minutes than the server
would need, and the lane-level wait (`awaitRemoteUpstreams`) never checks
heartbeat age at all — it waits for `completed` or `failed`, up to 60 minutes.
Nothing re-dispatches the reclaimed lane; `--retry --lane` exists but someone
has to run it, and it re-runs the job from its first step (a retried job's
state is reset, `runner.go:451-453`).

### F7 — Waiting is polling; a hung step holds a runner for the CI limit (R)

- Every waiter polls `LoadRunState` (the full run's state; on the v2
  coordination path a fold of the run's event log) every 15 s. Latency per
  cross-lane edge is up to 15 s plus the poll's own cost; a chain of four
  cross-lane edges can idle a runner a minute on polling alone. `status --watch`
  already waits on `ReadLog(wait=)` (`specs/orun-native-coordination/README.md`,
  NC3); the runner's waits do not.
- The runner has no default job or step timeout (`resolveTimeout`,
  `runner.go:1608`: step, else job, else none). Compositions in `examples/`
  set `timeout: 10m–30m`; one that does not turns a hung `terraform apply` or
  a stuck `wrangler` into a runner held until GitHub's 6-hour job limit.

### F8 — Smaller, in-process (R)

- A step's whole output is buffered in memory until the step ends
  (`internal/executor/local.go:41`, `docker.go:62`), and with several jobs in
  flight a job's whole output is held until the job ends
  (`internal/runner/gha_presenter.go:251`). On a memory-tight spot instance a
  chatty build competes with itself; nothing streams to the cockpit.
- The Docker executor starts a fresh `docker run --rm` **per step**
  (`internal/executor/docker.go:49-60`): a container create/teardown per step,
  and nothing installed in step 1 exists in step 2 unless it landed in the
  mounted workspace.
- Local runs project the whole run snapshot to `run.json` on every job and
  step tick (`internal/objrun/objrun.go:138-146` → `WorkTree.Project` →
  `writeJSONAtomic`): O(ticks × jobs) JSON, serialised under one mutex. Not on
  the remote path; noted for completeness.

## 4. What is already right

So the proposals do not re-solve it: per-job claims, heartbeats and leases are
cheap (one request per job per 20 s); the lane runner's scheduler is correct
and event-driven; `laneRecord.waited` already records *which* upstream a job
waited on; the plan already emits a stable `index` the pool keys a runner on
(v2.71.0); hermetic memoisation exists on the v2 coordination path (a cache
hit skips execution, `cmd/orun/command_run.go:721`). The gaps are in what the
plan tells the fleet and in what a lane does with the machine it gets.

## 5. Proposals

Ordered by effect on a quota-bound fleet. Each names the code it touches and
the invariant it keeps.

### 5.1 Tell the pool when a lane may start (F1) — small change, largest effect

Add to each `lane-matrix` entry (and to `plan.lanes[]`):

```json
{ "lane": "stage/auto-3", "level": 1, "upstreamLanes": ["stage/auto-1"],
  "upstreamJobs": ["billing-db.stage.migrate"], "estimatedSeconds": 140, "vcpu": 4 }
```

- `level`: the lowest topological level of the lane's jobs (already computed
  for ordering, §5 of the lane spec).
- `upstreamLanes`: the lanes the enforced edges contract to (the lane graph
  already exists for cycle repair, `internal/lanes/graph.go`);
  `upstreamJobs`: the jobs outside the lane that lane jobs depend on.

Two consumers, neither of which needs a change to the runner:

1. **A pool gate** (the `orun-managed-runners` milestone): provision the
   runner for a lane only when its upstream work is done. The run coordinator
   already knows; the gate reads the same run state the lane would have
   polled, from one place instead of from every runner. Two policies, and the
   model in §9 shows they are a real trade:
   - **`all`** — release when every `upstreamJobs` entry is complete. No
     runner ever idles on an upstream; peak quota use halves. The cost is the
     critical path: a chain lane's level-0 job (the production migration) now
     runs after the gate instead of before it, and the runner's setup lands
     after the gate too. Pairs with **pre-warm**: release at
     `estimatedSeconds(upstream) − setup`, so the setup overlaps the upstream's
     tail.
   - **`first`** — release when some lane job with no in-lane upstream has its
     cross-lane upstreams complete. No critical-path cost; the runner may idle
     for its later jobs, bounded by the in-lane chain.
   A quota-bound pool wants `all` + pre-warm; a latency-bound one wants
   `first`. The plan carries what both need. With either, the `AwaitUpstream`
   path becomes the fallback for GitHub-hosted runners.
2. **Levelled matrices for GitHub-hosted runners** — `orun plan --github-output`
   additionally writes `lane-matrix-levels`: a list of matrices, one per level,
   so a workflow can chain `run-l0 → run-l1 → …` with `needs:`. GitHub then
   does the gating. It costs a barrier per level (no overlap between levels),
   which on a spot quota is the right trade: an idle slot is free; an idle
   runner is not.

Keeps: `lane-matrix` entries only gain fields; a workflow on the current shape
is unchanged.

### 5.2 Size the lane to the machine and the budget to the fleet (F2)

- `execution.lanes.concurrency: auto` (and the default when unset): the
  runner resolves it at start as `max(1, NumCPU / cpuPerJob)`, where
  `cpuPerJob` is the lane's declared weight (5.4) or 2 when nothing is
  declared. `--concurrency` still overrides. One function in
  `cmd/orun/run_lane.go` (`selectLane` returns the lane's value today).
- `orun plan --max-lanes N` and `ORUN_MAX_LANES`, overriding `maxLanes` at
  plan time, so a CI plan job can set the budget from the pool's free capacity
  (`quota_vcpu / vcpu_per_runner`, or whatever the pool reports) rather than
  from a constant in `intent.yaml`. Determinism holds for identical inputs; the
  flag is an input and is recorded in `plan.metadata`.
- A guidance note in `concepts/lanes.md`: size the lane to the runner, not
  the runner to the plan. A lane's parallel frontier is narrow when its jobs
  form chains (a migration, then its workers), so `concurrency × cpu per job
  ≈ vCPU` on a *small* runner beats a large runner that idles through the
  chain. On the model in §8, 16-vCPU runners with two lanes per environment
  cost the same vCPU-hours as today; 4-vCPU runners with four lanes per
  environment and concurrency 3 cost 45 % less at the same wall-clock. Large
  runners pay only for wide lanes (many independent jobs), which the
  `estimatedSeconds`/`vcpu` fields let the pool recognise per lane.

### 5.3 Share the lane's setup; isolate the component, not the repository (F3)

Three changes to `internal/runner/workspace.go` and one to the composition
model:

1. **Never copy `.git`.** Add it to the skip set and set `GIT_DIR` /
   `GIT_WORK_TREE` in the job env to the source tree (or stage with
   `git worktree add --detach`, which shares objects by design). Tools that
   read the repository (`turbo --filter=...[HEAD^]`, `wrangler` metadata,
   `orun` itself) keep working; nothing is written to `.git` by a build.
2. **`isolation: component`** (new mode, and the `auto` behaviour when
   `job.Path` is set): stage only the job's own directory (`job.Path`) as a
   private copy and symlink every other top-level entry of the source tree
   into the staged root. A component's build writes under its own directory
   and `dist/`; the shared `node_modules`, `pnpm-lock.yaml`, `turbo.json` and
   the sibling packages stay one copy. This makes the staged tree
   O(component) instead of O(repository) and lets a single install serve the
   lane. Jobs that write outside their path keep `isolation: workspace`.
3. **A lane prelude.** The runner already has `step.Phase`
   (`normalizeStepPhase`). Mark composition steps `phase: setup` and
   `scope: lane` — `setup-node`, `setup-pnpm`, `pnpm install` — and the lane
   runner executes the *first* job's lane-scoped steps once in the shared
   tree before isolation, then skips them in every other job of the lane whose
   steps are byte-identical after templating (same `run`, same `use`/`with`,
   same env). The skipped steps are recorded `completed` with a note
   (`shared with <jobID>`), so the check run and the sealed record stay whole.
   Jobs of different compositions in one auto lane simply each run their own
   prelude once. This turns 12 installs into 1 per composition per lane.
4. **Point the caches at the lane.** `TURBO_CACHE_DIR`, `npm_config_cache`,
   `PNPM_HOME`'s store and `TF_PLUGIN_CACHE_DIR` default to a lane-level
   directory under `.orun/runs/<execID>/cache/` when unset. All four are
   content-addressed and concurrent-safe; the second job of a lane then hits
   the first job's turbo cache instead of rebuilding.
5. **Reflink by AMI, not by luck.** Document that the runner's workspace
   volume should be XFS with `reflink=1` (the default `mkfs.xfs` since 5.1)
   or btrfs; the existing `FICLONE` path then makes the remaining per-job
   copies metadata-only. One line in the pool's image build; no orun change.

Keeps: `isolation: workspace` and `none` unchanged; a composition without
`scope: lane` runs exactly as today.

### 5.4 Weigh jobs; pack by critical path (F4)

- **Declared weight first.** A `JobTemplate` gains
  `resources: { cpu: 2, memory: 4Gi, estimatedSeconds: 90 }`
  (`internal/model/composition.go`), carried to `plan.jobs[]`. Auto mode packs
  by `estimatedSeconds` (falling back to 1) with longest-processing-time first
  over *chain length* rather than cluster size: sort clusters by the sum of
  their critical path, place into the lane with the smallest current total.
  Lane `concurrency` then also respects `cpu`: a lane never has more than
  `NumCPU` worth of declared cpu in flight.
- **Measured weight second.** `orun plan --weights history` reads the last N
  sealed executions for the same revision family and uses the median
  `FinishedAt − StartedAt` per job id as its weight; `--weights path/to/lanes.lock`
  reads a committed file, and `orun plan --write-weights` produces it. This is
  the `lanes.lock` the lane spec deferred (README, "out of scope"). Determinism:
  weights are a plan input and are hashed into the plan; a plan without
  weights is byte-identical to today.
- Emit `estimatedSeconds` per lane into the lane matrix (5.1), so the pool can
  pick an instance size per lane.

### 5.5 Route the full-plan path through the lane scheduler (F5) — small change, measured effect

`runConcurrent` should be the lane runner with every job in the lane:
`runLane` already does per-job goroutines, upstream channels, a semaphore and
fail-fast. `runConcurrent`'s only extra is the component cap, which exists for
the dashboard. Keep the grouping as a presentation concern (the live region
already groups finished jobs by component) and default
`--component-concurrency` to 0. Regression tests: the two probes in §7 as
assertions (`< 1.1 s` and `< 0.4 s`).

### 5.6 Drain on signal; trust the server's lease (F6)

- `orun run` handles `SIGTERM`/`SIGINT`: stop admitting jobs, cancel running
  steps, send `:complete` with `failed` and a `preempted` note for the jobs it
  held (or a new `:release` so the job returns to `pending` rather than
  `failed`), flush the log pipeline, exit non-zero. On a spot runner the pool
  forwards the two-minute interruption notice as `SIGTERM`.
- `--on-interrupt resume`: a released job resumes at its first incomplete
  step on the next `--retry --lane` (the step map is already persisted in the
  job state; `--retry` should clear only steps after the last completed one
  rather than nil the job).
- The client-side `heartbeatTimeout` (5 min) becomes the server's lease TTL
  from the claim (60 s), and `awaitRemoteUpstreams` checks heartbeat age the
  way `waitForJobRunnable` does, so a downstream lane learns of a reclaim in
  one lease, not five minutes.
- Pool side (out of this repo): re-dispatch a lane whose runner was reclaimed
  with `--retry --lane`; the plan carries everything it needs.

### 5.7 Wait on events; bound every wait (F7)

- `awaitRemoteUpstreams` and `waitForJobRunnable` use `ReadLog(wait=)` where
  the backend supports it (`statebackend.Backend` grows a `WaitRunnable(ctx,
  runID, jobID, timeout)` with a polling default), so a job starts within
  milliseconds of its upstream finishing and a waiting runner costs the server
  one open request rather than one fold per 15 s.
- A default job timeout (`execution.defaults.timeout`, default 60 m) and a
  default step timeout of the job's, so no composition can hold a runner for
  the CI limit; `laneUpstreamWait` becomes `execution.lanes.upstreamWait`.

### 5.8 Stream and reuse (F8) — later

Stream step output through a bounded pipe to the sinks instead of a
`bytes.Buffer`; keep one container per job in the Docker executor (`docker
create` once, `docker exec` per step); coalesce `run.json` projections on a
timer. None of these change the vCPU story; they change memory and latency.

## 6. Milestones

| ID | Milestone | Touches | Done when |
|----|-----------|---------|-----------|
| RE0 | This document reviewed; the probe in §7 committed as regression tests | `specs/`, `internal/runner` | Tests assert the lane-scheduler timings for the full-plan path (red until RE1) |
| RE1 | Full-plan path on the lane scheduler; `--component-concurrency` default 0 (5.5) | `internal/runner/runner.go`, `cmd/orun/command_run.go` | RE0's tests green; existing runner tests green; dashboard grouping unchanged in the live region |
| RE2 | `level`, `upstreamLanes`, `estimatedSeconds`, `vcpu` on lanes and the lane matrix; `lane-matrix-levels` (5.1) | `internal/lanes`, `cmd/orun/github_matrix.go`, `plan.json` schema | A plan without lanes is byte-identical; the levelled workflow in `concepts/lanes.md` runs the 38-job example with no runner polling |
| RE3 | `concurrency: auto`; `--max-lanes` / `ORUN_MAX_LANES` (5.2) | `cmd/orun/run_lane.go`, `internal/lanes` | A lane on a 16-vCPU machine runs 8 in flight by default; the budget flag is recorded in `plan.metadata` |
| RE4 | `.git` never staged; `isolation: component`; lane-scoped setup steps; lane-level caches (5.3) | `internal/runner/workspace.go`, `internal/runner/runner.go`, `internal/model/composition.go` | On the 38-job example with the turbo compositions, a lane of n jobs performs one `pnpm install`; staged bytes per job are O(component) |
| RE5 | Declared `resources`; weighted, critical-path packing; `--weights history|<file>`; `--write-weights` (5.4) | `internal/model`, `internal/lanes/auto.go`, `cmd/orun/command_plan.go` | On the 50-job Cirrus-shaped fixture with synthetic weights, the longest lane is within 15 % of the mean; plans without weights are byte-identical |
| RE6 | Signal drain, resume at step, server lease TTL in the client (5.6); event waits and default timeouts (5.7) | `cmd/orun/command_run.go`, `cmd/orun/run_lane.go`, `internal/statebackend` | A `SIGTERM` mid-lane releases every held job within one lease; `--retry --lane` resumes at the first incomplete step; a downstream lane sees a reclaimed upstream within 60 s |

RE1 and RE2 are independent and each one afternoon; RE4 is the one that moves
the useful share in §2; RE5 depends on RE2's fields.

## 7. The probe

The measurements in F3 and F5 came from a throwaway test in
`internal/runner` (not committed; RE0 turns the timing cases into assertions):
a `sleepExecutor` whose `RunStep` sleeps for the duration in `step.Run`,
plans built from `model.PlanJob` literals, `NewRunner(..., concurrency, ...)`
with `Isolation = IsolationNone`, and `materializeTree` called on this
checkout. Numbers are from one run on the session's container (ext4, Go
1.25); they are stable to a few milliseconds across repeats.

| Case | `runConcurrent` | `runLane` |
|---|---|---|
| 4 independent jobs, 4 components, 300 ms each, concurrency 4, `--component-concurrency 1` (default) | 1,204 ms | 301 ms |
| same, `--component-concurrency 0` | 302 ms | — |
| 1,000 ms job + chain of 4 × 100 ms, concurrency 4 | 1,303 ms | 1,002 ms |
| stage this checkout (1,780 files, 17.4 MB, `.git` 5 MB) | 130–160 ms copy, ~95 ms cleanup | — |

## 8. Expected effect

[`model.py`](model.py) (run with `python3 -I specs/orun-runner-efficiency/model.py`)
switches the proposals on cumulatively over the plan of §2. Inputs: 70 s runner
setup on 4 vCPU (30 s network + 40 s CPU-bound, scaled by 4/vCPU), 35 s for a
job's own install and cold build cache in an isolated tree, 1.5 vCPU demand
per running job, 8 s average poll latency per cross-lane wait, no spot
interruptions, perfect `estimatedSeconds` for pre-warm. The 70/20 split is the
lane spec's measurement; the rest are assumptions to be replaced by numbers
from a real pool.

| | Scenario | Lanes | vCPU | Wall | vCPU·h | Peak | Useful | Runs/h | vs B: wall | vCPU·h | runs/h |
|---|---|---|---|---|---|---|---|---|---|---|---|
| A | today, `mode: job` | 88 | 4 | 7.9 m | 13.4 | 200 | 12 % | 7.6 | ×0.52 | ×2.06 | ×0.61 |
| B | **today, lanes** (16, concurrency 4) | 16 | 4 | 15.2 m | 6.5 | 64 | 25 % | 12.3 | — | — | — |
| C1 | B + RE2 gate `all` | 16 | 4 | 19.4 m | 5.3 | 32 | 30 % | 19.3 | ×1.28 | ×0.81 | ×1.56 |
| C2 | B + RE2 gate `first` | 16 | 4 | 15.2 m | 5.5 | 64 | 29 % | 12.3 | ×1.00 | ×0.84 | ×1.00 |
| D | C2 + RE4 shared setup | 16 | 4 | 12.6 m | 4.8 | 64 | 33 % | 14.9 | ×0.83 | ×0.74 | ×1.21 |
| E | D + RE5 weighted packing | 16 | 4 | 8.9 m | 5.7 | 64 | 28 % | 21.1 | ×0.59 | ×0.88 | ×1.71 |
| F | E + RE7 event waits | 16 | 4 | 8.4 m | 5.5 | 64 | 29 % | 22.2 | ×0.56 | ×0.84 | ×1.80 |
| G | F, 8-vCPU runners, 4 lanes/env, concurrency 5 | 8 | 8 | 7.9 m | 6.5 | 64 | 25 % | 23.7 | ×0.52 | ×1.00 | ×1.92 |
| H | F, 16-vCPU runners, 2 lanes/env, concurrency 10 | 4 | 16 | 7.7 m | 6.4 | 64 | 25 % | 24.2 | ×0.51 | ×0.97 | ×1.97 |
| I | F, 2-vCPU runners, 8 lanes/env, concurrency 2 | 16 | 2 | 9.9 m | 3.5 | 32 | 46 % | 37.8 | ×0.65 | ×0.53 | ×3.06 |
| **J** | **F, 4-vCPU runners, 4 lanes/env, concurrency 3** | 8 | 4 | 8.5 m | 3.6 | 32 | 45 % | 44.1 | **×0.56** | **×0.55** | **×3.58** |

Reading it:

- **On the runners a pool has today (4 vCPU), the whole program (F) is
  ≈ 45 % less wall-clock, ≈ 15–25 % fewer vCPU-hours, ≈ 1.8× the runs a 200-vCPU
  quota sustains.** Most of the wall-clock comes from RE5 (the heavy jobs stop
  sharing a lane); most of the vCPU-hours from RE4 (one install per lane).
- **Matching the lane to the runner (J) roughly doubles that again: ≈ 45 %
  less wall-clock, ≈ 45 % fewer vCPU-hours, ≈ 3.5× the runs per quota.** Same
  runner size as today, half the lanes, concurrency set so `3 × 1.5 ≈ 4 vCPU`.
- **Against `mode: job` (A) — what a managed-runner pool runs today per the
  lane spec §12 — J is ≈ 3.7× fewer vCPU-hours and ≈ 5.8× the runs per quota at
  about the same wall-clock.**
- Bigger runners (G, H) buy nothing on this shape: the lanes are chains, so a
  16-vCPU box idles through them. They pay for wide lanes only.
- The gate policy is a trade (C1 vs C2): `all` halves the peak and adds a
  quarter to the wall-clock; `first` keeps the wall-clock and frees less.
  RE7's event-driven waits are worth ~8 s per cross-lane edge (E → F); their
  value is bounded waits and server load, not throughput.
- Not in the model: **RE6**. Per spot interruption of a lane on the critical
  path, today costs the time for someone to notice and rerun plus a full lane
  rerun (10–20 min); with RE6 it is one lease (60 s) to detect, one setup, and
  the remaining steps (2–3 min). At a few interruptions per hundred
  runner-hours this is a tail effect, but it is the tail that misses a deploy
  window. **RE1** is outside the fleet too: it affects `orun run` without
  `--lane` (local runs, `--env` fan-out) and was measured at 4× on independent
  components and 30 % on a chain beside a long job (§7).

The sensitivities: the RE4 gain scales with install time × jobs per lane; the
RE5 gain with how unevenly the heavy jobs land under count packing; the RE2
gain with level depth × lane lifetime; and the runner-size result with how
wide a lane's parallel frontier is. A pool should measure its own 70/35/1.5
before choosing between F and J.

## 9. Risks and open questions

| # | Question | Proposed answer |
|---|---|---|
| Q1 | Does a pool gate (5.1) belong in the plan or only on the platform? | Both read the same fields; the plan emits them so a workflow without the platform (levelled matrices) gets the same effect. |
| Q2 | `isolation: component` symlinks the shared tree: is that safe for jobs that `rm -rf dist` at the root? | Those declare `isolation: workspace` in the composition; the default stays `auto`, which picks `component` only when `job.Path` is set and the composition does not opt out. |
| Q3 | Lane-scoped steps change what a job's check run shows. | The skipped step is recorded `completed · shared with <job>` with the shared step's log attached; the check run reads the same. |
| Q4 | Measured weights make `orun plan` depend on history: is that still deterministic? | The weights are an explicit, hashed input (`--weights`); the default stays the fixed cost model. A `lanes.lock` in the repository is the reviewed form. |
| Q5 | A `:release` verb is a contract change owned by the platform. | Until it exists, drain sends `failed` + `preempted`; `--retry --lane` already re-opens failed jobs. |
