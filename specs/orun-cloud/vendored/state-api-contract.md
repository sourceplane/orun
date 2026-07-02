# Orun Platform — State API Contract (v1)

Status: Draft → freezes at OP2. **Normative.** This is the seam between the
two repos: `apps/state-worker` + api-edge implement the server; Orun's
`internal/remotestate` implements the client; the OSS self-host backend
implements the same surface. Changes after freeze follow the platform's
contract change-control (additive or versioned, never silently breaking).

Shaped deliberately to be a multi-tenant superset of the client Orun already
ships (`internal/remotestate/client.go`, `internal/statebackend/backend.go`):
every `Backend` method maps to exactly one endpoint.

## 0. Conventions

- Base path: `/v1/organizations/{orgId}/projects/{projectId}/state`
  (path-scoped tenancy per `core/domain-model.md`; the OSS single-tenant
  backend serves the same paths with a fixed `_local/_local` scope so one
  client codepath serves both).
- Auth: `Authorization: Bearer <token>` — CLI session access token, OIDC-
  exchanged workflow token, or `sk_` API key. All resolve to ActorContext.
- Versioning: `Orun-Contract-Version: 1` request header; servers reject
  unknown majors with `409 contract_version_unsupported` + the supported range,
  so version skew fails loud and actionable at the CLI.
- Errors: the platform envelope emitted by api-edge (`apps/api-edge/src/http.ts`) —
  `{ error: { code, message, details?, requestId } }`. (The flat
  `packages/contracts/src/errors.ts` type is reconciled to this nested shape in
  OP0.) New codes:
  `already_claimed`, `lease_lost`, `deps_not_ready`, `run_terminal`,
  `object_missing`, `contract_version_unsupported`.
- Idempotency: run creation is keyed by client-supplied ULID `runId`; job
  transitions are idempotent by (runId, jobId, runnerId, status); object PUTs
  are idempotent by digest. `Idempotency-Key` is honored on all other POSTs
  (existing api-edge machinery).
- IDs: `runId` ULID (client-mintable, sortable); `jobId` from the plan DAG;
  `runnerId` opaque client string; digests `sha256:<hex>`.

## 1. Auth endpoints (identity-worker; org-independent paths)

| Endpoint | Purpose |
|---|---|
| `POST /v1/auth/cli/start` | begin browser-loopback login → `{ authorizeUrl, cliCode, expiresAt }` |
| `POST /v1/auth/cli/device/start` | device flow → `{ deviceCode, userCode, verificationUrl, interval, expiresAt }` |
| `POST /v1/auth/cli/device/poll` | poll → pending \| `{ session }` |
| `POST /v1/auth/cli/token` | refresh → `{ accessToken, expiresAt, refreshToken }` (rotating; reuse ⇒ family revoked) |
| `POST /v1/auth/cli/revoke` | logout (revokes session) |
| `POST /v1/auth/oidc/exchange` | GitHub OIDC JWT → `{ accessToken, expiresAt, orgId, projectId }` per trust binding |

Session payload (matches Orun's `SessionResponse`): `{ accessToken, expiresAt,
refreshToken, user: { id, email, displayName }, orgs: [{ id, slug, name, role }] }`.
`orgs[].id` serves the field the CLI currently calls `allowedNamespaceIds`.

## 2. Run coordination ↔ `statebackend.Backend`

| Backend method | Endpoint |
|---|---|
| `InitRun` | `POST …/state/runs` |
| `LoadRunState` | `GET …/state/runs/{runId}` |
| (list) | `GET …/state/runs?environment=&status=&cursor=` |
| `ClaimJob` | `POST …/state/runs/{runId}/jobs/{jobId}/claim` |
| `Heartbeat` | `POST …/state/runs/{runId}/jobs/{jobId}/heartbeat` |
| `UpdateJob` | `POST …/state/runs/{runId}/jobs/{jobId}/update` |
| (list jobs) | `GET …/state/runs/{runId}/jobs` |
| `RunnableJobs` | `GET …/state/runs/{runId}/runnable` |
| `AppendStepLog` | `POST …/state/runs/{runId}/logs/{jobId}` |
| `ReadJobLog` | `GET …/state/runs/{runId}/logs/{jobId}?fromSeq=` |
| (cancel) | `POST …/state/runs/{runId}/cancel` |

### 2.1 Create run

```jsonc
POST …/state/runs
{
  "runId": "01J…",                  // client ULID; replay returns the existing run (200, not 409)
  "planDigest": "sha256:…",         // must exist in the object plane (else 412 object_missing)
  "environment": "production",       // optional; registered on first use
  "source": "cli" | "ci",
  "git": { "commit": "…", "ref": "…", "dirty": false },
  "labels": { }                      // free-form, indexed for list filters
}
→ 201 { "run": Run }
```

`Run`: `{ runId, orgId, projectId, environment?, status, planDigest, source,
git, createdBy: ActorRef, createdAt, startedAt?, finishedAt?, jobCounts: { queued, running, succeeded, failed } }`.

### 2.2 Claim / heartbeat / update

```jsonc
POST …/jobs/{jobId}/claim     { "runnerId": "host-abc" }
→ 200 { "claimed": true, "leaseExpiresAt": "…", "attempt": 1 }
→ 200 { "claimed": false, "reason": "already_claimed" | "deps_not_ready" | "terminal" }

POST …/jobs/{jobId}/heartbeat { "runnerId": "host-abc" }
→ 200 { "leaseExpiresAt": "…" }            // 409 lease_lost if lease lapsed/reassigned

