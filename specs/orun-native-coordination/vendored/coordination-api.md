# Coordination API — Contract (v2, greenfield)

Status: Draft → freezes at BM2. **Normative.** The seam between the two repos:
`apps/state-worker` + api-edge implement the server (DO-sharded hosted / plain
-Postgres OSS); `orun/internal/remotestate` implements the client. This contract
**replaces** the legacy `orun-backend` `/v1/runs` coordination surface and the
relational §2 of `saas-orun-platform/state-api-contract.md`; the object plane
(§3 there) and auth (§1) are reused unchanged. Owned here; vendored into
`orun/specs/orun-native-coordination/vendored/` with a checksum guard.

Shaped around one idea: **a run is an append-only event stream rooted at a
content-addressed plan; coordination is conditional appends; reads are folds.**

## 0. Conventions

- Base path: `/v1/organizations/{orgId}/projects/{projectId}/state`
  (path-scoped tenancy; OSS serves the same paths at `_local/_local`).
- Auth: `Authorization: Bearer <token>` — CLI session, OIDC-exchanged workflow
  token, or `sk_` key → one `ActorContext`. Every appended event is attributed to
  it.
- Versioning: `Orun-Contract-Version: 2`. Unknown major ⇒ `409
  contract_version_unsupported` + supported range.
- IDs: `runId` ULID (client-mintable, sortable, the stream id); `jobId` from the
  plan DAG; `runnerId` opaque; `leaseEpoch` monotonic per (job) claim; digests
  `sha256:<hex>`; `seq` monotonic per run stream.
- Idempotency: appends are idempotent by `(runId, eventKey)` where `eventKey`
  is `(jobId, kind, leaseEpoch)` for coordination events and a client ULID for
  `RunCreated` — replays return the existing `seq`, never double-append.
- Errors (api-edge envelope `{ error:{ code, message, details?, requestId } }`):
  `deps_not_ready`, `job_held`, `run_terminal`, `lease_lost`, `object_missing`,
  `seq_conflict`, `contract_version_unsupported`.

## 1. Object plane (reused; new kinds)

Unchanged from `state-api-contract.md` §3, with added kinds:

```jsonc
POST …/state/objects/missing   { "digests":[ "sha256:…" ] } → { "missing":[…] }
PUT  …/state/objects/{digest}  // kinds: plan | job-result | log | run-record | catalog-snapshot | …
GET  …/state/objects/{digest}
```

- `job-result` body: `{ "jobInputHash":"sha256:…", "outputs":["sha256:…"],
  "exit":0, "logsDigest":"sha256:…" }` — content-addressed by `jobInputHash`.
- `log`: assembled job log bytes (sealed); referenced by a `job-result`.

## 2. Run = an event stream

```jsonc
POST …/state/runs
{ "runId":"01J…",                 // client ULID; replay returns the existing run
  "planDigest":"sha256:…",        // plan object must exist (else 412 object_missing)
  "environment":"production",
  "source":{ "commit":"…","ref":"…" } }
→ 201 { "run": { "runId","sourceHash","planDigest","status":"pending","head":{"seq":0} } }
```

The server derives the job set from the `plan` object — **no jobs array is sent.**
`RunCreated{planDigest,sourceHash}` is the first event; quota is enforced here
(strongly consistent), not per job.

```jsonc
GET …/state/runs/{runId}                 → projected snapshot { run, jobCounts }   // delayed, cacheable
GET …/state/runs/{runId}/log?from={seq}  → { "events":[ {seq,kind,jobId?,actor,at,...} ], "next":N, "open":true }
GET …/state/runs/{runId}/frontier        → { "jobs":[ jobId… ] }                   // projection of the fold
GET …/state/runs?environment=&status=&cursor=   → paginated list (projection)
```

`GET …/log` supports `Accept: text/event-stream` (SSE) and long-poll
(`?wait=30s`) for live tail. **It replaces** status-polling, runnable-polling, and
per-job status: the client folds one stream.

