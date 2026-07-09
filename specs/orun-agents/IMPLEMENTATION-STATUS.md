# orun-agents — Implementation Status (as-built)

The as-built record, kept distinct from the design/plan docs. One row per
milestone; the `As-built` cell names exactly what landed.

| Milestone | Status | As-built |
|-----------|--------|----------|
| AG0 — Object kinds + foundation | ✅ Shipped | `internal/nodes/agents.go`: `AgentTypeSnapshot` (tree: `agent-type.json` + `body.md` [+ `base-literacy.md`], doc-blob spine, mandatory owner), `AgentBrief`, `AgentSessionSegment` (prev-chained, closed 11-kind event vocabulary — no status kind exists), `AgentSessionSnapshot` (outcome carries no timestamps/cost — annotation stays out of identity); kinds registered in `kinds.go`; pure ids (`AgentTypeID`/`AgentBriefID`/`AgentSessionSegmentID`/`AgentSessionID`) in `ids.go` via the `hashStore` cross-check. `internal/agent`: package skeleton + embedded versioned **base literacy** (`literacy.md`, `LiteracyName`/`LiteracyVersion`, `SealLiteracy` idempotent under `refs/agents/literacy/v1`). CLI: `orun agent context [--seal]` + `orun agent context id` (`cmd/orun/command_agent.go`). Tests: determinism (map-order/whitespace invariance, persona/model edits change identity), pure-id ↔ assembled-id parity, validation rejections (ownerless, empty persona, unknown event kind incl. `status_asserted`, non-monotonic seq), literacy seal idempotency. |
| AG1 — Authoring + seal/pull + catalog projection | 🗓️ Not started | — |
| AG2 — The delegation runtime | 🗓️ Not started | — |
| AG3 — TUI Agent mode | 🗓️ Not started | — |
| AG4 — Driver conformance + session seal | 🗓️ Not started | — |
