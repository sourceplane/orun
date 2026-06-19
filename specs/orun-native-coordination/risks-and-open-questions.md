# orun-native-coordination — Decisions (locked) & residual risks

Status: **Locked — ready for implementation.** Client-side decisions are resolved
below. Contract-level decisions (event envelope, fold, `runId`, `jobInputHash`,
memoization scope, quota, drain window, SLOs) are owned platform-side and locked
in `orun-cloud/specs/epics/saas-orun-backend-merge/risks-and-open-questions.md`
(G0, C1–C10, D1–D5, O1–O3) — not re-litigated here.

## Locked decisions (client side)

- **Q1 Fold parity** — the platform owns `reduce()`; the CLI ports it to Go. The
  **shared golden-vector suite** (events → expected fold), checked into both repos
  and run in both CIs, is the source of truth; the prose contract is secondary; no
  codegen unless drift recurs (NC0).
- **Q2 `jobInputHash` parity** — the **client is the sole hasher** (platform C5);
  the server only stores/looks-up, never recomputes, so there is no two-sided
  computation to disagree. The hash definition is pinned by a shared vector (NC1).
- **Q3 Offline ↔ cloud authority** — a run is **either** local-authoritative
  **or** cloud-authoritative for its whole lifetime; no half-offline live race.
  An offline run syncs as a **distinct `runId`** (import → `run-record`), and
  idempotent appends make re-sync safe (NC3).
- **Hermetic opt-in** — the client never marks a job cacheable unless its plan
  declares `hermetic: true` (platform C6); default off (NC1).
- **Implementation-agnostic client** — the CLI never learns the server shards runs
  on a Durable Object or that Postgres is a delayed projection; the same binary
  speaks to the hosted (DO) and OSS (plain-Postgres) servers (locked).

## Residual engineering risks

- **R1 Local scheduler is advisory only (medium; NC2).** The client no longer
  gates deps — the server `:claim` does. A scheduler bug can only cause rejected
  claims (`deps_not_ready`), never a deps-gate escape. Mitigation: keep the local
  fold advisory (schedules, never authorizes); test that offering a not-ready job
  is safely rejected.
- **R2 At-least-once surfaced to users (medium; NC2/NC3).** Lease takeover re-runs
  a slow-but-alive job. Mitigation: the cockpit labels a takeover/re-run
  explicitly; docs state at-least-once + idempotent steps; memoization is opt-in,
  never a correctness crutch.
- **R3 Live-tail transport fragility (low; NC3).** SSE/long-poll can stall on
  flaky CI networks. Mitigation: fall back to from-seq polling on drop; the fold
  is resumable from any `seq`, so a reconnect never loses or double-counts.
- **R4 Contract skew with the platform (medium; NC0).** Mitigation: vendored copy
  + checksum guard (`TestVendoredCoordinationChecksum`) + the shared fold vectors;
  a major bump requires a coordinated re-vendor — neither repo bumps unilaterally.

## Out of scope

- The legacy `/v1/runs` v1 client — replaced wholesale, not maintained behind a
  flag (older pinned CLIs are covered only by the platform's transient cutover
  drain bridge, O1).
- Server internals (Durable Object, projections) — the client is
  implementation-agnostic by design.