## 3. Coordination = conditional appends

Typed verbs are the public surface (the server owns invariant enforcement);
each is a single conditional append onto the run stream.

```jsonc
POST …/state/runs/{runId}/jobs/{jobId}:claim   { "runnerId":"host-abc" }
→ 200 { "claimed":true,  "leaseEpoch":1, "leaseExpiresAt":"…", "seq":12,
        "leaseSeconds":60, "heartbeatIntervalSeconds":20 }
→ 200 { "claimed":false, "reason":"deps_not_ready" | "job_held" | "run_terminal" }
→ 200 { "claimed":false, "cached":true, "result":{ "digest":"sha256:…" } }   // memoized → skip exec

POST …/state/runs/{runId}/jobs/{jobId}:heartbeat { "runnerId":"…","leaseEpoch":1 }
→ 200 { "leaseExpiresAt":"…" }            // 409 lease_lost if not the holder

POST …/state/runs/{runId}/jobs/{jobId}:complete  { "runnerId":"…","leaseEpoch":1,
                                                   "outcome":"succeeded"|"failed",
                                                   "resultDigest":"sha256:…",   // succeeded: a job-result object
                                                   "errorText":"…" }            // failed
→ 200 { "seq":40 }                         // idempotent by (jobId, leaseEpoch); 409 lease_lost; terminal sticky

POST …/state/runs/{runId}:cancel           → 200 { "seq":… }
```

Semantics (the parity invariants from `orun-backend`, preserved):
- `:claim` wins iff the job's latest event ∈ `{JobReady, LeaseExpired}`, every
  dep's latest event ∈ `{JobSucceeded, JobMemoized}`, and no `job-result` already
  memoizes it. Exactly one of N racers wins (single writer per run).
- A memoization hit (existing `job-result` for the job's `jobInputHash`, when the
  plan marks the job `hermetic`) returns `cached` and appends `JobMemoized`.
- Lease: 60s, heartbeat 20s (server-returned, never client-hardcoded); lapse →
  the run DO appends `LeaseExpired` then `JobReady` (bounded re-queue, then
  `JobFailed{reason:timed_out}`).

## 4. Logs

```jsonc
POST …/state/runs/{runId}/jobs/{jobId}/logs  { "leaseEpoch":1, "content":"<≤1 MiB chunk>" } → { "seq":7 }
```

Chunks append as `LogChunk` events; on `:complete` the assembled log is sealed
into a `log` object and referenced by the `job-result`. Live tail = read the run
log stream (§2) or `GET …/state/objects/{logDigest}` once sealed.

## 5. Advanced: the append primitive (optional)

The typed verbs above are sugar over one mechanism, exposed for tooling only:

```jsonc
POST …/state/runs/{runId}/events  { "expectedSeq":12, "event":{ "kind":"…","jobId":"…", … } }
→ 200 { "seq":13 }   // 409 seq_conflict on stale expectedSeq; server still validates the transition
```

Clients SHOULD use the typed verbs; the server enforces the same invariants on
this path and rejects event kinds a client may not author.

## 6. Policy map (deny-by-default; extends `state-api-contract.md` §6)

| Route group | Action |
|---|---|
| run/log read, `…/log`, `…/frontier` | `state.run.read` |
| run create, `:claim`/`:heartbeat`/`:complete`, log append, `:cancel`, `…/events` | `state.run.write` |
| object read / write (incl. `job-result`, `log`) | `state.object.read` / `state.object.write` |

## 7. Consistency contract

- The **only** strongly-consistent operations are run-create/quota (§2) and the
  conditional append of `:claim` (§3). Everything in §2's read endpoints is a
  projection and MAY lag by seconds.
- Execution is **at-least-once**: lease takeover can re-run a slow-but-alive job;
  steps MUST be idempotent. Memoization is opt-in and never required for
  correctness.
- The contract is implementation-agnostic: a DO-sharded server and a
  plain-Postgres conditional-append server are indistinguishable on the wire.
