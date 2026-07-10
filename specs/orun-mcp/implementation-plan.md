# orun-mcp — Implementation Plan (UM0–UM3)

Status: In progress. PR-sized milestones; the spine is **UM0 → UM1 → UM2 →
UM3**. UM1 consumes `saas-mcp-server` MCP9's manifest export (lands first in
`orun-cloud`); UM3 cuts the release the MCP10 docs flip points at.

## UM0 — Provider composition — 🗓️ Planned

- Extract the stdio JSON-RPC loop from `internal/workmcp/server.go` into
  `internal/mcpserve` (scanner limits, initialize/ping/tools handling,
  notification silence, error codes — moved verbatim); define `ToolProvider`
  and a composing `Server`.
- serverInfo → `{name: "orun", version: <binary version>}`.
- `workmcp` implements `ToolProvider`; `cmd/orun/mcp.go` builds the
  composite (work provider only, until UM1); behavior otherwise identical.
- Tests: existing `workmcp` suite passes unchanged; new `mcpserve` tests for
  composition (merged list, routed call, name-uniqueness guard); the
  forbidden-tool assertion runs over the merged roster.
- Wire `./internal/mcpserve ./internal/workmcp` into the CI Go-test gate
  (the `test-state-redesign` Makefile target or a sibling step — follow
  whichever the repo prefers).

**Done when:** `orun mcp serve` behaves exactly as before for work tools
(9, same schemas), reports serverInfo `orun`, and the loop lives in
`mcpserve` with composition tests green.

## UM1 — Native platform reads + manifest parity — 🗓️ Planned

- `internal/remotestate`: public-API wire methods for the read families
  (org/project lists, catalog entities + docs, runs/jobs/logs, audit,
  events, security events, membership/teams/effective-access, usage/quotas,
  billing summary/entitlements, config, webhook endpoints/deliveries) —
  `work.go` pattern, no new client machinery.
- `internal/platformmcp`: the 19 read tools over a `PlatformAPI` seam;
  workspace-default semantics; 64 KiB truncation; error mapping preserving
  platform codes.
- Vendor `mcp-tool-manifest.json` (from MCP9) under
  `specs/orun-cloud/vendored/`; parity test pinning roster + normalized
  schemas + annotations.
- Mount the platform provider in `cmd/orun/mcp.go` when auth resolves; add
  `orun mcp tools`; pointer note in `orun-agents-live/design.md` §4.4.

**Done when:** against a stubbed `PlatformAPI`, all 19 tools round-trip with
outputs matching the TS plane's shape; the parity test fails on any name/
schema/annotation drift from the vendored manifest; `orun mcp serve` lists
28 tools (9 work + 19 platform) under one initialize.

## UM2 — Platform writes + rails — 🗓️ Planned

- The 6 write tools; per-attempt `Idempotency-Key` (+ supplied-key
  passthrough, ≤ 255 printable ASCII); `x-client-surface: mcp` on every
  platform call; `member_invite` strips the accept token.
- `--read-only` filters writes from `tools/list`; default serves all 34.
- Entitlement gate: lazy per-workspace `feature.mcp_server` check, 60s
  cache, fail-open, `entitlement_required` on explicit denial.
- Parity test now covers the full 25; forbidden-tool sweep still green over
  34.

**Done when:** write tools match the vendored manifest; two calls mint two
distinct auto keys and a supplied key reaches the wire verbatim; a denied
workspace gets the platform error; `--read-only` lists 28 (9 work + 19
platform reads — work writes are mutator-shaped by WP-6 and stay, as the
work plane's own read-only story is WP5's, not this spec's).

## UM3 — Docs + release — 🗓️ Planned

- `website/docs/cli/orun-mcp.md` rewritten for the unified surface (also
  fixes the stale 7-tool work-MCP text → 9); client config snippets
  (Claude Code, Cursor, VS Code) point at `orun mcp serve`.
- `website/docs/release-notes/v<next>.md` + `sidebars.js` entry.
- Version: next minor tag (`v2.23.0` from v2.22.0 unless main has moved) —
  tag push drives `release-oci.yaml` (GoReleaser + kiox provider stamp);
  verify the release workflow goes green and the artifacts/naming match
  `install.sh` expectations.

**Done when:** the release is published with the unified MCP, release notes
live on the website, and a fresh `install.sh` install serves 34 tools.
