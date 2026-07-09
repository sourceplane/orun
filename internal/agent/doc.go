// Package agent is the orun agent runtime (specs/orun-agents/design.md): the
// delegation loop that turns a frozen, content-addressed brief into a PR by
// driving a coding agent behind the AgentDriver seam, with the orun MCP as
// hands and an append-only, sealable session event log as the record.
//
// AG0 ships the substrate only: the base-literacy module (literacy.go) and the
// object kinds it seals through (internal/nodes/agents.go). The loop, the
// brief assembler, the driver seam, and the session log land in AG2; the TUI
// Agent mode in AG3; conformance + replay in AG4. The cloud control plane
// (sandboxes, session identity, the DO relay) is orun-cloud's half of cluster
// AG and never lives in this package — the runtime is local-first, and a cloud
// session is this same binary run under `orun agent serve`.
package agent
