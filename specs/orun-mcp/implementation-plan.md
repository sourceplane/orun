# orun-mcp — Implementation Plan (UM0–UM3)

Status: In progress. PR-sized milestones; the spine is **UM0 → UM1 → UM2 →
UM3**. UM1 consumes `saas-mcp-server` MCP9's manifest export (lands first in
`orun-cloud`); UM3 cuts the release the MCP10 docs flip points at.

## UM0 — Provider composition — ✅ Done

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

## UM1 — Native platform reads + manifest parity — ✅ Done

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

## UM2 — Platform writes + rails — ✅ Done

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

## UM3 — Docs + release — ✅ Done (v2.24.0 released)


- `website/docs/cli/orun-mcp.md` rewritten for the unified surface (also
  fixes the stale 7-tool work-MCP text → 9); client config snippets
  (Claude Code, Cursor, VS Code) point at `orun mcp serve`.
- `website/docs/release-notes/v<next>.md` + `sidebars.js` entry.
- Version: next minor tag (v2.24.0 — v2.23.0 was cut by parallel work before the UM milestones landed) —
  tag push drives `release-oci.yaml` (GoReleaser + kiox provider stamp);
  verify the release workflow goes green and the artifacts/naming match
  `install.sh` expectations.

**Done when:** the release is published with the unified MCP, release notes
live on the website, and a fresh `install.sh` install serves 34 tools.

---

# Hardening phase (UM4–UM6) — from the 2026-07-12 field evaluation

Source: an external Claude Code test report (operator-run, macOS) + this
repo's own live probe. The server scored 9/10 on protocol/tool design and
2–4/10 on packaging/startup robustness. These milestones close that gap.

## UM4 — Truthful wire metadata + `orun mcp doctor` — 🗓️ Planned

- **Work tools gain wire annotations**: `workmcp.Tools()` populates
  `readOnlyHint`/`destructiveHint`/`idempotentHint` (reads true/false/true;
  the four mutators false/false/true — they are idempotency-shaped by WP-6).
  Today a strict client sees work reads as writes and over-prompts.
- **Dynamic counts everywhere**: `mcp --help`/serve Long text derive tool
  counts from the live rosters (the field report caught "9 tools" drift).
- **Wrong-backend hint**: a platform tool getting `not_found` on a known
  route appends "the backend URL does not look like an Orun Cloud API
  endpoint — check `orun cloud status` / `orun auth login`" (the legacy
  state backend 404s platform routes with an unhelpful NOT_FOUND today).
- **`orun mcp doctor`**: validates in order — binary has the mcp capability
  (self-version), auth state + token expiry (without printing secrets),
  workspace resolution chain, backend URL sanity (probes one platform and
  one work route), then prints the exact `claude mcp add orun -- <abs path>
  mcp serve` line (absolute path — the field report's P0 was a stale PATH
  binary silently lacking `mcp`).

**Done when:** `tools/list` annotations are complete for all 34 tools (a
mcpserve-level test requires every ToolDef to carry all three hints); help
text has no hardcoded counts; doctor exits non-zero with an actionable line
per failed check and zero with a copy-pastable registration line.

## UM5 — Never fail the handshake — 🗓️ Planned

- **Always answer `initialize`.** Missing/unresolvable auth no longer exits
  before the handshake (the field report's second blocker: exit 1 with zero
  stdout is indistinguishable from a crash). Instead the server starts with
  whatever mounts, plus an always-present **`auth_status`** tool that
  reports auth state, expiry, backend URL, and the exact login command.
- **Consistent degradation**: missing token and expired token behave the
  same — mounted-but-degraded, per-call isError carrying the login hint.
- **`--verbose` startup summary** on stderr: planes mounted, auth path
  tried (OIDC/ORUN_TOKEN/session), token expiry, workspace source.

**Done when:** with no credentials at all, an MCP client completes
initialize, lists ≥1 tool (`auth_status`), and gets actionable isError text
from any other call; expired vs absent tokens produce the same shape;
`--verbose` explains what mounted and why.

## UM6 — Resources & prompts parity (resolves U-D2) — 🗓️ Planned

- `mcpserve` gains `resources/list`/`resources/read` +
  `prompts/list`/`prompts/get` (hand-rolled, same loop — U-D1 stays open;
  the Go SDK remains a later option) and providers can advertise them.
- `platformmcp` serves the TS plane's 2 resource templates
  (`catalog://{workspace}/{entityKey}`, `runs://{workspace}/{project}/{runId}`)
  and 4 prompts (investigate_failed_run, access_review, usage_review,
  service_snapshot) — sourced from the vendored manifest's reserved
  `resources`/`prompts` stubs, parity-tested like the tools.
- Capabilities advertise `resources` + `prompts` only when a mounted
  provider supplies them.

**Done when:** a resources-capable client can read a catalog entity
overview and a run summary as markdown; prompts/get renders the four
workflows; the parity test covers the manifest's resources/prompts
sections; work-plane surface unchanged.
