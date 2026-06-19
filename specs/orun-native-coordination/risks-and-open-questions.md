# orun-native-coordination — Risks & Open Questions

Status: Draft. Client-side risks/opens behind the locked bet (append/fold
coordination, content-addressed results, local-first, implementation-agnostic).
Contract-level decisions (memoization scope, `jobInputHash` definition, drain
window) are owned platform-side in `saas-orun-backend-merge`
`risks-and-open-questions.md` (D1–D5) and are not re-litigated here.

## Open questions

### Q1 — Local fold vs shared `reduce()` drift (owner NC0)

The platform owns `reduce()` in TypeScript; the CLI ports it to Go. Two
implementations of one reduction can drift. **Leaning:** a shared **golden-vector
suite** (events → expected fold) checked into both repos and run in both CIs is
the source of truth; the prose contract is secondary. Revisit a codegen path only
if drift recurs.

### Q2 — `jobInputHash` parity with the server (owner NC1)

The client computes the hash that the server memoizes against; a mismatch means
silent cache misses (or, worse, a false hit if the hash is too coarse). **Leaning:**
the hash is defined normatively in the contract (D2) and verified by a shared
vector; the client computes, the server only stores/looks-up, so the client is
the single hasher — no two-sided computation to disagree.

### Q3 — Offline → cloud reconciliation semantics (owner NC3)

When a run executes offline then syncs, the local appends must merge into the
cloud stream without conflict. **Leaning:** offline runs are a distinct `runId`
(or sync-as-import producing a `run-record`), not a live-claim race against cloud
runners; idempotent appends make re-sync safe. Open: whether a half-offline /
half-cloud run is allowed (lean **no** — a run is either local-authoritative or
cloud-authoritative for its lifetime).

## Engineering risks

### R1 — Losing the local dependency-check authority (severity: medium; owner NC2)

The client no longer gates deps itself — the server's `:claim` does. A bug in the
local scheduler can only cause wasted claim attempts (rejected `deps_not_ready`),
never a deps-gate escape, because the server is authoritative. Mitigation: keep
the local fold purely advisory (it *schedules*, never *authorizes*); test that a
scheduler that offers a not-ready job is safely rejected.

### R2 — At-least-once surfaced to users (severity: medium; owner NC2/NC3)

Lease takeover re-runs a slow-but-alive job; users see a job "run twice." This is
inherent (and unchanged from the row model) but now visible. Mitigation: the
cockpit labels a takeover/re-run explicitly; docs state at-least-once + idempotent
steps; memoization is opt-in and not a correctness crutch.

### R3 — Live-tail transport fragility (severity: low; owner NC3)

SSE/long-poll over flaky CI networks can stall a `--follow`. Mitigation: fall back
to from-seq polling on stream drop; the fold is resumable from any `seq`, so a
reconnect never loses or double-counts events.

### R4 — Contract skew with the platform (severity: medium; owner NC0)

The CLI and server can drift the contract apart. Mitigation: vendored copy +
checksum guard (this repo) and the shared fold golden vectors; a major bump
requires a coordinated re-vendor — neither repo bumps unilaterally (mirrors the
`specs/orun-cloud` discipline).

## Out of scope

- The legacy `/v1/runs` v1 client — replaced wholesale, not maintained behind a
  flag (older pinned CLIs are covered only by the platform's transient cutover
  drain bridge).
- Server internals (Durable Object, projections) — the client is
  implementation-agnostic by design.
