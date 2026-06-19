# orun-native-coordination — Design

Status: Draft. The client architecture for append/fold coordination. The wire
contract is normative in the vendored
[`coordination-api.md`](./vendored/coordination-api.md); this doc explains how the
CLI implements its side and keeps local and cloud on one model.

## 1. The reshaped `Backend` interface

`internal/statebackend.Backend` stops being a set of row mutations and becomes an
event-log client. Indicative Go shape:

```go
type Backend interface {
    // Create or join a run; the run is derived from the plan object (no jobs sent).
    InitRun(ctx, plan *model.Plan, opts InitRunOptions) (*RunHandle, error)

    // Coordination = conditional appends. Each returns the new stream seq.
    Claim(ctx, runID, jobID, runnerID string) (*ClaimResult, error)   // claimed | deps_not_ready | job_held | terminal | cached
    Heartbeat(ctx, runID, jobID, runnerID string, leaseEpoch int) (*Lease, error) // 409 lease_lost
    Complete(ctx, runID, jobID, runnerID string, leaseEpoch int, outcome Outcome, resultDigest string) error

    // Read the log and fold it; ReadLog supports from-seq + live tail.
    ReadLog(ctx, runID string, fromSeq int) (events []Event, next int, open bool, err error)

    // Content-addressed result + log push (object plane).
    PutObject(ctx, kind string, digest string, body []byte) error

    Close(ctx) error
}
```

`ClaimResult` carries `{ Claimed, LeaseEpoch, LeaseExpiresAt, Seq, Cached,
Result }` — the server returns lease tunables (60s/20s) so the client never
hardcodes them, and `Cached=true` tells the runner to skip execution and adopt
the memoized `Result`.

The fold is the shared reduction (ported from the platform's
`packages/contracts` `reduce()` and kept byte-identical via the contract):

```go
func Fold(events []Event) RunState // { run, jobs{phase,holder,leaseEpoch,result}, frontier }
```

The runner, cockpit, `status`, and `logs` all read run state through `Fold`,
never through ad-hoc row reads.

## 2. The runner loop, in the new model

```
for job in localScheduler(plan):           # frontier from the local fold
    r := Claim(run, job, runnerID)
    switch {
    case r.Cached:        adopt r.Result; continue          # hermetic hit → skip
    case !r.Claimed && r.Reason == deps_not_ready:  waitOnLog(run); retry
    case !r.Claimed:      skip (held/terminal elsewhere)
    case r.Claimed:
        go heartbeat(run, job, r.LeaseEpoch)                # every 20s
        out, logs := execute(job)
        PutObject("log", logsDigest, logs)
        PutObject("job-result", resultDigest, result{inputHash, outputs, exit, logsDigest})
        Complete(run, job, runnerID, r.LeaseEpoch, out, resultDigest)
    }
```

`waitOnLog` tails `ReadLog`/SSE rather than polling a `/runnable` endpoint: deps
readiness arrives as `JobSucceeded`/`JobMemoized` events. There is no client-side
dependency check authority — the server's conditional `:claim` is the gate; the
local fold only *schedules* (picks what to try).

## 3. Result push & memoization (client side)

- Before executing a `hermetic` job, the client computes `jobInputHash` (resolved
  step defs + input object digests + declared env keys + composition lock — the
  contract D2 definition) and lets `:claim` report a `cached` hit; on a hit it
  adopts the existing `job-result` and emits `JobMemoized` server-side.
- On a miss it executes, uploads `log` then `job-result` to the object plane
  (digest-negotiated via `objects/missing`, reusing the object-model sync), then
  `:complete` references the result digest.
- Non-hermetic jobs never hash-skip; the client refuses to mark a job cacheable
  unless the plan declares it hermetic (safe default).

## 4. Local-first: one fold, online or off

- Offline, the CLI appends the same coordination events to a **local event log**
  (in `.orun/`) and folds it — `orun run`/`status`/`logs` work with no network,
  identical semantics.
- With cloud, sync ships local appends to the run stream and pulls remote events;
  the fold reconciles. Because appends are idempotent by `(jobID, kind,
  leaseEpoch)`, re-sync after a network blip never double-applies.
- `--local` forces the local log even when configured for cloud (the escape hatch
  when the backend is unreachable), unchanged from today.

## 5. Cockpit / status / logs over the stream

`bridge.Source` gains a stream-folding reader: for an active cloud run it tails
`…/log` (SSE/long-poll) and re-folds on each event; for a finished run it reads
the projection snapshot. `orun logs --follow` tails `LogChunk` events (or the
sealed `log` object once `:complete` lands). The cockpit viewmodels
(`RunView`/`LogsView`) are unchanged — they consume the fold, not the transport.

## 6. Degradation & failure

- Backend down on `Claim`/`Heartbeat`/`Complete`: retry with backoff (existing
  `remotestate` policy); on sustained failure surface a clear cockpit state and
  honor `--local`.
- `lease_lost` (409 on heartbeat/complete): the client stops work on that job
  immediately — another runner took over (at-least-once; idempotent steps
  absorb the overlap).
- `seq_conflict`/`object_missing`: re-read the log / re-push the missing object,
  then retry; never silently proceed.

## 7. What stays behind the interface

The runner, compiler, and cockpit never learn that the server shards runs on a
Durable Object or that Postgres is a delayed projection — all of that is behind
`statebackend.Backend`. The same client binary talks to the hosted (DO) server
and an OSS plain-Postgres server with no code path difference.
