# orun-sync-efficiency — how the CLI and Orun Cloud synchronise, what it costs, and where to improve

Status: analysis complete; nothing implemented. Companion to
[orun-runner-efficiency](../orun-runner-efficiency/README.md), which made the
runner side of a run efficient; this spec is about the *synchronisation* side:
the state store (the per-run event log, its folds, projections and logs) and
the API traffic between `orun` and `orun-cloud`.

Measured at orun `c7e68bb` (v2.78.0 plus #752) against orun-cloud `671e037`.
`model.py` reproduces the measurements and scales them.

## 0. Summary

A run talks to the platform in three ways: it **creates** the run (plan object
plus a run record), each job runs a **coordination cycle** (claim, heartbeat
every 20 s, complete, one log append per step), and every job that waits for
an upstream in another process **watches** the run's event log. The coordination
cycle is cheap and well designed: a verb is one conditional append to a
per-run Durable Object. Everything else is where the cost is:

| | per wake of one waiting job | what it carries |
|---|---|---|
| today | 1 long-poll + 1 frontier read + 1 **whole event log** + 1 **plan object** | O(events) + O(plan) bytes |
| after this spec | 1 long-poll, whose events are folded locally | O(new events) |

and a wake happens on **every heartbeat of every running job**, because lease
renewals are appended to the same log the waiters watch. On top of that, a
defect shipped in v2.78.0 (F1) makes a lane's waiting jobs re-read everything
**every 2 seconds** for as long as they wait.

Measured on a 4-job product against a request-logging stand-in for the
platform (§3): the same work costs 23 requests in one process, 137 as a CI
matrix and 100 as two lanes (plus 2.3 requests/s for `status --watch` and
0.7/s for `logs --follow` while they run). Scaled by the model to the shapes a
platform runs (§6): a 200-job, 5-level, 20-minute fleet run makes about
270,000 requests and downloads about 160 GB as a matrix, and about 780,000
requests and 640 GB as five lanes, almost all of it re-reading the event log.
Fixing F1 alone divides that by 1.5–6; the full set of proposals (§5) brings
both shapes to about 60,000 requests and a few MB, and cuts the platform's
Postgres statements for the run from ~106,000 to ~2,400.

The ranked findings are in §4, the proposals in §5, the milestones in §7.
The first milestone (SE0, the F1 fix) is a one-line change with a test.

## 1. Scope and method

- Read both code bases end to end on the sync paths: `internal/remotestate`,
  `internal/statebackend`, `internal/objremote`, `cmd/orun/command_run.go`,
  `run_lane.go`, `command_status.go`, `command_logs.go` in orun;
  `apps/state-worker` (router, run-coordinator DO, coordination-native/route,
  handlers/{runs,logs,objects,catalog}, projection-sweep, lease),
  `apps/api-edge` (state-facade, rate-limit, actor-cache, idempotency) and
  `packages/contracts/src/state` in orun-cloud.
- Ran the real CLI against a stand-in backend that implements the state API
  with the server's constants (60 s lease, 20 s heartbeat, 25 s long-poll held
  with a 250 ms tick) and logs every request (§8 says how to reproduce).
- Built `model.py` from the client loops and server handlers as coded,
  validated it against the three measured runs, and used it to scale to
  larger runs and to cost each proposal.

Out of scope: the runner itself (covered by orun-runner-efficiency), the
catalog and work-sync surfaces beyond the hygiene notes in F12, and the OSS
self-host backend (same contract, not measured).

## 2. How sync works today

File references are `orun:` or `cloud:` relative to each repository.

### 2.1 Identity and scope, per process

Every `orun run` process resolves its token source
(`orun:internal/remotestate/auth.go:383-450`): in GitHub Actions one OIDC GET
plus one `POST /v1/auth/oidc/exchange`, cached in memory until 60 s before
expiry (`auth.go:30,92-121`); a static `ORUN_TOKEN`; or the local session,
whose file is read and parsed on **every request** (`auth.go:313-323`). The
scope (org, project) comes from flags, env, the intent or the repo link
(`orun:cmd/orun/remote_config.go:85-153`); a project slug costs one resolve
call (`command_run.go:603-610`).

