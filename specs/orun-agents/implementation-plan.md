# orun-agents — Implementation Plan (AG0–AG4, the runtime)

The orun-owned runtime milestones. The cloud control plane (AG5–AG11) lives in
`orun-cloud/specs/epics/saas-agents/implementation-plan.md`. Design refs are to
[`design.md`](./design.md) / [`data-model.md`](./data-model.md).

## AG0 — Object kinds + foundation — ✅ Shipped (`internal/nodes/agents.go`, `internal/agent`, `orun agent context`)

The content substrate + the `internal/agent` skeleton. No delegation yet.

- `internal/nodes`: `KindAgentType`, `KindAgentSession`, `KindAgentBrief`,
  `KindAgentSessionSegment` in `kinds.go`; schemas in `model.go`; `Validate()`
  in `validate.go` (assert kind, key grammar, `validID(bodyRef)`, **exclude**
  annotation fields from identity — the identity-purity invariant);
  `Assemble*` in `assemble.go` (doc-blob pattern for the persona); pure ids in
  `ids.go`.
- `internal/agent/literacy.go`: the embedded, versioned **base literacy**
  blob; `orun agent context [--seal]`.
- `internal/agent` package skeleton (runtime/brief/mcp/session stubs, compiling,
  unwired).
- Refs `refs/agents/…` reserved (`data-model.md` §5).
- Owner: `internal/nodes` + `internal/agent`.
- **Done when:** the four kinds round-trip through encode/validate/assemble with
  canonical-JSON determinism tests (key-order/whitespace invariance); pure ids
  match assembled ids (the `hashStore` cross-check); base literacy seals and
  `orun agent context` prints it; `go build ./...` green with the skeleton.

## AG1 — Authoring + seal/pull + catalog projection — 🗓️ Planned

Agent `.md` files become first-class objects — the "agent md in the object
store" deliverable.

- `internal/agenttype/load.go`: parse `agents/*.md` (frontmatter + body) →
  `AgentTypeSnapshot`; `orun agent lint agents/` (schema, closed keys, owner
  resolution).
- **Entity-kind projection (ships immediately):** `EntityKindAgentType` in
  `assemble.go`; resolver emits agent types via `DeclaredEntities` with the
  persona in `PendingDocs`; `objcatalog.readEntities` projects them with no
  reader change (`data-model.md` §6, §8).
- **Snapshot seal + pull (shares WP4 plumbing):** `nodewriter.WriteAgentType`;
  `orun agent import agents/` (mirrors `orun work import specs/`);
  `orun agent pull <type>@<hash>` via `objremote.Pull` (set-difference; sync +
  GC free).
- Catalog edges: `ownedBy`, `mayAffect` (→ components), `usesLiteracy`.
- Owner: `internal/agenttype` + `internal/nodes` + `internal/objcatalog` +
  `cmd/orun`.
- **Done when:** `agents/orchestrator.md` (and a new `implementer.md`) import,
  seal, and appear in `orun catalog` as `AgentType` entities with owner +
  `mayAffect` edges; `orun agent pull <type>@<hash>` materializes the closure
  offline; two files differing only in key order seal to the same id.

## AG2 — The delegation runtime — 🗓️ Planned

The loop: brief in, PR out.

- `driver/driver.go` (interface + registry + stdio protocol) +
  `driver/claudecode.go` (headless stream-JSON, MCP config, permission→approval
  mapping).
- `brief.go`: assemble + seal `AgentBrief` (spec pull + `catalog affected`
  frozen as `AffectedSet` + rendered instructions); `orun agent run --dry-run`
  prints brief + id.
- `mcp.go`: launch orun MCP (stdio) + write driver MCP config; tool-policy
  enforcement (`allow`/`ask`/`deny`), `ask` → approval block.
- `session.go`: append-only session event log (closed vocabulary) +
  `AgentSessionSegment` sealing with the `prev` chain; transcript chunks to
  content-addressed blobs.
- `orun agent run --task <key>`: end-to-end headless run on a task-keyed
  branch, ending at an open PR; `--json` output via cockpit viewmodels.
- Owner: `internal/agent` + `cmd/orun`.
- **Done when:** `orun agent run --task <fixture>` against a fixture repo
  produces a task-keyed branch + PR using only MCP tools + git; a `deny` tool
  is unreachable and an `ask` tool blocks for a verdict, both logged; the
  session log seals to a chained segment set that replays the transcript.

## AG3 — TUI Agent mode — 🗓️ Planned

The local-first product surface.

- `internal/tui`: `ModeAgent` (6th mode, key `6`) + `views/agent.go` —
  sessions/types sidebar, live transcript stage with the sealed brief + live
  affected subgraph, inspector (tool call/approval/contract/cost/PR),
  approvals as actionable cards; keymap + mode-capability wiring
  (searchable/inspector).
- `orun agent` opens the TUI directly into Agent mode; fully local
  (`.orun/` + env model key), no cloud calls.
- Owner: `internal/tui` + `cmd/orun`.
- **Done when:** `orun agent` runs an interactive session end-to-end in the
  terminal with no cloud — transcript streams, an `ask` approval is answered by
  keystroke, the PR link renders; the mode passes the existing TUI mode-
  capability + snapshot tests.

## AG4 — Driver conformance + session seal — 🗓️ Planned

Make "any binary" real and runs replayable.

- `driver/conformance.go`: the documented stdio protocol + an oracle suite; a
  stub driver passes the full lifecycle (launch/stream/steer/approve/artifact/
  stop) unchanged.
- Session seal: `AgentSessionSnapshot` (agent-type@hash + brief@hash + catalog
  pin + segment/transcript refs + outcome) on terminal state;
  `orun agent replay <session>@<hash>` re-renders offline, byte-identical.
- Provenance test: from a fixture `revision_live`, walk back to the exact
  agent-type and brief (`data-model.md` §7).
- Owner: `internal/agent/driver` + `internal/agent` + `cmd/orun`.
- **Done when:** the stub second driver passes conformance with no
  `internal/agent` change; a finished session seals and `orun agent replay`
  reproduces the transcript from content alone; the provenance walk resolves
  end to end.

## Sequencing note

**AG0 → AG1 → AG2 → AG3 → AG4**, all human-independent: a local agent runs
against a local `.orun/` with the model key from the environment, so the whole
runtime is buildable and demoable with no cloud and no vendor. AG1's snapshot
half shares its seal/pull plumbing with `orun-work` WP4 — co-develop them so
the `SpecSnapshot` and `AgentTypeSnapshot` seals land on one implementation.
AG2 needs the orun MCP (WP5) for the *four write tools*; until WP5, the runtime
wires read-only catalog/affected MCP tools and the driver commits/pushes
directly (the PR is still the artifact) — dispatch-quality write tooling lands
with WP5. The cloud-attach entrypoint `orun agent serve` is specified here but
*ships* with cloud AG6 (it needs the session token + DO relay); its protocol
is testable against a local stub before then.