POST …/jobs/{jobId}/update    { "runnerId": "host-abc", "status": "succeeded" | "failed", "errorText": "…" }
→ 200 {}                                    // idempotent replay-safe; 409 lease_lost; terminal states sticky
```

Lease default 60s; heartbeat every 20s (server returns both so the client never
hardcodes). The server cron re-queues lapsed claims (attempt+1, bounded) or
marks `timed_out`.

### 2.3 Logs

```jsonc
POST …/logs/{jobId}   { "runnerId": "…", "content": "<chunk>" }   → 200 { "seq": 7 }
GET  …/logs/{jobId}?fromSeq=7                                      → 200 { "content": "…", "nextSeq": 12, "complete": false }
```

Chunks ≤ 1 MiB; `fromSeq` polling is the live-tail mechanism for console and
`orun logs --follow`.

## 3. Object plane (CAS)

```jsonc
POST …/state/objects/missing   { "digests": ["sha256:a…", "sha256:b…"] }
→ 200 { "missing": ["sha256:b…"] }

PUT  …/state/objects/{digest}            // body: blob bytes; headers: Orun-Object-Kind, Content-Length
→ 201 | 200 (already exists — no-op)     // server verifies digest; mismatch ⇒ 400

GET  …/state/objects/{digest}            → blob bytes
GET  …/state/objects?kind=catalog-snapshot&cursor=   → index listing
```

Kinds: `plan | catalog-snapshot | composition-lock | artifact-manifest`.
Single-request bodies up to the platform budget (default 25 MiB); larger blobs
use chunked upload: `POST …/objects/{digest}/uploads` → `{ uploadId, partSize }`,
`PUT …/uploads/{uploadId}/parts/{n}`, `POST …/uploads/{uploadId}/complete`
(maps to R2 multipart).

### 3.1 Catalog heads

```jsonc
PUT  …/state/catalog/head    { "digest": "sha256:…", "environment": null, "commit": "…" }
→ 200 { "head": …, "previous": … }        // digest must exist; emits catalog.head.advanced
GET  …/state/catalog/head?environment=
GET  …/state/catalog/heads/history?cursor=
GET  …/state/catalog/entities?kind=&owner=&q=&cursor=     // read-model for console/CLI search
```

## 4. Secrets (config-worker) — v3 (orun-secrets)

Revised to the canonical `orun-secrets` v3 design (`orun/specs/orun-secrets/`,
platform slice `../saas-secret-manager/`). The prior §4 sketch was never
implemented; this revision aligns the write path with the **shipped**
config-worker surface and makes the resolve lease-bound for real.

```jsonc
// Write path — the shipped …/config/secrets surface (org/project/environment scope)
POST   …/config/secrets                      { "secretKey": "…", "value": "…" }   // write-only; encrypted before persist
GET    …/config/secrets[?chain=true]         → metadata only (key, version, scope, rotatedAt, lastUsedAt; chain adds servesFrom)
POST   …/config/secrets/{id}/rotate          // append a version (history retained), bump head
DELETE …/config/secrets/{id}                 // revoke (head or ?version=)

// v3 additions
POST   …/config/secrets/import               // dotenv bulk (write-only)
GET    …/config/secrets/{id}/versions        // version metadata history
PUT    …/config/secret-policies              // tier-tagged SecretPolicy documents (idempotent by hash)
POST   …/config/secret-policies/evaluate     // dry-run decision (orun policy test)
GET    …/config/secrets/syncs                // materialization provenance
POST   …/config/secrets/{id}/reveal          // break-glass; elevated secret.reveal; alerted

// Run-scoped resolve — state plane; the ONLY value-returning machine route
POST …/state/runs/{runId}/secrets/resolve
{ "runnerId": "…", "jobId": "…", "leaseEpoch": 3, "refs": ["secret://acme/api/prod/DATABASE_URL", …] }
→ 200 { "secrets": { "DATABASE_URL": "…" },
        "resolved": [ { "key": "DATABASE_URL", "version": 9, "scope": "environment",
                        "personal": false, "decisionId": "dec_…" } ],
        "ttlSeconds": 300 }
→ 409 lease_lost | 403 typed policy denial | 404 resource-hiding
// Two independent checks: bearer authz AND a live job lease matching
// (runId, jobId, runnerId, leaseEpoch) — same epoch discipline as :heartbeat/:complete.
// Chain per key: personal(local-cli, owner) → environment → project → workspace → account.
// Emits secret.accessed per key; stamps last_used_at; personal rung never serves CI.
```

## 5. Workspace links

```jsonc
POST /v1/organizations/{orgId}/cli/links     { "remoteUrl": "git@github.com:acme/platform.git", "projectSlug": "platform"? }
→ 201 { "link": { orgId, orgSlug, projectId, projectSlug, remoteUrl } }   // creates project if absent (policy org.cli.link)
GET  /v1/cli/links/resolve?remoteUrl=…       → the orgs/projects this actor may link/use for that remote
```

The resolve endpoint powers `orun cloud link`'s picker; the response is what
the CLI caches in `RepoLink`.

## 6. Policy map (server-enforced, deny-by-default)

| Route group | Action |
|---|---|
| runs read / logs read | `state.run.read` |
| run create / claim / heartbeat / update / log append / cancel | `state.run.write` |
| objects read / write | `state.object.read` / `state.object.write` |
| catalog head read / entities | `catalog.read` |
| catalog head advance | `catalog.publish` |
| secrets metadata / write / runtime resolve / break-glass reveal | `secret.read` / `secret.write` / `secret.value.use` / `secret.reveal` (elevated) |
| workspace link create | `org.cli.link` |
| OIDC trust bindings | `org.ci.trust.write` |