Two HTTP clients serve the run: the remotestate client with its own transport
(16 idle connections per host, 30 s reads, 60 s uploads,
`orun:internal/remotestate/client.go:22-31,242-268`) and the coordination
client, built **without** a transport (`command_run.go:622`), so it uses
`http.DefaultClient`: no client timeout and two idle connections per host
(`orun:internal/statebackend/coordclient.go:190-192`).

### 2.2 Run creation

`InitRun` (`orun:internal/statebackend/remote.go:61-120`) converts the plan to
the backend shape, uploads it to the object plane (`EnsureObject`: one
`POST objects/missing` and, when missing, one `PUT objects/{digest}`), then
`POST runs` with the job list. The converted plan stamps `CreatedAt:
time.Now()` into the digested bytes (`orun:internal/remotestate/convert.go:47`),
so its digest changes every second: processes that start in different seconds
each upload the full plan again (F4).

On the server, `POST runs` (`cloud:apps/state-worker/src/handlers/runs.ts:193-582`)
costs about ten SQL statements, one to twenty-one projects-worker calls (the
run's environment and each job's), the DO `/init`, and event and audit
inserts; a replay by the same ULID still costs a run select, a backing check
and a counts select.

### 2.3 The coordination cycle

A run is a per-run `RunCoordinator` Durable Object (`cloud:apps/state-worker/src/run-coordinator.ts`)
holding an append-only event log under per-seq keys (`:84,97-99`), an
in-memory fold advanced incrementally (`:119-131,198-232`), a snapshot every
64 events (`:83,405-412`) and an alarm every 60 s while any job is claimed or
queued (`:414-421,601-625`). Postgres `state.runs/run_jobs` is only a
projection of that log (`cloud:packages/contracts/src/state/coordination.ts:8-9`).

Per job the CLI sends:

- `POST …/jobs/{job}:claim` once (`orun:cmd/orun/command_run.go:744-777` for a
  whole plan, `performRemoteJobClaim` `:900-1030` for `--job`). The server
  appends `job.claimed` (`cloud:packages/contracts/src/state/coordination-core.ts:128-202`),
  checks the alarm, announces newly runnable jobs, and **projects the whole
  run into Postgres** (`cloud:apps/state-worker/src/coordination-native.ts:248`,
  `coordination-route.ts:337-377`): DO `GET /state`, one run update and **one
  `UPDATE state.run_jobs` per job in the plan** (`cloud:packages/db/src/state/projector.ts:49-98`),
  deferred with `waitUntil`.
- `POST …:heartbeat` every `heartbeatIntervalSeconds` the server grants (20;
  `coordination-core.ts:28-32`; the client falls back to 20 s,
  `command_run.go:1163`). Each one **appends a `job.lease_renewed` event**
  (`coordination-core.ts:212-230`; DO `:547-554`): two storage puts, no
  projection (`coordination-native.ts:272-278`). A 10-minute job is about 33
  events, 30 of them heartbeats (F3).
- `POST …/logs/{job}` once per step with non-empty output
  (`orun:internal/runner/runner.go:935-946`, hook at `command_run.go:803-816`),
  through a pipeline that uploads synchronously under one mutex
  (`orun:internal/remotestate/logpipe/pipeline.go:121-138,207-227`). The
  server (`cloud:apps/state-worker/src/handlers/logs.ts:125-285`) selects the
  run, the job, **every existing chunk of the job to compute the next seq**
  (`:221-223`), inserts the index row, puts the bytes in R2 and inserts a
  metering row: about five statements and one R2 write per step.
- `POST …:complete` once (`command_run.go:832-843`, `updateJobWithRetry`
  `:1133-1158`). On success the server first **seals the log**: lists the
  job's R2 chunks, gets each, concatenates, hashes and puts the result
  (`coordination-native.ts:147-187,310-318`), then appends `job.succeeded`
  and projects the run again.

### 2.4 Waiting for upstreams in other processes

Two loops, both built on `eventWait` (`orun:cmd/orun/run_lane.go:147-193`),
which long-polls `GET …/log?from=<seq>&wait=<s>` for up to 15 s.

