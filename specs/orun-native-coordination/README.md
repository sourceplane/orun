# Spec: orun-native-coordination

**The CLI's remote-state client moves from a `runs/jobs/claim` REST dialect to an
append-events / read-the-log client over the content-addressed store.** A run
becomes an append-only event stream rooted at the plan's source hash; the runner
coordinates by appending `JobClaimed`/`Heartbeat`/`Complete` and folding the
stream, and a completed job ships a **content-addressed `job-result`** that any
later run can reuse. Local stays first: the same event log and fold drive offline
runs, and cloud sync just ships the appends.

Paired epic on the platform side:
`orun-cloud/specs/epics/saas-orun-backend-merge/` (cluster **BM**). The two share
one normative wire contract — the platform owns the copy
([`coordination-api.md`](../../../orun-cloud/specs/epics/saas-orun-backend-merge/coordination-api.md));
this repo **vendors** it under [`vendored/`](./vendored/) with a checksum drift
guard (same mechanism as `specs/orun-cloud/vendored/`). Neither repo may break
the contract unilaterally.

> This supersedes the coordination half of `specs/orun-cloud` (OC3): that client
> spoke the relational `state-api-contract.md` §2. This epic is the greenfield
> event-sourced replacement — no backward compatibility with the old `/v1/runs`
> dialect is kept (the platform drops it; only a transient cutover drain bridge
> exists).

## Status

| Field | Value |
|-------|-------|
| Status | **In progress (cores landed, not wired)** — audited 2026-06-20. NC0 ✅; NC1/NC2/NC4/NC5 🟡 partial; NC3 ⛔. Pure `Fold`, `jobInputHash`/memo gate, the claim/heartbeat→action core, `CoordClient`, and `RunLoop` are built and Go-test-green — **but the entire new stack is a dead-code island**: `cmd/orun/command_run.go:502-504` still constructs `remotestate.NewClientWithScope` → `NewRemoteStateBackend` (legacy relational `/claim`,`/update`,`/runnable`), and `statebackend.Backend` was never reshaped. Gaps + evidence: `orun-cloud/specs/epics/saas-orun-backend-merge/GAPS.md`. |
| Cluster | **NC** (NC0–NC4) |
| Builds on | `internal/statebackend` (the `Backend` interface — reshaped here), `internal/remotestate` (HTTP client + the three `TokenSource`s), `specs/orun-object-model/` (content-addressed store — `job-result`/`log` are new kinds), `internal/cockpit` (`bridge.Source` folds the stream) |
| Pairs with | `orun-cloud/specs/epics/saas-orun-backend-merge/` (cluster **BM**) |
| Contract | vendored `coordination-api.md` (owned by the platform; drift-guarded) |
| Decisions locked | local-first forever (offline runs keep a local event log + the same fold; cloud is additive); the client is **implementation-agnostic** (it never knows the server shards runs on a Durable Object); coordination is **append + fold**, not row CRUD; execution is **at-least-once** (lease takeover can re-run a slow job — steps must be idempotent); memoization is **opt-in** per job (`hermetic`) and never required for correctness; all cloud behavior stays behind `statebackend.Backend` + `bridge.Source` — the runner/cockpit/compiler do not learn HTTP |

## The one-paragraph thesis

The client already has the right bones: `statebackend.Backend` abstracts run
state, `remotestate.Client` speaks HTTP with retries, and the object model gives
every plan and snapshot a content address. What changes is the *shape of the
coordination surface*. Instead of `ClaimJob → UpdateJob → RunnableJobs` against
mutable rows, the client **appends typed coordination events** to a per-run
stream and **folds the stream** to know the run — the same reduction the server
and projector use. Completion uploads a `job-result` object, so a later run can
skip a hermetic job on a content-hash hit. The cockpit, `status`, and `logs`
render cloud runs by folding the same stream they fold locally — one model,
online or off.

## Read order

1. `README.md` (this file) — thesis, status, milestones, dependency map.
2. `design.md` — the reshaped `Backend` interface, the append/fold client, result
   push + memoization, the local event log + cloud sync, degradation.
3. `cli-surface.md` — commands/flags this spec adds or changes.
4. `implementation-plan.md` — NC0–NC4 with "done when".
5. `risks-and-open-questions.md`.

## Milestones at a glance

| ID | Milestone | Status |
|----|-----------|--------|
| NC0 | Vendor `coordination-api.md` + checksum guard; port the shared **fold** (events → run/jobs/frontier) into `internal/statebackend` | ✅ Done (pairs **BM0**) |
| NC1 | Result plane: push `job-result`/`log` objects; cache-aware claim (skip exec on `cached`) | 🟡 Partial — hash/memo gate done; no result push, no `--no-cache`, no `hermetic` field, unwired (pairs **BM1**) |
| NC2 | Event-log client: `AppendClaim/Heartbeat/Complete`, conditional-append semantics, `ReadLog(from)` + local fold; reshape `statebackend.Backend` | 🟡 Partial — action core + `CoordClient` done; `Backend` NOT reshaped; no heartbeat; unwired (pairs **BM2**) |
| NC3 | Cockpit/`status`/`logs` over the folded stream (SSE/long-poll `--follow`); offline local event log + cloud sync | ⛔ Missing — still row-read + poll; no stream fold, SSE, or offline log (pairs **BM3**) |
| NC4 | CI OIDC golden path on the new surface + conformance suite vs stage | 🟡 Partial — `OIDCTokenSource` wired to the **legacy** client; no stage conformance suite (pairs **BM5**) |

## Cross-repo dependency map

| This repo (**NC**) | Platform (`saas-orun-backend-merge`, **BM**) | Seam |
|---|---|---|
| NC0 vendor + fold | BM0 contract v2 + shared fold | `coordination-api.md` + `reduce()` |
| NC1 result push | BM1 object kinds + memoization | `job-result`/`log` objects ↔ object-model sync |
| NC2 event-log client | BM2 DO coordination shard | conditional-append verbs ↔ `internal/remotestate` |
| NC3 read-the-log UX | BM3 projections + `…/log` SSE | event stream ↔ `bridge.Source`/cockpit |
| NC4 OIDC golden path | BM5 auth/quota | exchange + ActorContext ↔ `OIDCTokenSource` |

## Scope boundary

| In scope | Out of scope |
|----------|--------------|
| The reshaped `statebackend.Backend` (append/fold/read-the-log), `job-result`/`log` push, cache-aware claim, the local event log + cloud sync, cockpit/status/logs over the stream, CI OIDC golden path, conformance vs stage | The server (→ `saas-orun-backend-merge`), the catalog model (→ `orun-service-catalog`), platform-hosted runners, secrets (→ `orun-secrets`), the legacy `/v1/runs` client (dropped, not maintained) |
