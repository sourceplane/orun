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
(OP1 hardening). Tracked platform follow-up: reuse grace interval (needs review).

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

Remaining OC3 increments (unstarted):

- `lease_lost` "stop the job silently" runner wiring + server-supplied interval
  scheduling; the OP2 contention + kill -9 lease-recovery gates.
- Log pipeline hardening: bounded buffering with spill-to-file when the backend
  is unreachable mid-run, drain-on-recover, non-zero exit + warning when
  undrained (design §7 row 5).
- Reads parity: `bridge.FromBackend` wired so `status`, `logs --follow` (fromSeq
  tail), and the cockpit render cloud runs through the existing viewmodels
  (`ListRuns` on the client now exists; the bridge wiring does not yet).
- `run --local` escape hatch; no silent fallback.

**Done when:** against stage, a multi-job DAG runs to completion under
`--remote-state`; kill -9 of the runner mid-job → a second invocation resumes
and finishes the run (lease recovery observed); the OP2 contention test passes
with this client; `status --watch` and the cockpit show a cloud run
indistinguishably from a local one; mid-run network cut follows row 5 of the
degradation table in a scripted test.

## OC4 — Object & catalog push — 🗓️ Planned

Pairs with OP3 + OP7.

- `internal/remotestate/objsync`: digest negotiation, single-shot PUT ≤25 MiB,
  multipart above, kind headers; reused by OC3's plan sync.
- `orun catalog push` / `catalog refresh --push` / `cloud.catalog.autopush`:
  snapshot sync + head advance with source commit; output shows missing-blob
  count and head transition.
- Environment-scoped heads (`--environment`).

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

## OC6 — CI golden path + conformance — 🗓️ Planned

Pairs with OP5; closes D5 (conformance) from the platform risks doc.

- `OIDCTokenSource` → `POST /v1/auth/oidc/exchange` (audience `orun-cloud`),
  selected automatically in GHA (design §3 selection order); `ORUN_ORG`/
  `ORUN_PROJECT` env scoping for CI. (Today `OIDCTokenSource` sends the raw GHA
  token straight to the backend; this inserts the exchange step.)
- Generated/documented GHA workflow: `permissions: id-token: write` +
  `orun run --remote-state` with zero stored secrets; docs page with the
  trust-binding setup pointer.
- **Conformance suite**: a Go test package driving the full `Backend`
  interface + objects + secrets-resolve against any base URL; CI runs it
  against the OSS `orun backend` server; the platform repo runs the same suite
  against state-worker on stage. The suite is the contract's executable form.
- `orun backend` (OSS server) updated to the v1 contract with fixed
  `_local/_local` scope. (Today `orun backend init` provisions an embedded
  Cloudflare Worker + D1 on a single-tenant *namespace* model; this migrates it
  to org/project `_local/_local`.)

**Done when:** a public example repo's GHA run executes against stage via OIDC
with no stored secret; an unbound repo's exchange is denied (OP5 gate); the
conformance suite passes against both servers in both repos' CI; switching a
workspace between OSS backend and stage is demonstrated as a URL change.

## Sequencing

```
OC0 ─→ OC1 ─→ OC2 ─→ OC3 ─→ OC4 ─→ OC5 ─→ OC6
                      (plan-only objsync lands in OC3; full objsync in OC4)
```
