# orun-agents-live — Implementation Plan (AL0–AL5, the orun half)

The orun-owned milestones. The cloud half (AL6–AL9: the DO relay as attach
server, the console head, handoff + notifications, suspend/metering) lives in
`orun-cloud/specs/epics/saas-agents-live/implementation-plan.md`. Design refs
are to `design.md` (§) and `attach-protocol.md` (P§) in this directory.

Every milestone is mergeable alone and lands with tests green; the stub
driver remains the CI workhorse throughout (the Claude Code driver adds
recorded fixtures, never a CI dependency on a vendor).

---

## AL0 — The attach plane (protocol + runtime inputs) — 🗓️ Planned

The wire and the body-side core, no UI.

- `internal/agent/attach`: frame types (P§2–§3), NDJSON encode/decode, the
  server core (multiplex heads, replay-from-seq, per-head bounded queues,
  delta drop-first backpressure P§6.2), the in-process client (P§6.1).
- Runtime inputs: `RunOptions` gains an input seam the attach server drives —
  steer enqueue → `message_user` at injection, verdict → `approval_resolved`
  + unblock (replacing the synchronous `Approve` callback with a
  request-id-keyed pending set), interrupt → driver control + `harness_event`
  (§2.1, §2.2). `EventDelta` added observe-only (P§4).
- Golden fixtures (P§7) authored and round-tripped; fixture copies staged for
  the cloud repo.

**Done when:** a stub-driver run accepts steer/verdict/interrupt through the
in-process client; the log contains attributed `message_user` /
`approval_resolved` in arrival order; replay renders the conversation; all
fixture sequences round-trip; `ask` tools resolve via a live verdict instead
of auto-deny (auto-deny remains the no-head-timeout fallback).

## AL1 — The Claude Code driver + MCP wiring — 🗓️ Planned

- `driver/claudecode.go` (§4): bidirectional stream-JSON, the event mapping
  table (§4.2), permission bridge → `EventApproval`/verdict return, steer to
  stdin, interrupt via control request, harness-session capture, version
  handshake + `orun agent doctor`.
- `internal/agent/mcp.go` (§4.4): write the driver MCP config — orun MCP
  (`internal/workmcp`, stdio) always, platform MCP when serving; tool policy
  filters at write time (deny = absent).
- `fake-claude` fixture harness + recorded captures (§4.5); live smoke gated
  behind `ORUN_LIVE_DRIVER_SMOKE=1`.

**Done when:** `driver.CheckConformance` passes claude-code unmodified; the
fixture suite covers every mapping row incl. approval block/unblock and
mid-turn steer; `orun agent run --type implementer --task <key> --driver
claude-code` completes a real task end-to-end in live smoke, sealed and
replayable with the conversation.

## AL2 — The session host — 🗓️ Planned

- Socket serving in the body (P§6.2), the live registry + stale sweep
  (§3.2), `--detach` process model (§3.1).
- CLI: `orun agent ps` / `attach <id>` (local path) / `kill [--force]`
  (§3.3); `orun agent run` hosts while rendering inline.

**Done when:** a `--detach` run survives its launcher's exit; two terminals
attach concurrently and both steer (order preserved in the log); `ps` lists
truthfully after a `kill -9` (stale sweep); the §6.3 story's *local* half —
launch, detach, re-attach, approve, complete — passes as an e2e test against
the stub.

## AL3 — The TUI head — 🗓️ Planned

`ModeAgent` elevated in place (§5): sessions-first sidebar with attention
badges, conversation stage (turns, delta streaming, tool cards, sticky
approval cards, activity line, checklist), always-on composer
(steer/interrupt/detach keys), inspector proof pane, launch flow with brief
preview (§5.5), `orun agent` bare + `orun agent attach` opening straight into
the session. Frame-stability invariants hold (the `live_scroll_test.go`
discipline: every frame exactly terminal-sized under interleaved stream +
input).

**Done when:** the five §5 sentences each have a test: streamed turns render
live; typing mid-turn queues and injects; tool cards fold/expand; a pending
approval cannot scroll off; detach + reattach loses nothing. Launch→PR
against the stub runs entirely inside the TUI.

## AL4 — Remote attach — 🗓️ Planned (pairs with cloud AL6)

- `orun agent serve` (§6.1): the loop + attach plane over the dial-out
  binding (P§6.3) — batched event frames to ingest, delta stream, input
  long-poll, heartbeat/token as shipped; replaces `bootstrapScript`.
- `orun agent attach as_…` (§6.2): remote resolution, `cliauth` bearer, SSE +
  input POSTs, same TUI head.
- Developed against a local fake relay speaking the shared fixtures; live
  against cloud AL6 when it lands.

**Done when:** the fake-relay e2e passes the full §6.3 story; against a real
Daytona session (cloud AL6 live), the TUI attaches, steers, approves, and the
console shows the same attributed events; the cloud repo deletes
`bootstrapScript` in favor of `orun agent serve`.

## AL5 — Hardening + evals — 🗓️ Planned

Verdict races across heads (fixture P§7), steer flood + delta backpressure,
interrupt-during-tool-call semantics pinned, replay-with-conversation goldens,
resume-mid-delta correctness, socket permissions audit, `orun agent doctor`,
docs (`website/` agent pages + the literacy addendum teaching agents that
humans may speak mid-run).

**Done when:** the conformance suite runs both drivers through the full
interactive lifecycle; a soak (long session, repeated attach/detach, forced
disconnects) holds the frame and log invariants; docs published.

---

## Sequencing note

**AL0 → AL1 → AL2 → AL3** is strictly local and human-independent — the
whole interactive product works on a laptop against the stub after AL3, and
against Claude Code after AL1. **AL4** is the only cloud-coupled milestone
and its protocol surface is frozen at AL0 (the fixtures), so cloud AL6 builds
in parallel against the same files. AL5 overlaps the tail. The first
demoable cut is **AL0+AL2+AL3** (interactive TUI over the stub — no key, no
vendor); the first *real* cut adds AL1; the headline cut is AL4's handoff
story.
