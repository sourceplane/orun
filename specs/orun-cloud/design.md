# orun-cloud — Design (client side)

Status: Draft. The platform-side model (tenancy, auth doors, the two state
planes, secrets, console surfaces) is specified in
`orun-cloud/specs/epics/saas-orun-platform/design.md`; the wire contract
in `state-api-contract.md` there is normative. This doc covers what changes
*in this codebase* and the principles that keep cloud integration seamless.

## 1. Principles

1. **Local-first forever.** No command requires an account. Cloud enriches:
   shared run state, the team catalog, secrets, CI coordination. Every cloud
   failure has a defined degradation (§7) — orun never strands a user because
   a SaaS is down.
2. **The interfaces already drawn are the integration.** All cloud behavior
   stays behind `statebackend.Backend` (runner/state) and `bridge.Source`
   (cockpit/status/logs). The compiler, runner, and TUI gain zero HTTP
   knowledge. If a change can't be expressed behind those interfaces, the
   interfaces get a deliberate revision — not a bypass.
3. **One contract, two servers.** Orun Cloud (multi-tenant) and the OSS
   `orun backend` server (single-tenant, `_local/_local` scope) implement the
   same surface. The conformance suite (OC6) runs against both. Switching is a
   URL change; this is a public promise, so the client never special-cases
   which server it talks to.
4. **Same digests everywhere.** The remote object plane is keyed by the same
   content addresses as the local object store (`specs/orun-object-model/`).
   Sync is therefore "ask what's missing, push those blobs, move a head" — no
   translation layer, no drift.

## 2. Tenancy & scope resolution

The platform spine is org → project → environment. The CLI's job is resolving
"this working directory" to that spine exactly once, then never asking again:

- `orun cloud link` calls `GET /v1/cli/links/resolve?remoteUrl=…` (normalized
  git remote), presents the actor's candidate orgs/projects, creates the
  project server-side if chosen, and caches the result in the existing
  `RepoLink` (`internal/cliauth/types.go`) extended to
  `{remoteURL, orgID, orgSlug, projectID, projectSlug}`.
- Every state call is path-scoped:
  `/v1/organizations/{orgId}/projects/{projectId}/state/…`. `remotestate.Client`
  gains a `Scope{OrgID, ProjectID}` set at construction from the RepoLink (or
  `--org`/`--project` overrides; or `ORUN_ORG`/`ORUN_PROJECT` in CI).
- The "namespace" wording (`allowedNamespaceIds`, cloud-link namespace prompts)
  retires in OC0; the session payload's `orgs[]` is the replacement.
- Environments come from `intent.yaml` as today; the run create call carries
  the environment name and the platform auto-registers it.

Unlinked-repo UX: any `--remote-state` invocation in an unlinked repo fails
fast with the exact next command (`orun cloud link`), not a 404 from the
server.

## 3. Auth & sessions

`internal/cliauth` keeps its shape; its endpoints move from the reference
backend/GitHub device flow to the platform:

- **BrowserLogin** → `POST /v1/auth/cli/start`, open `authorizeUrl`, listen on
  the loopback for the single-use grant, redeem for a session. The approval
  page (platform-side) shows host + requested scope.
- **DeviceLogin** → `POST /v1/auth/cli/device/start` / `…/poll` (RFC-8628
  shape). This replaces driving *GitHub's* device flow — the platform owns the
  grant; GitHub identity is just one of its login methods.
- **Session**: access JWT (~15 min) + rotating refresh (~30 d), stored at
  `~/.orun/credentials.json` (0600 enforced; macOS already uses the
  `io.sourceplane.orun` keychain — broader keychain coverage is the follow-up,
  risks R5). `SessionTokenSource` keeps its refresh-loop contract:
  refresh via `POST /v1/auth/cli/token`; a `family_revoked`/401 response clears
  the session and surfaces "run `orun auth login`" once, not per call.
- **Token sources** stay exactly three, now with real servers behind each:
  `SessionTokenSource` (humans), `OIDCTokenSource` (CI → `POST
  /v1/auth/oidc/exchange`, audience `orun-cloud`), `StaticTokenSource`
  (`ORUN_TOKEN`, platform `sk_` API keys). Selection order in CI:
  OIDC if `ACTIONS_ID_TOKEN_REQUEST_URL` is present, else static, else session.

## 4. The state client

`internal/remotestate/client.go` updates in place (it has one consumer —
`statebackend.RemoteStateBackend`):

