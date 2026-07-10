# orun-mcp — Risks & Open Questions

## Open decisions

### U-D1 — Protocol revision of the unified loop

The hand-rolled loop speaks `2024-11-05`; the TS plane speaks `2025-06-18`
via the official SDK. Clients negotiate, so this is not blocking — but the
two ends of the product now advertise different revisions. Options: keep the
hand-rolled loop (smallest change, taken for UM0–UM2) or adopt the official
`modelcontextprotocol/go-sdk` (new dependency, revisit when a feature needs
it — resources/prompts parity, see U-D2). Track with the sibling epic's D6.

### U-D2 — Resources & prompts parity (MCP4)

The TS plane serves 2 resource templates + 4 prompts. The hand-rolled loop
serves tools only. Ship the unified server tools-first (UM0–UM3) and add
resources/prompts parity as a follow-up — likely together with U-D1, since
the Go SDK gives both cheaply. The manifest format reserves room for them.

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
