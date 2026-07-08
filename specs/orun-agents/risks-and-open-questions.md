# orun-agents — Risks & Open Questions

The runtime-side gate items and decisions. Cloud/live gates (Daytona, session
identity, entitlement) live in the paired epic
`orun-cloud/specs/epics/saas-agents/risks-and-open-questions.md`.

## ⛔ Still open — confirm before building

| # | Question | Default lean |
|---|----------|--------------|
| OA1 | **Snapshot seal timing vs. WP4.** AG1's `AgentTypeSnapshot` seal reuses the same unbuilt `nodes`+`nodewriter`+`objremote` plumbing as `orun-work`'s `SpecSnapshot`/`spec pull`. Co-land them, or ship AG1's entity-kind projection first and defer the snapshot half until WP4? | Ship the **entity-kind projection immediately** (it needs no new plumbing — agent types are discoverable now), and **co-develop the snapshot seal with WP4** so both seals share one implementation. Dispatch-pin (`@hash`) waits for the shared seal. |
| OA2 | **Base-literacy authorship & size.** Who owns the base-literacy content, and how big is it (it's in every brief's context budget)? | A curated, versioned doc in-repo, embedded at build; kept tight (the invariants + tool surface + object-model primer), measured against `contextBudget`. Treat it like a public API — changes are reviewed, versioned, and changelog'd. |
| OA3 | **Local identity for writes.** Locally the agent acts as *you*. Should local `contract_propose`/`task_assign` be allowed, or is dispatch-quality mutation cloud-only (where the service principal + RBAC apply)? | Local runs default **read + comment + PR**; `contract_propose`/`assign` are `ask` locally and only auto-allowed under the cloud principal. Keeps a laptop run from silently mutating shared work intent. |
| OA4 | **Driver protocol surface.** How much of a harness's capability does the normalized stdio protocol expose (sub-agents, parallel tool calls, files-changed streaming)? Too thin and drivers lose power; too thick and "any binary" gets hard. | Start with the lifecycle the session log needs (events/steer/approve/artifact/stop); extend the protocol only when a second real driver forces it — and make each extension a conformance-oracle addition, not a per-driver special case. |
| OA5 | **Persona provenance / sharing.** Agent-type personas will be shared across workspaces/orgs. Is there a public/registry notion, and how does a shared persona rebind `owner`/`mayAffect`/`secrets`? | A shared persona is just a body blob; adopting it mints a local `AgentTypeSnapshot` with the adopter's own capability frontmatter. No cross-tenant capability travels — only character. A registry is a later, additive idea. |

## ✅ Decisions made

| # | Decision | Resolution |
|---|----------|------------|
| D1 | Runtime location | In the binary (`internal/agent`), local-first; the cloud runs the same binary in a sandbox. Obeys orun's constitution. |
| D2 | Agent types as objects | `agents/*.md` → `AgentTypeSnapshot`: frontmatter → typed capability envelope, persona → content-addressed body blob (the doc-blob spine). Catalog-projected, sync/pull/GC via the existing object graph. |
| D3 | Delegation model | The coding agent is an `AgentDriver` (executor) behind a seam; Claude Code first; a conformance oracle makes "any binary" real. |
| D4 | Base literacy | Versioned, ships with the binary; agent types `extends` it; personas never restate orun mechanics. |
| D5 | Sealed runs | `AgentBrief` + `AgentSessionSnapshot` are content-addressed; runs are reproducible and `replay`-able; provenance extends the source→catalog→spec→task chain through the agent. |
| D6 | Honesty | No status-write surface at the runtime; inherited from the work plane's MCP (no such tool exists). |
| D7 | Tool-policy enforcement | Runtime-enforced between driver and MCP; layered by context (local = your grants + guardrails; cloud = service principal + RBAC re-enforcement). |
| D8 | Cloud seam | `orun agent serve` (this spec) ↔ session token + per-session DO relay (cloud epic); sandbox dials out, cloud never reaches in. |

## Risks

| Risk | Mitigation |
|------|------------|
| **Seal/pull is unbuilt (WP4 dependency).** AG1's snapshot half can't ship before the shared seal plumbing exists. | The entity-kind projection (discoverability) needs none of it and ships first; the snapshot half is scoped to co-develop with WP4, not to block AG2/AG3 (which run from the local view). |
| **Base literacy rot / bloat.** A stale or bloated literacy silently degrades every agent. | Versioned + reviewed like an API; measured against context budget; `usesLiteracy` catalog edge makes "which version ran?" a query; an AG9 lint flags personas that duplicate literacy. |
| **Driver lock-in to Claude Code.** The seam could accrete Claude-Code-shaped assumptions. | The conformance oracle (a stub second driver passing the full suite) is an AG4 exit criterion — the anti-lock-in gate. Protocol extensions must pass the oracle, not special-case a driver. |
| **Identity confusion local vs. cloud.** Same `.md`, different principal — easy to reason about wrongly. | Documented explicitly (design §5); local defaults to read+comment+PR (OA3); the session snapshot records `principal`, so every run says who it acted as. |
| **Object-graph growth.** Sessions + transcripts are content; a busy workspace accretes objects. | Transcript chunks are content-addressed (dedup) and GC'd by reachability like everything else; retention policy on session refs is a cloud-epic knob (AG-side A5); segments keep Postgres/rows bounded. |

## Non-blocking notes

- `agents/orchestrator.md` becomes the first real `AgentTypeSnapshot` at AG1 —
  the dogfood conversion is the AG1 acceptance demo.
- `orun agent replay` is also a debugging tool: a failed run is fully
  reconstructable from content, which makes agent-behavior regressions
  bisectable.
- The AffectedSet freeze (brief §4.2) is independently useful — a sealed
  "what a change touches" object other tools (review, CI) could reuse.
