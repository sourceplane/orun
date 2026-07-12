# orun-mcp — Risks & Open Questions

## Open decisions

### U-D1 — Protocol revision of the unified loop

The hand-rolled loop speaks `2024-11-05`; the TS plane speaks `2025-06-18`
via the official SDK. Clients negotiate, so this is not blocking — but the
two ends of the product now advertise different revisions. Options: keep the
hand-rolled loop (smallest change, taken for UM0–UM2) or adopt the official
`modelcontextprotocol/go-sdk` (new dependency, revisit when a feature needs
it). UM6 showed resources/prompts did NOT need it — the hand-rolled loop
grew them in ~100 lines — so the remaining trigger is the protocol revision
itself. Track with the sibling epic's D6. **Still open.**

### U-D2 — Resources & prompts parity (MCP4) — ✅ Resolved (UM6)

The TS plane serves 2 resource templates + 4 prompts; the hand-rolled loop
served tools only. Resolved WITHOUT the Go SDK (contra the "likely together
with U-D1" guess): UM6 added `resources/*` + `prompts/*` to the same loop,
providers opt in via optional `ResourceProvider`/`PromptProvider`
interfaces, `platformmcp` serves both surfaces from the manifest's reserved
stubs (parity-tested), and capabilities advertise them only when a mounted
provider supplies them. U-D1 stays open on its own merits.

## Risks

- **U-R1 — Dual-implementation drift.** The core risk of two tool planes.
  Mitigation is structural: the vendored manifest + parity test (design §4)
  make drift a CI failure, TS-first flow makes the ordering unambiguous, and
  the sibling epic's conformance suite pins the TS side. Residual: *behavioral*
  drift inside identical schemas (e.g. truncation limits, error text) —
  covered by mirroring the TS tests' assertions in Go where they encode
  behavior, and by keeping semantics in the manifest description fields.
- **U-R2 — serverInfo rename.** `orun-work` → `orun` is cosmetic to clients
  (config name dominates), but anything scripted against serverInfo.name
  would notice. Nothing in either repo asserts it besides workmcp's own
  tests (updated in UM0); release notes call it out.
- **U-R3 — The work plane's write posture under `--read-only`.** Platform
  writes filter out; work writes (mutator-shaped, WP-6) remain. That
  asymmetry is deliberate (the work MCP's safety model is sealed-brief +
  mutator-only, not read-only mode) but must be documented on the flag —
  done in UM2's help text and UM3's docs.
- **U-R4 — Release coupling.** UM3's release must not ship a half-mounted
  plane: the tag lands only after UM2 merges and the parity test is green
  against the then-current vendored manifest.
