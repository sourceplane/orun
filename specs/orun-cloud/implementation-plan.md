# orun-cloud — Implementation Plan (OC0–OC6)

Status: Draft. Each milestone pairs with a platform milestone
(`saas-orun-platform`, OP0–OP9); "done when" gates verify against the stage
deployment with a real binary from this branch. OC0 is human-independent and
safe to land first.

## OC0 — Contract alignment — ✅ Done

Pairs with OP0 (contracts exist server-side, even dormant).

- Vendor the wire contract: `specs/orun-cloud/vendored/state-api-contract.md`
  copied verbatim from the platform repo + a CI check that diffs it against
  the source (drift fails the build with "re-vendor or renegotiate").
- `internal/remotestate`: `Scope{OrgID, ProjectID}` on the client; scoped path
  construction; `Orun-Contract-Version: 1` header;
  `contract_version_unsupported` rendered actionably; platform error envelope
  parsed everywhere (`code`, `requestId` surfaced).
- Retire "namespace": `SessionResponse.allowedNamespaceIds` →
  `orgs []OrgRef{ID, Slug, Name, Role}` (storage migration reads the old
  `credentials.json`/keychain entry once, rewrites); `RepoLink` (today carrying
  `NamespaceID`/`NamespaceKind`) gains org/project IDs + slugs. This retirement
  also spans the embedded OSS server (`orun backend`'s Worker bundle + its D1
  `namespaces` schema), not just the client.
- Config: `cloud.url` block in user config + `execution.state` intent wiring;
  the existing `backend.url` key honored as deprecated alias with a one-line
  warning; precedence per design §8, with tests.

**Done when:** unit tests cover path construction, version-header rejection
rendering, error-envelope parsing, config precedence, and session-file
migration; the vendored-contract CI check is live; existing local-mode tests
still pass untouched (no behavior change without `--remote-state`).

## OC1 — Auth completion — ✅ Done (refresh hardening shipped)

Pairs with OP1.