- **`--job` (the matrix shape)**: a rejected claim (`deps_not_ready`) enters
  `waitForJobRunnable` (`command_run.go:1037-1100`). Each iteration: the
  long-poll (capped at the current poll interval, 2 s doubling to 15 s), then
  `GET …/frontier`, then `LoadRunState`, then the interval doubles.
- **`--lane`**: `awaitRemoteUpstreams` (`run_lane.go:78-146`) for every job
  whose upstream is in another lane. Each iteration: `LoadRunState`, then the
  long-poll (capped at the current delay); on a reported event `continue`
  with the delay unchanged, otherwise sleep the delay and double it.

`LoadRunState` on the coordination backend (`orun:internal/statebackend/coordbackend.go:236-255`)
reads the **whole event log from seq 0**, then **downloads the plan object**
(`GetObject`, no cache, no retry) to learn the job graph, folds, and converts.
The long-poll it just made already returned exactly the new events, and they
are discarded (`WaitForRunEvents` keeps only the head seq, `coordbackend.go:210-220`).

`eventWait.wait` returns `true` whenever the long-poll succeeded, **including
on a timeout with no events** (`run_lane.go:174-192`): both loops treat every
timeout as an event (F1).

On the server the long-poll is held inside the DO: a `storage.list` of the
events after `from` every 250 ms for up to 25 s (`run-coordinator.ts:85-88,443-463`);
any append, a heartbeat included, ends it. `GET …/frontier` serialises the
whole fold (`cloud:apps/state-worker/src/coordination-route.ts:120-124`).

### 2.5 Reading a run: `status`, `logs`, `tui`

`orun status --remote-state` builds the plain remote backend, not the
coordination one (`orun:cmd/orun/commands_root.go:199-218`), so `--watch`
never finds an event waiter and polls `GET runs/{id}` + `GET runs/{id}/jobs`
every second (`command_status.go:59,199-248`). `logs --follow` polls
`GET …/logs/{job}?fromSeq=` every 1.5 s (`command_logs.go:197-226`); each read
costs the server a run select, a chunk select, **one R2 get per chunk in
sequence**, a job select and the declared steps (`handlers/logs.ts:337-446`).

### 2.6 The object plane and the catalog

`objects/missing` is one select with one parameter per digest
(`cloud:apps/state-worker/src/handlers/objects.ts:97-141`); `PUT objects/{d}`
hashes the body, upserts the index row, puts to R2 and meters (`:145-260`);
the client batches missing-checks by 1000 and uploads 8 objects at a time,
one PUT each (`orun:internal/remotestate/objtransport.go:37-61`,
`objremote.go:25,85-103`). A run never uses this beyond the plan blob and a
hermetic job's result object. The catalog push does, and then always sends
`PUT catalog/head` even when the digest is unchanged
(`orun:cmd/orun/catalog_push.go:173-187`); the plan-time autopush is
debounced by a local marker, so an unchanged catalog costs nothing.

### 2.7 The edge, per request

Every state request passes `handleStateRoute`
(`cloud:apps/api-edge/src/state-facade.ts:144-264`): a rate-limit family by
path (`:55-66`), idempotency replay only with an `Idempotency-Key`, actor
resolution cached 30 s per token (`actor-cache.ts:31`), then a service-binding
fetch to state-worker, which re-authorises: a workflow (CI) actor costs no I/O,
a user or key actor costs a membership-worker and a policy-worker call on
**every** request, uncached (`cloud:apps/state-worker/src/authz.ts:35-109`).

Rate families (`cloud:apps/api-edge/src/rate-limit.ts:115-236`): `coordination`
(claim, heartbeat, complete, cancel, `/log`, `/frontier`) at 1200/min per
token **per run** and 6000/min per org; `objects` at 1200/min; everything
else under `/state`, **log appends and reads included**, in `state` at
60/min per token and 300/min per org. Writes consume two RateLimiterDO
operations (org and identity buckets, `:373-392`); reads use an in-isolate
bucket.

### 2.8 Background work on the server

- The DO alarm every 60 s per active run: lease and abandonment sweeps over all
  jobs (`run-coordinator.ts:601-625`).
