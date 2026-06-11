# orun-cloud — Implementation Plan (OC0–OC6)

Status: Draft. Each milestone pairs with a platform milestone
(`saas-orun-platform`, OP0–OP9); "done when" gates verify against the stage
deployment with a real binary from this branch. OC0 is human-independent and
safe to land first.

## OC0 — Contract alignment — 🗓️ Planned

Pairs with OP0 (contracts exist server-side, even dormant).

- Vendor the wire contract: `specs/orun-cloud/vendored/state-api-contract.md`
  copied verbatim from the platform repo + a CI check that diffs it against
  the source (drift fails the build with "re-vendor or renegotiate").
- `internal/remotestate`: `Scope{OrgID, ProjectID}` on the client; scoped path
  construction; `Orun-Contract-Version: 1` header;
  `contract_version_unsupported` rendered actionably; platform error envelope
  parsed everywhere (`code`, `requestId` surfaced).
- Retire "namespace": `SessionResponse.allowedNamespaceIds` →
  `orgs []OrgRef{ID, Slug, Name, Role}` (storage migration reads old
  session.json once, rewrites); `RepoLink` gains org/project IDs + slugs.
- Config: `cloud.url` block in user config + `execution.state` intent wiring;
  `backendUrl` honored as deprecated alias with a one-line warning; precedence
  per design §8, with tests.

**Done when:** unit tests cover path construction, version-header rejection
rendering, error-envelope parsing, config precedence, and session-file
migration; the vendored-contract CI check is live; existing local-mode tests
still pass untouched (no behavior change without `--remote-state`).

## OC1 — Auth completion — 🗓️ Planned

Pairs with OP1.

- `internal/cliauth`: BrowserLogin against `POST /v1/auth/cli/start` +
  loopback grant redeem; DeviceLogin against the platform device endpoints
  (GitHub's device flow removed); rotating-refresh handling in
  `SessionTokenSource` (single-use refresh, `family_revoked` → clear session +
  one actionable error).
- Session storage: enforce 0600 on write, refuse 0644 reads with a fix-it
  message; `auth status` shows user, orgs + roles, expiry, backend URL.
- `auth token` prints a fresh access token (refreshing if needed).

**Done when:** against stage, `auth login`, `auth login --device`,
`auth status`, `auth token`, `auth logout` all work end-to-end; access-token
expiry mid-`run` refreshes transparently; console-side revoke makes the next
CLI call fail with the re-login message; recorded in the paired OP1 gate.

## OC2 — Cloud link & scope resolution — 🗓️ Planned

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

## OC3 — Remote state v1 — 🗓️ Planned

Pairs with OP2 + OP3. The core milestone.

- `statebackend.RemoteStateBackend` over the v1 contract: InitRun = ensure
  plan object (via OC4's `objsync`, landed minimally here for plans) + create
  run with client ULID + digest; structured claim outcomes; `lease_lost` stops
  the job silently; server-supplied lease/heartbeat intervals.
- Log pipeline: chunked `AppendStepLog` (≤1 MiB), bounded buffering with
  spill-to-file when the backend is unreachable mid-run, drain-on-recover,
  non-zero exit + warning when undrained (design §7 row 5).
- Reads: run list on the client; `bridge.FromBackend` wired so `status`,
  `logs --follow` (fromSeq tail), and the cockpit render cloud runs through
  the existing viewmodels.
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
  `ORUN_PROJECT` env scoping for CI.
- Generated/documented GHA workflow: `permissions: id-token: write` +
  `orun run --remote-state` with zero stored secrets; docs page with the
  trust-binding setup pointer.
- **Conformance suite**: a Go test package driving the full `Backend`
  interface + objects + secrets-resolve against any base URL; CI runs it
  against the OSS `orun backend` server; the platform repo runs the same suite
  against state-worker on stage. The suite is the contract's executable form.
- `orun backend` (OSS server) updated to the v1 contract with fixed
  `_local/_local` scope.

**Done when:** a public example repo's GHA run executes against stage via OIDC
with no stored secret; an unbound repo's exchange is denied (OP5 gate); the
conformance suite passes against both servers in both repos' CI; switching a
workspace between OSS backend and stage is demonstrated as a URL change.

## Sequencing

```
OC0 ─→ OC1 ─→ OC2 ─→ OC3 ─→ OC4 ─→ OC5 ─→ OC6
                      (plan-only objsync lands in OC3; full objsync in OC4)
```