- `internal/cliauth`: BrowserLogin against `POST /v1/auth/cli/start` +
  loopback grant redeem; DeviceLogin against the platform device endpoints
  (GitHub's device flow removed); rotating-refresh handling in
  `SessionTokenSource` (single-use refresh, `family_revoked` → clear session +
  one actionable error).
- Session storage: 0600 writes are already enforced (and macOS already uses the
  `io.sourceplane.orun` keychain) — add refusal of world-readable (0644) reads
  with a fix-it message; `auth status` shows user, orgs + roles, expiry, backend URL.
- `auth token` prints a fresh access token (refreshing if needed).

**Done when:** against stage, `auth login`, `auth login --device`,
`auth status`, `auth token`, `auth logout` all work end-to-end; access-token
expiry mid-`run` refreshes transparently; console-side revoke makes the next
CLI call fail with the re-login message; recorded in the paired OP1 gate.

**Hardening shipped (PR #366):** real usage exposed the rotating-refresh-token
race — concurrent invocations (two terminals, a script, or one `run` firing
parallel state requests) each redeemed the single-use refresh token, the losers
tripped reuse-detection, and the family was revoked, forcing a re-login that
read as "the token expires instantly." `SessionTokenSource` now serializes
refresh across goroutines (singleflight) and processes (advisory file lock in
`internal/cliauth`), re-checks the stored token after winning the lock
(double-checked reload → siblings reuse the freshly rotated token), and refreshes
proactively at 60 s before expiry. Pairs with the platform's sliding idle window
(OP1 hardening). The paired platform-side reuse-grace interval (R11, Option A)
shipped in orun-cloud PR #62: a refresh-token replay within ~10 s of its
rotation is re-served the same successor idempotently instead of revoking the
family, absorbing the lost-response / concurrent-redemption races the client
lock cannot fully prevent.

## OC2 — Cloud link & scope resolution — ✅ Done

Pairs with OP4.

- `cloud link`: remote-URL normalization (ssh/https/`.git` forms), resolve
  call, interactive org/project picker (create-project path included),
  non-interactive `--org/--project` form, RepoLink cache write.
- `cloud unlink`, `cloud status`, `cloud open` (console URL from org/project
  slugs).
- Unlinked/unauthenticated fail-fast errors on every `--remote-state` entry
  point (design §7 rows 2–3), with the exact next command.

**Done when:** against stage, a fresh clone goes `auth login` → `cloud link` →
`run --remote-state` with no flags; both error rows render as specified; link
in a repo with multiple candidate orgs presents the picker; non-interactive
form works headless.

## OC3 — Remote state v1 — 🚧 In progress (core landed)

Pairs with OP2 + OP3. The core milestone, landing in increments.

- ✅ **Increment 1 (PR #367):** `doJSONOnce` unwraps the platform `{data,meta}`
  success envelope (the OC0 latent bug where wrapped run/object responses decoded
  to zero-values).
- ✅ **Increment 2 (PR #368) — the v1 client + plan objsync.** Stage-verified:
  the `foundation→api→web` 6-job DAG runs to completion under `--remote-state`.
  - `internal/remotestate/objsync.go`: `objects/missing` → `PUT objects/{digest}`
    (single-shot, kind=plan). `statebackend.RemoteStateBackend.InitRun` now
    serializes the plan, ensures it in the object plane, and creates the run by
    **digest** (no inline plan).
  - Run id ↔ ULID: the CLI execId maps deterministically to a contract-valid
    Crockford ULID (`RunULID`), so the wire id passes `isRunUlid` and the same
    execId resumes the same run (idempotent create — stage-verified).
  - v1 run create/get/list, structured claim outcomes (`already_claimed` /
    `deps_not_ready` / `terminal`, mapped to the runner's `ClaimResult`),
    server-supplied lease/heartbeat tunables surfaced, `lease_lost` as a typed
    `APIError`. Append-only chunked logs (delta per step, ≤1 MiB) + `fromSeq` read.
  - Surfaced (and fixed, platform-side) three latent server bugs only a real
    Postgres exercise catches: objects/missing array-param bind
    (orun-cloud #58), claim/runnable correlated-SRF deps guard (#60), and
    deps/labels stored as a jsonb scalar string (#61).

- ✅ **Increment 3 (PR #369) — lease handling.** Stage-verified.
  - Heartbeats run at the **server-supplied interval** (from the claim), keyed to
    a per-job context so they stop when the job ends (fixing a goroutine leak
    where the old beat ran against `context.Background()` forever).
  - `lease_lost` (heartbeat 409) **preempts the job**: a new `runner.OnJobStart`
    hook hands the per-job cancel to the heartbeat, which cancels the job and
    suppresses the terminal update — work another runner has taken over stops
    silently (design §4). Unit-tested (forcing a live partition is impractical).
  - Verified on stage: the DAG completes unchanged (regression); a 36 s job's
    lease stays alive across the 20 s interval (heartbeat observed in the
    state-worker tail, zero `lease_lost`); two runners on one job → exactly one
    executes, the other waits and exits clean (no double-claim).

- ✅ **Increment 4 (PR #370) — `--local` escape hatch + run-start degradation.**
  Stage-verified. `orun run --local` forces local filesystem state, overriding
  remote config/flags (with a one-line bypass note); when the backend is
  unreachable (or 5xx) at run start, the run fails fast with an actionable error
  that points at `--local` — never a silent fallback to local (design §7 row 4).
  Point reads were already working from increment 2: `orun status --remote-state`
  and `orun logs --remote-state` render a completed cloud run (state, progress,
  per-job step logs) indistinguishably from a local one.

- ✅ **Increment 5 (PR #371) — `orun logs --follow` (fromSeq live tail).**
  Stage-verified. A new `statebackend.Backend.TailJobLog(runId, jobId, fromSeq)`
  (over `client.ReadLog`) powers `orun logs --follow --job <id> --remote-state`,
  which polls the cursor and prints new chunks until the job is complete (Ctrl-C
  to stop; bounded reconnect on transient errors). Verified against stage by
  tailing a live job's output as its steps landed.

- ✅ **Increment 6 — log pipeline spill-to-file.** `internal/remotestate/logpipe`:
  the runner's per-step log upload now goes through a bounded buffer that spills
  to an ndjson file (`$TMPDIR/orun-logspill-<execId>.ndjson`) when the backend is
  unreachable mid-run, re-drains oldest-first (with a retry backoff) on recovery,
  and at run end exits non-zero with "state may be stale on the server" (pointing
  at the spill file) if anything is still undelivered (design §7 row 5). Append
  is non-blocking — a down backend never interrupts execution. Unit-tested
  (happy path, buffer-while-down → drain-on-recover in order, memory-bound spill
  to file, undrained-at-close persistence + report, backoff suppression).

Remaining OC3 increments (unstarted):

- Cockpit TUI over cloud runs (`ListRuns` on the client exists; the TUI bridge
  wiring does not yet). Multi-job `logs --follow` (currently single `--job`).
- Full kill -9 lease-recovery timing gate run live (the pieces — atomic claim,
  heartbeat, server sweep re-queue — are each verified; the end-to-end ~60 s
  lapse+resume was not run). **Stage-only gate.**
- Live mid-run network-cut scripted test exercising increment 6 against stage
  (the spill/drain logic is unit-verified; the live row-5 walkthrough remains).

**Done when:** against stage, a multi-job DAG runs to completion under
`--remote-state`; kill -9 of the runner mid-job → a second invocation resumes
and finishes the run (lease recovery observed); the OP2 contention test passes
with this client; `status --watch` and the cockpit show a cloud run
indistinguishably from a local one; mid-run network cut follows row 5 of the
degradation table in a scripted test.

## OC4 — Object & catalog push — 🚧 In progress (objsync landed)

Pairs with OP3 + OP7.

- ✅ `internal/remotestate/objsync`: digest negotiation, single-shot PUT ≤25 MiB,
  and the **chunked multipart sub-protocol above the budget** (start → per-part
  PUT → complete; the server reassembles from its own part records and verifies
  the assembled digest, so the client sends only the kind header at complete),
  kind headers throughout. `EnsureObject` dispatches by size; reused by OC3's
  plan sync. Unit-tested (start/parts/complete in order + reassembly, size
  dispatch). **Stage gate:** the live 100 MiB multipart round-trip remains.
- 🗓️ `orun catalog push` / `catalog refresh --push` / `cloud.catalog.autopush`:
  snapshot sync + head advance with source commit; output shows missing-blob
  count and head transition. (Pairs with OP7; not started.)
- 🗓️ Environment-scoped heads (`--environment`).

**Done when:** against stage, first push uploads, second push transfers ~zero
bytes (negotiation verified); the pushed catalog renders in the console (OP7
gate); a 100 MiB synthetic object round-trips via multipart; autopush stays
off by default and is exercised in a test when enabled.

## OC5 — Secrets in the runner — 🗓️ Planned

Pairs with OP8.

- Runner secret provider: per-claimed-job `secrets/resolve` with live lease,
  env injection, fail-closed on error (dependent job fails before the step
  starts, independent jobs continue).
- Redactor: resolved values registered with the log pipeline; every chunk
  scrubbed before upload; covers multi-line values; documented residual risk
  for transformed values.
- `orun secrets set/list/rm` (cli-surface §5): stdin/prompt-only values,
  metadata-only list.

**Done when:** against stage, a step that `echo`s a secret shows `***` in
`orun logs` and the console; the value never appears in any file under
`~/.orun` or the local object store (scripted grep gate); resolve-denied
(policy) and resolve-missing (key) both fail the job with the platform error;
OP8's rotation-grace gate passes with this client.

## OC6 — CI golden path + conformance — 🗓️ Planned (scope narrowed, D5)

Pairs with OP5.

> **D5 (decided 2026-06-16): drop `orun backend init` (OSS single-tenant
> self-host) for now.** The embedded Cloudflare-Worker + D1 backend is NOT
> migrated to the v1 `_local/_local` contract, and the "one API, two
> implementations" promise is parked (revivable later). OC6 therefore narrows to
> the OIDC CI golden path; the conformance suite is retained as the executable
> contract but runs against **stage only** — no dual-server / OSS target, and no
> "switch backend by URL change" demo gate.

- `OIDCTokenSource` → `POST /v1/auth/oidc/exchange` (audience `orun-cloud`),
  selected automatically in GHA (design §3 selection order); `ORUN_ORG`/
  `ORUN_PROJECT` env scoping for CI. (Today `OIDCTokenSource` sends the raw GHA
  token straight to the backend; this inserts the exchange step.)
- Generated/documented GHA workflow: `permissions: id-token: write` +
  `orun run --remote-state` with zero stored secrets; docs page with the
  trust-binding setup pointer.
- **Conformance suite**: a Go test package driving the full `Backend`
  interface + objects + secrets-resolve against any base URL; run against
  **state-worker on stage** as the contract's executable form. (The dual-server
  run against an OSS `orun backend` is dropped with D5; the suite is written
  base-URL-agnostic so a future OSS target can be re-added if D5 is revived.)
- ~~`orun backend` (OSS server) updated to the v1 contract~~ — **dropped (D5).**

**Done when:** a public example repo's GHA run executes against stage via OIDC
with no stored secret; an unbound repo's exchange is denied (OP5 gate); the
conformance suite passes against state-worker on stage in CI.

## Sequencing

```
OC0 ─→ OC1 ─→ OC2 ─→ OC3 ─→ OC4 ─→ OC5 ─→ OC6
                      (plan-only objsync lands in OC3; full objsync in OC4)
```