- A state-worker cron every minute (`cloud:apps/state-worker/src/index.ts:28-105`)
  whose projection sweep (`projection-sweep.ts:15-63`) re-projects up to 200
  pending or running runs: DO `GET /state` + a select each, and 1 + N updates
  for every run whose seq advanced, which with live heartbeats is every
  running run, every minute.

## 3. Measurements

The 4-job saas-baseline product (api and web, staging then production; web
depends on api in each environment, so the levels are 1/2/1 jobs), each job
given a 45 s step so that two heartbeats happen per job. Requests as logged by
the stand-in backend.

| scenario | processes | wall | requests | bytes down | of which waiting |
|---|---|---|---|---|---|
| A: whole plan, `--remote-state` | 1 | 2m16s | **23** | 2 KB | 0 |
| B: `--job` per job, shared exec id (the matrix recipe) | 4 | 2m16s | **137** | 101 KB | 108: 54 log reads, 27 frontier, 24 plan GETs, 3 rejected claims |
| C: two lanes (`--lane`), shared exec id | 2 | 2m16s | **100** | 92 KB | 75: 50 log reads, 25 plan GETs |
| alongside C: `status --watch` for 35 s | | | 80 | 65 KB | 2.3 requests/s |
| alongside C: `logs --follow` for 35 s | | | 27 | 2 KB | 0.8 requests/s |

A is the floor: per job one claim, two heartbeats, one log append, one
complete; per run one missing-check, one plan upload, one create.

