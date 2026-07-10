# Spec: orun-mcp — one binary, one MCP

**Unify the ecosystem's local MCP surface in the orun binary.** Today the
binary serves the **work MCP** (`orun mcp serve` — `internal/workmcp`, 9
tools over the work plane), while the **platform MCP** (25 tools over the
Orun Cloud public API: catalog, runs/logs, audit, events, access, usage,
billing, config, webhooks) ships its *local* transport inside the node
`orun-cloud` CLI. That is the wrong distribution: **the orun binary is the
official client people actually install**; the node CLI is a reference
implementation. This spec reimplements the platform tool plane natively in
Go and mounts it into the one `orun mcp serve` — an agent connects to a
single local MCP and gets work *and* platform tools, gated by what the
context supports.

## Status

| Field | Value |
|-------|-------|
| Status | **In progress** — UM0–UM3 |
| Cluster | **UM** (unified MCP — pairs the `saas-mcp-server` epic's unification phase, MCP9–MCP10, in `orun-cloud`) |
| Owner(s) | `internal/workmcp` (transport refactor) · `internal/platformmcp` (new) · `internal/remotestate` (public-API wire methods) · `cmd/orun/mcp.go` · `specs/orun-cloud/vendored/` (tool-manifest vendor) · `website/docs/cli/orun-mcp.md` · release machinery |
| Target branch | `claude/orun-cloud-mcp-server-h95b57` (PRs merged incrementally) |
| Builds on | `internal/remotestate` (`Client` + `TokenSource` — bearer/refresh/retry/error-decode come free), `internal/cliauth` (OP1 session), `specs/orun-work/` WP5 (the work MCP + its forbidden-tool invariants, unchanged), `specs/orun-cloud/` OC0 (the vendored-contract + CI-diff pattern this spec reuses for the tool manifest), `orun-cloud/specs/epics/saas-mcp-server/` MCP0–MCP8 (the shipped platform tool plane whose contract this implements) |
| Decisions locked | (1) **One local server.** `orun mcp serve` is the ecosystem's local MCP: one stdio JSON-RPC loop, serverInfo name `orun`, composing tool providers — work tools when workspace scope resolves, platform tools when cloud auth resolves. The two-endpoint mount in `orun-agents-live` design §4.4 collapses to one endpoint. (2) **The TS tool plane stays the contract source of truth.** `orun-cloud/packages/mcp` exports a machine-readable **tool manifest** (names, input schemas, annotations); orun vendors it under `specs/orun-cloud/vendored/` (OC0 pattern, CI-diffed) and a Go parity test pins the native roster to it. Tool names, schemas, and semantics are identical across implementations — docs and prompts are portable. (3) **The remote transport is untouched.** `apps/mcp-worker` (Streamable HTTP) keeps serving the TS plane; this spec moves only the *local* distribution. (4) **The node CLI demotes to reference implementation.** `orun-cloud mcp` keeps working; all docs point at `orun mcp serve`. (5) **Domain boundaries are unchanged.** Work tools never grow platform vocabulary and vice versa (saas-mcp-server locked decision 8); the WP-3/WP-10 forbidden-tool assertions (no status/pin/lifecycle) extend over the merged roster. (6) **Client-not-service holds in Go.** Platform tools call only the public API via `remotestate` with the caller's credential — RBAC, rate limits, idempotency, audit (`x-client-surface: mcp`), and metering apply unchanged. |

## Thesis

A developer's agent should need exactly one local MCP for everything orun:
"who owns billing-worker?", "why did the last prod run fail?", *and* "what's
in flight on the work plane?" — one `claude mcp add orun -- orun mcp serve`.
The binary already owns the auth session (OP1), the backend client, and the
work MCP loop; the platform tools are a thin native layer over machinery
that exists. What keeps two implementations honest is not discipline but a
**contract artifact**: the manifest exported from the TS plane (which still
powers the hosted remote server) is vendored here and enforced by a parity
test, exactly like the OC0 wire contract. One contract, two implementations,
one product surface.

## Read order

1. This README — status + thesis + milestones.
2. [`design.md`](./design.md) — provider composition, the native platform
   plane, manifest parity, auth/scope resolution, gating semantics.
3. [`implementation-plan.md`](./implementation-plan.md) — UM0–UM3 with
   "done when".
4. [`risks-and-open-questions.md`](./risks-and-open-questions.md).
5. As-built: [`IMPLEMENTATION-STATUS.md`](./IMPLEMENTATION-STATUS.md).

## Milestones at a glance

| ID | Milestone | Status |
|----|-----------|--------|
| UM0 | Provider composition: factor the stdio JSON-RPC loop out of `workmcp` into a provider-composing server; serverInfo → `orun`; work tools unchanged (9); merged-roster invariant tests | 🗓️ Planned |
| UM1 | Native platform reads: `internal/platformmcp` (19 read tools) over `remotestate` public-API methods; vendored tool manifest + parity test (needs `saas-mcp-server` MCP9's manifest export) | 🗓️ Planned |
| UM2 | Platform writes + rails: the 6 write tools, per-attempt `Idempotency-Key`, `x-client-surface: mcp` on every call, `--read-only`, entitlement gate (`feature.mcp_server`, fail-open) | 🗓️ Planned |
| UM3 | Docs + release: `website/docs/cli/orun-mcp.md` rewrite (unified surface; fixes the stale 7-tool text), release-notes page, version tag → GoReleaser/kiox release | 🗓️ Planned |

## Scope boundary

| In scope | Out of scope |
|----------|--------------|
| The unified local server (provider composition over one stdio loop); the native Go platform tool plane and its `remotestate` wire methods; manifest vendor + parity test; `--read-only` and context-dependent mounting; website CLI doc; the release | The work MCP's tool semantics (WP5 — consumed as a provider, unchanged); the remote Streamable-HTTP transport (`apps/mcp-worker`, stays TS); the TS tool plane itself (stays, as contract source + remote implementation); new tool domains (the 25-tool budget and decision-8 boundaries are `saas-mcp-server`'s to evolve); agent-driver wiring (`orun-agents-live` — it consumes the one endpoint) |

## Cross-repo pairing

| This repo (UM) | `orun-cloud` (`saas-mcp-server` phase 2) |
|---|---|
| UM1 vendors + parity-tests the manifest | MCP9 exports the tool manifest from `packages/mcp` (single source of truth, freshness-tested) |
| UM3 ships the release the docs point at | MCP10 flips console Connect page, web-docs, and CLI README to `orun mcp serve`; node CLI labeled reference implementation |