- Scoped paths + `Orun-Contract-Version: 1` header; a
  `contract_version_unsupported` response renders as "this orun is too
  old/new for the backend" with the supported range — version skew fails loud.
- Run create sends the client-minted ULID and the **plan digest** (not the
  plan body): the plan blob ships through the object plane first (§5), so
  InitRun is cheap and idempotent. `statebackend.RemoteStateBackend.InitRun`
  composes: ensure plan object → create run.
- Claim/heartbeat/update adopt the contract's structured outcomes
  (`already_claimed`, `deps_not_ready`, `lease_lost`); the runner treats
  `lease_lost` as "stop this job silently — someone else owns it now", which
  is the crash-recovery story working as intended.
- Lease + heartbeat intervals come from server responses; no client-side
  constants.
- Error envelope: every surfaced error carries the platform `requestId` so a
  user can paste it into support.
- Existing retry/backoff (exponential, jittered) stays; writes are all
  idempotent by design so retries are safe.

`bridge.Source` parity: `FromBackend` already adapts a `Backend` for the
cockpit. OC3 adds the run-list call so `orun status`, `orun logs --follow`
(fromSeq tail), and the cockpit render cloud runs identically to local ones —
same viewmodels, zero TUI changes.

## 5. Object & catalog sync

A small `internal/remotestate/objsync` package:

```
Sync(ctx, digests []Digest) error   // POST objects/missing → PUT the gaps (≤25MiB single-shot, else multipart)
```

- **Plans:** `orun run --remote-state` syncs the plan blob before run create.
- **Catalog:** `orun catalog push` (and `--push` on `catalog refresh`) syncs
  the resolved snapshot (the entity envelope from `orun-service-catalog`) and
  advances the head for (project, environment?) with the source commit. Heads
  are how the platform's Catalog and drift surfaces light up; the CLI never
  re-uploads what the server has — digest negotiation makes repeat pushes
  near-free.
- Push is explicit or opt-in-automatic (`cloud.catalog.autopush: true` in
  config) — never silent, because publishing the catalog is a team-visible
  act.

## 6. Secrets in the runner

Steps already declare the env they need; compositions mark which keys are
secret-sourced. At execution time, per claimed job:

- The runner calls `POST …/runs/{runId}/secrets/resolve` with its live lease +
  the declared keys, injects values into the step env, and registers each
  value with the log pipeline's **redactor** — every log chunk is scrubbed
  (`***` substitution) before `AppendStepLog` uploads it.
- Values live only in the step process env: never in local state files, the
  object store, plan blobs, or `~/.orun`. A failed resolve fails the job
  before the step starts (fail closed), with the platform error (`forbidden`,
  quota, missing key) surfaced verbatim.
- `orun secrets` (cli-surface §5) manages values through the same write-only
  platform API — set/rotate/list-metadata, never read.

## 7. Degradation table (normative)

| Condition | Behavior |
|---|---|
| No session, no `--remote-state` | everything works locally (status quo) |
| `--remote-state`, no session | fail fast: "run `orun auth login`" |
| `--remote-state`, repo unlinked | fail fast: "run `orun cloud link`" |
| Backend unreachable at run start | error with `--local` escape hatch suggested; **no silent fallback** (a team expecting shared state must notice) |
| Backend lost mid-run | runner keeps executing current jobs, buffers log chunks (bounded, memory + spill file), retries with backoff; exits non-zero with "state may be stale on the server" if buffers can't drain |
| Secret resolve fails | the dependent job fails closed; independent jobs continue |
| Catalog push fails | warning, exit 0 for the underlying command (push is enrichment) |
| Contract version mismatch | loud, actionable, immediate (§4) |

## 8. Configuration

`~/.orun/config.yaml` (user) and `intent.yaml` `execution.state` (repo) gain a
single `cloud` block; the existing `backend.url` key remains as a deprecated alias one release:

```yaml
cloud:
  url: https://api.orun.cloud        # or the self-hosted backend URL
  catalog:
    autopush: false
# intent.yaml — execution.state.mode: remote selects the cloud backend;
# org/project come from the RepoLink, never from intent (intent is shared,
# tenancy is per-user trust).
```

Flag/env precedence (highest first): `--backend-url`/`--org`/`--project` →
`ORUN_BACKEND_URL`/`ORUN_ORG`/`ORUN_PROJECT`/`ORUN_TOKEN` → repo intent →
user config.
