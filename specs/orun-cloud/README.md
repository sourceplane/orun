# Spec: orun-cloud

**orun's remote state backend graduates from a reference implementation into
the client of a real multi-tenant platform: Orun Cloud.** The CLI
authenticates against the SaaS, stores plans, runs, logs, and catalog
snapshots in it, resolves secrets from it at execution time — and stays fully
functional offline, because cloud is additive, never required.

Paired epic on the platform side:
`orun-cloud/specs/epics/saas-orun-platform/` (cluster **OP**). The two
share one wire contract — the platform owns the normative copy
(`state-api-contract.md` there); this repo vendors it and CI diffs the vendored
copy (OC0). Neither repo may break the contract unilaterally.

## Status

| Field | Value |
|-------|-------|
| Status | **Draft → Ready for review** |
| Cluster | **OC** (OC0–OC6) |
| Builds on | `internal/statebackend` (the `Backend` interface), `internal/remotestate` (HTTP client + the three `TokenSource`s), `internal/cliauth` (loopback + device flows, session storage), `specs/orun-object-model/` (content-addressed store — remote sync pushes the same digests), `specs/orun-service-catalog/` (the snapshot envelope the catalog push ships) |
| Pairs with | `orun-cloud/specs/epics/saas-orun-platform/` — server-side epic (**OP**) |
| Decisions locked | local-first forever (every command works with no account; cloud failures degrade, never block); one wire contract, two server implementations (Orun Cloud + the OSS `orun backend` single-tenant server — a URL change, not a migration); tenancy is the platform's org → project → environment spine (the "namespace" wording retires); all cloud behavior stays behind the existing `statebackend.Backend` and `bridge.Source` interfaces — the runner, cockpit, and compiler do not learn about HTTP; secrets resolved at run time are never written to local state or unredacted logs |

## The one-paragraph thesis

The hard client work already exists: `statebackend.Backend` cleanly abstracts
run state, `remotestate.Client` speaks a REST dialect with retries and
backoff, `cliauth` implements browser-loopback and device login, and the
object model gives every plan and catalog snapshot a content address. What's
missing is the *real server* and the deltas its multi-tenancy implies:
org/project-scoped paths, a platform-owned login (instead of GitHub's device
flow), OIDC token exchange for CI, digest-negotiated object push, and a secret
provider in the runner. This spec is those deltas — six milestones that turn
`--remote-state` from a demo flag into the product's team mode, while the
cockpit, `status`, and `logs` render cloud runs through the same
`bridge.Source` they use for local ones.

## Read order

1. `README.md` (this file) — status, thesis, milestones, dependency map.
2. `design.md` — client architecture: auth/session, scope resolution, the
   backend client, object/catalog sync, secrets in the runner, degradation.
3. `cli-surface.md` — every command and flag this spec adds or changes.
4. `implementation-plan.md` — OC0–OC6 with "done when".
5. `risks-and-open-questions.md`.

## Milestones at a glance

| ID | Milestone | Status |
|----|-----------|--------|
| OC0 | Contract alignment (vendored contract, version header, scoped paths, config schema) | 🗓️ Planned |
| OC1 | Auth completion (platform login flows, session lifecycle, storage hardening) | 🗓️ Planned |
| OC2 | Cloud link & scope resolution (repo → org/project, overrides, unlinked UX) | 🗓️ Planned |
| OC3 | Remote state v1 (coordination client, idempotency, degradation, status/logs/cockpit) | 🗓️ Planned |
| OC4 | Object & catalog push (digest negotiation, plan/snapshot sync, heads) | 🗓️ Planned |
| OC5 | Secrets in the runner (resolve grants, env injection, redaction) | 🗓️ Planned |
| OC6 | CI golden path (OIDC exchange default in GHA) + OSS backend conformance | 🗓️ Planned |

## Cross-repo dependency map

| This repo | Platform (`saas-orun-platform`) | Seam |
|---|---|---|
| OC1 | OP1 CLI session auth | `/v1/auth/cli/*` ↔ `internal/cliauth` |
| OC2 | OP4 tenancy & workspace links | link/resolve API ↔ `RepoLink` |
| OC3 | OP2 run coordination (+OP3 logs) | contract §2 ↔ `internal/remotestate` |
| OC4 | OP3 objects + OP7 catalog | contract §3 ↔ object store sync |
| OC5 | OP8 secret manager | contract §4 ↔ runner secret provider |
| OC6 | OP5 OIDC federation | exchange endpoint ↔ `OIDCTokenSource` |

## Scope boundary

| In scope | Out of scope |
|----------|--------------|
| Auth/session against the platform, repo→org/project resolution, the v1 state client (runs/objects/logs), catalog snapshot push, runtime secret injection + redaction, CI OIDC golden path, OSS-backend conformance suite, cockpit/status/logs parity over cloud runs | The server (→ `saas-orun-platform`), the catalog model itself (→ `orun-service-catalog`), platform-hosted runners, the console |