B's extra 114 requests are the three waiting jobs: each wake (a long-poll
return, on a heartbeat of any running job or on the poll timer) costs a
frontier read, a whole-log read and a plan download, and the first claim of
every waiting job is rejected. The timeline (`scratchpad` log, §8) shows the
three waiters re-reading at 2, 4, 6, 12, 14 s (the timer) and at 20 and 40 s
(the running job's heartbeats), then all three at once at 45 s.

C's one cross-lane waiter (production's `api` job, waiting for staging's) made
a long-poll, a whole-log read and a plan download **every 2 seconds** for its
45 s wait: the delay never grew because every timeout was reported as an
event (F1).

`model.py` gives 23, 146 and 103 for A, B and C.

Per-request server cost, from the handlers (§2 has the references):

| request | edge | state-worker | DO | Postgres | R2 |
|---|---|---|---|---|---|
| `:claim` | 2 limiter ops | authz | 2–3 puts, fold, alarm get | 2 + N statements (async) | 0 (2 if hermetic) |
| `:heartbeat` | 2 limiter ops | authz | 2 puts, fold | 0 | 0 |
| `:complete` | 2 limiter ops | authz | 2–3 puts, fold | 2 + N statements (async) | list + k gets + 1 put (seal) |
| `/log?from&wait` | in-isolate | authz | 1 list per 250 ms held, ≤100 | 0 | 0 |
| `/frontier` | in-isolate | authz | full fold serialised | 0 | 0 |
| `/log?from=0` | in-isolate | authz | list of all events | 0 | 0 |
| `objects/{d}` GET | in-isolate | authz | 0 | 1 select | 1 get |
| `/logs/{job}` POST | 2 limiter ops | authz | 0 | 5 statements + k rows read | 1 put |
| `/logs/{job}` GET | in-isolate | authz | 0 | 3 selects | k gets |
| `runs/{id}`, `runs/{id}/jobs` | in-isolate | authz | 0 | 1–2 selects | 0 |

"authz" is nothing for a CI token and two service calls for a user token.

## 4. Findings, ranked by effect

**F1 — a long-poll timeout is reported as an event** (`orun:cmd/orun/run_lane.go:174-192`).
`eventWait.wait` returns true on any successful long-poll, so
`awaitRemoteUpstreams` never backs off (a full state re-read every 2 s per
waiting lane job) and `waitForJobRunnable` re-reads on every timeout. Shipped
in v2.78.0 (#750). In the model this one line is the difference between
780,000 and 134,000 requests for the 200-job lane run.

**F2 — every wake re-reads the whole event log and the plan object**
(`orun:internal/statebackend/coordbackend.go:236-255`; callers
`run_lane.go:107`, `command_run.go:1072`, `command_status.go:223`). The
long-poll already returned the delta; the client throws it away and reads
O(E) events plus the plan, which is content-addressed and never changes, on
every wake. Bytes per wake grow with the run: the 200-job run's log is 4 MB
by its last level.

**F3 — heartbeats are log events** (`cloud:packages/contracts/src/state/coordination-core.ts:212-230`).
They are 90 % of a long job's events, they make every long-poller in the run
wake every 20/R seconds (R running jobs), they advance the seq the minute
sweep keys on (so every running run is re-projected every minute, F7), and
they are the bulk of what F2 re-reads.

**F4 — the plan upload is never deduplicated across processes**
(`orun:internal/remotestate/convert.go:47`). `CreatedAt` is inside the
digested bytes, so a matrix of N jobs uploads the plan N times and the object
index gains N rows for one plan.

**F5 — `status --watch` and `logs --follow` poll** (`orun:cmd/orun/commands_root.go:199-218`,
`command_status.go:199-248`, `command_logs.go:197-226`): 2.3 and 0.8
requests/s per watcher, the former on the `state` family (60/min per token:
a watcher alone spends its budget in 26 s), each `logs` poll replaying the
chunk reads on the server.

**F6 — `/frontier` serialises the whole fold per call**
(`cloud:apps/state-worker/src/coordination-route.ts:120-124`) and the matrix
waiter calls it on every wake; the DO has the frontier in memory.

**F7 — projections write every job, and every minute**
(`cloud:packages/db/src/state/projector.ts:76-78`, `coordination-route.ts:207-250`,
`projection-sweep.ts:15-63`): 2 + N statements after each claim and complete
(2N verbs per run → ~2N² statements), plus 1 + N per minute per running run
whether or not any job changed. The 200-job run: ~106,000 statements, of
which ~2,400 carry information.

**F8 — log appends are the tightest path of the run.** Server: an O(k) chunk
listing per append (`cloud:apps/state-worker/src/handlers/logs.ts:221-223`),
five statements and an R2 put per step; the route rides the `state` family
(60/min per token, `cloud:apps/api-edge/src/state-facade.ts:55-66`), shared
with run create and the status reads. Client: the pipeline uploads under one
mutex, so a step's completion waits for its upload and parallel jobs
serialise on it (`orun:internal/remotestate/logpipe/pipeline.go:121-138`);
a 429 is not retried on the spot (`client.go:639-647`, single attempt, then
the pipeline's 2 s gate); an undrained chunk at exit fails an otherwise green
run (`orun:cmd/orun/command_run.go:443-456`). A lane running 20 jobs of 4
steps that finish within a minute exceeds the family's budget on its own.

**F9 — log reads fetch chunks one at a time** (`handlers/logs.ts:390-409`) and
the seal on `:complete` repeats the same list-and-get (`coordination-native.ts:147-187`),
on the response path of the verb.

**F10 — fixed per-request overheads that add up at fleet scale.** Two
RateLimiterDO operations per write at the edge (`rate-limit.ts:373-392`); for
user tokens a membership and a policy call per request in state-worker, never
cached (`authz.ts:35-109`); the session token file parsed per request
(`orun:internal/remotestate/auth.go:313-323`); the coordination client on
`http.DefaultClient` (`coordclient.go:190-192`), so a lane's concurrent
heartbeats and long-polls churn connections past two idle ones and have no
client timeout.

**F11 — the DO long-poll ticks by listing** (`run-coordinator.ts:443-463`): a
`storage.list` of the events after `from` every 250 ms per held request,
and `/log?from=0` has no page cap. A hundred waiters on one run is 400 lists
per second on one single-threaded object.

**F12 — hygiene outside the run path.** The catalog head is advanced even
when unchanged (`orun:cmd/orun/catalog_push.go:186-187`); work sync does one
`GetTask` per declared task and pushes every doc's full content on every
sync (`orun:cmd/orun/work_sync.go:395,456`); the OCI composition source is
re-resolved on every `orun plan` even when the lockfile pins the digest
(`orun:internal/composition/registry.go:662-680`); `orun run` keeps three
HTTP connection pools to one host.

## 5. Proposals

Each names the repository it changes and the findings it closes.

**P0 (orun) — a timeout is a timeout.** `eventWait.wait` returns true only
when the head advanced; `awaitRemoteUpstreams` then backs off as designed and
`waitForJobRunnable` re-reads only on events. One line and a test. Closes F1.

**P1 (orun, optionally cloud) — incremental client state.** Keep the folded
`RunFoldState` in the waiting process; the long-poll's events are folded onto
it (the fold is already the shared, golden-vectored
`statebackend.Fold`; add a `FoldFrom(state, events)` as the TypeScript
`reduceFrom` has); derive the frontier locally with `ClaimableJobs`; cache
the plan blob per process by digest. A wake then costs the long-poll alone.
For the initial load, expose the DO's fold as `GET …/runs/{id}/state`
(state-worker already reads it internally,
`coordination-route.ts:207-211`): O(N) bytes instead of O(E) + the plan.
Closes F2 and F6, and gives `status --watch` its waiter (F5). The wire
contract gains one read; nothing changes for the OSS backend except an
optional route.

**P2 (cloud) — lease renewals leave the event log.** A heartbeat updates the
job's `leaseExpiresAt` in the DO's fold and storage without appending; the
sweep still appends `lease_expired`. Alternatively, keep appending but let
`?wait=` ignore `job.lease_renewed` and `from=` skip it: the log stays
complete for audit while waiters stop waking on it. The fold vectors change
only in that renewals no longer appear; the Go fold already tolerates their
absence. Closes F3, and most of F7's sweep cost (a run whose seq did not move
is not re-projected).

**P3 (orun) — one plan upload per run.** Take `CreatedAt` out of the digested
plan bytes (carry it on the run record). N processes negotiate the same
digest and only the first uploads. Closes F4.

**P4 (cloud) — project by difference.** Project only the jobs whose events
arrived since the run's `last_seq`, in one `UPDATE … FROM
jsonb_to_recordset` statement; have the minute sweep skip runs whose new
events are only lease renewals (automatic with P2). Closes F7.

**P5 (both) — logs.** Server: the next seq from `MAX(seq)` or a counter on
`run_jobs` instead of listing every chunk; bounded parallel R2 gets on read
and on seal; move `/logs/{job}` to the per-run `coordination` family (it is
lease-gated like the verbs) or to a per-run bucket of its own. Client: an
asynchronous pipeline with per-job queues so a step never waits for its
upload and jobs never serialise on one mutex; honour `Retry-After` on 429 at
the append; coalesce a job's small consecutive chunks (one append carrying
`steps[]` is a small additive contract change). Closes F8 and F9.

**P6 (both) — watching without polling.** `status --watch` on the
coordination backend, so it long-polls (with P1 one request per 15 s plus one
state read per real change). `logs --follow` with `?wait=` on the log read
(the server polls its index at the same 250 ms tick) or, with P1, tails the
job's step events from the run log and reads chunks only when a step ends.
Closes F5.

**P7 (both) — fixed costs.** Coordination client on the shared keep-alive
transport with a timeout; an in-memory token cache keyed on the session
file's mtime; `/frontier` answered from the DO's `fold.frontier` without
serialising the state; the long-poll tick checks `storage.get("seq")` and
lists only when it moved; in state-worker, cache the authorization context
per (actor, org, action) for the actor cache's 30 s. Closes F10 and F11.

**P8 (orun) — hygiene.** Skip the catalog head advance when the digest is
unchanged; batch task lookups; push docs by hash; resolve a pinned OCI source
from the lockfile. Closes F12.

## 6. Expected effect

From `model.py` (the client loops and server handlers as coded; one run; CI
tokens; a level's heartbeats coincide, which makes the "today" wake counts a
**lower** bound).

38 jobs in 4 levels, 10-minute jobs (the `examples/` plan):

| | requests | bytes down | Postgres statements |
|---|---|---|---|
| matrix today | 16,794 | 1.0 GB | 5,440 |
| matrix, F1 fixed | 10,194 | 587 MB | 5,440 |
| matrix, P0–P2 | 4,309 | 0 | 3,880 |
| matrix, all | 4,158 | 0 | 498 |
| lanes today | 52,429 | 4.5 GB | 5,440 |
| lanes, F1 fixed | 8,131 | 614 MB | 5,440 |
| lanes, P0–P2 | 4,195 | 125 KB | 3,880 |
| lanes, all | 4,078 | 125 KB | 498 |

200 jobs in 5 levels, 20-minute jobs (a 200-vCPU fleet):

| | requests | bytes down | Postgres statements |
|---|---|---|---|
| matrix today | 269,120 | 160 GB | 106,100 |
| matrix, F1 fixed | 173,120 | 99 GB | 106,100 |
| matrix, P0–P2 | 62,320 | 0 | 86,000 |
| matrix, all | 61,321 | 0 | 2,400 |
| lanes today | 779,345 | 641 GB | 106,100 |
| lanes, F1 fixed | 133,985 | 101 GB | 106,100 |
| lanes, P0–P2 | 61,505 | 3.7 MB | 86,000 |
| lanes, all | 60,701 | 3.7 MB | 2,400 |

What remains after "all" is the coordination cycle itself: 12,000 heartbeats
(200 jobs × 20 min ÷ 20 s), 1,000 log appends, 400 claims and completes, and
one long-poll per 15 s per waiting job. Halving the heartbeat rate (a 30 s
interval under a 90 s lease, both server-chosen) would take another 6,000
off; it is not proposed here because P2 makes a heartbeat two storage puts
and nothing else.

The rate families fit afterwards: a 200-job run's verbs are about 10/s at
peak against 20/s per run; today a lane's waiters alone exceed it, and the
client's fallback on a 429 (`run_lane.go:186-189`) is a 2 s poll of the same
whole-log and plan reads.

## 7. Milestones

| | repo | change | closes |
|---|---|---|---|
| **SE0** | orun | P0: the F1 fix with a regression test; `status --watch` on the coordination backend | F1, half of F5 |
| **SE1** | orun | P1 client side: incremental fold, local frontier, plan cache; P3 stable plan digest; P7 client fixed costs | F2, F4, F6 (client), F10 (client) |
| **SE2** | cloud | P1 server side: `GET …/runs/{id}/state`; P7 server fixed costs (`/frontier`, long-poll tick, authz cache) | F6, F10, F11 |
| **SE3** | cloud | P2 lease renewals out of the log (or filtered from waits); P4 projection by difference | F3, F7 |
| **SE4** | cloud | P5 server: seq counter, parallel chunk reads, log route family | F8, F9 |
| **SE5** | orun | P5 client: async log pipeline, 429 retry, chunk coalescing; P6 `logs --follow` | F8, F5 |
| **SE6** | orun | P8 hygiene | F12 |

SE0 is urgent (a regression in the current release). SE1 and SE2 are
independent and together remove the O(E) reads; SE3 removes the wake storm
and most of the Postgres load. Each milestone is verifiable with the stand-in
backend of §8: the request table of §3 is the acceptance test.

Risks and non-goals:

- The coordination contract (`cloud:work/epics/saas-orun-backend-merge/coordination-api.md`,
  vendored into `orun:specs/orun-native-coordination/vendored/`) lists
  `job.lease_renewed` in §8.1 and the fold vectors pin it. P2's filtered-wait
  variant keeps the log unchanged; the no-append variant is a contract
  revision and needs the vendored checksum updated in the same release.
- The OSS backend implements the same routes; `GET …/state` must be optional
  (the client falls back to the log read when it is absent).
- Nothing here changes what a run records: logs, outcomes and attribution
  are unchanged; only how often they are read and written.

## 8. Reproducing the measurements

The stand-in backend and the driver live in this session's scratchpad
(`fakestate/main.go`, `measure.sh`, `summarize.py`); they are small enough to
move under `examples/remote-state-matrix/test/` as part of SE0 so the request
table becomes a test. The stand-in implements the REST run store, the object
plane, chunked logs and the native verbs with the server's constants, and
writes one JSON line per request. A scenario is:

```
ORUN_TOKEN=x orun run <plan> --job <id> --remote-state --backend-url http://127.0.0.1:<port> \
  --org org_test --project prj_test --exec-id <shared>
```

one process per job for the matrix shape, `--lane <lane>` per lane for the
lane shape, and `orun status --remote-state --watch --exec-id <shared>` /
`orun logs --remote-state --follow --job <id>` alongside. `python3 model.py`
prints §6.
