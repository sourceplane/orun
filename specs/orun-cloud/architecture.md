# orun-cloud — Architecture Overview & Decisions

Status: Draft. The at-a-glance companion to `design.md` — the diagram, the
load-bearing decisions, and *why* each was taken, from the CLI's side. The
platform-side twin is
`multi-tenant-saas/specs/epics/saas-orun-platform/architecture.md`.

## The shape

```
┌─ orun CLI (this repo) ───────────────────┐      ┌─ Orun Cloud / OSS backend ────────────────────────┐
│ runner / compiler / cockpit              │      │ one public entry, org/project-scoped routes        │
│   └── statebackend.Backend  ──────────── HTTPS ──→  run coordination (claims, leases, heartbeats)   │
│   └── bridge.Source (status/logs/TUI)    │      │  CAS objects + logs (R2)                            │
│ internal/remotestate (client + retries)  │      │  CLI sessions + OIDC exchange + secret manager      │
│ internal/cliauth (loopback/device login) │      └────────────────────────────────────────────────────┘
│ local object store (sha256 digests) ───── same digests pushed to the CAS plane
└──────────────────────────────────────────┘
```

Local-first forever: every command works with no account. Cloud is additive and
every cloud failure has a defined degradation (`design.md` §7) — orun never
strands a user because a SaaS is down.

## The five load-bearing decisions

### 1. All cloud behavior stays behind two interfaces already drawn
`statebackend.Backend` (runner/state) and `bridge.Source` (cockpit/status/logs).
**Why:** the compiler, runner, and TUI gain zero HTTP knowledge; cloud runs
render through the *same* viewmodels as local ones. If a change can't be
expressed behind those interfaces, the interfaces get a deliberate revision —
never a bypass.

### 2. One contract, two servers — the client never special-cases which
Orun Cloud (multi-tenant) and the OSS `orun backend` (single-tenant,
`_local/_local` scope) implement one frozen contract; OC6 ships a conformance
suite run against both. **Why:** "switching is a URL change" is a public promise,
so branching on the server would betray it — and the suite hardens both servers.

### 3. Same digests as the local object store
The remote CAS plane is keyed by the same content addresses orun already
computes. **Why:** sync is "ask what's missing, push those blobs, move a head" —
no translation, repeat pushes near-free, and provenance preserved end to end.

### 4. Three token sources, real servers behind each
`SessionTokenSource` (humans, loopback/device + rotating refresh),
`OIDCTokenSource` (CI → exchange, no stored secret), `StaticTokenSource`
(`ORUN_TOKEN` / `sk_` keys). **Why:** these already exist in the codebase;
this spec changes their endpoints, not their shape. The "namespace" wording
retires in favor of the platform's org/project spine.

### 5. Secrets resolved at run time, never persisted locally
The runner resolves declared keys against a live job lease, injects them into
the step env, and registers each value with the log redactor before upload.
**Why:** values never touch local state, the object store, plan blobs, or
`~/.orun` — the blast radius is bounded to the live log stream the user's own
step produced.

## Degradation is the spec, not an afterthought

The normative degradation table (`design.md` §7) defines behavior for every
failure: no session, unlinked repo, backend down at start, backend lost
mid-run (buffer + spill + honest gap marker), secret resolve failure (fail the
job closed), catalog push failure (warn, exit 0), version mismatch (fail loud).
**Why:** a team relying on shared state must *notice* when it's not being
written — so there is no silent fallback to local.

## What to review first

The cross-repo seam: the contract is **vendored** into this repo and CI diffs it
against the platform's source (OC0), so drift fails the build. The riskiest
client-side item is heartbeat liveness on CPU-saturated runners
(`risks-and-open-questions.md` R2); the conformance suite (OC6) is the
contract's executable form and the best single artifact to scrutinize.
