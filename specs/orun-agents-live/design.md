# orun-agents-live — Design (bodies, heads, and the attach plane)

Status: Draft (normative once AL0 lands)

Written against repo reality as of 2026-07-10: the agent runtime is shipped
(`specs/orun-agents/` AG0–AG4 — `internal/agent`, `internal/agent/driver`,
`internal/agenttype`, `orun agent run/replay/import/lint/show/context`); the
only registered driver is the deterministic `Stub`; `driver.IO.Steer` is
created and never written (`internal/agent/runtime.go:80`); `RunOptions.Approve`
is a synchronous callback never set by any caller (all `ask` tools auto-deny);
the TUI `ModeAgent` (key `3`) renders agent types and a transcript pane whose
`StartStream` no producer calls (`internal/tui/views/agent.go`); the orun MCP
exists (`internal/workmcp`) but no MCP config is ever written for a driver;
`orun agent serve` does not exist — the cloud sandbox runs a bash stand-in
(`orun-cloud/apps/agents-worker/src/handlers/provision.ts` `bootstrapScript`).
The cloud control plane (saas-agents AG5–AG12) is largely shipped: Daytona
provisioning with BYO keys, agent-session tokens, event ingest with seq
dedupe, lease + sweep, a console session page that polls at 5s.

---

## 1. The gap, stated honestly

AG0–AG4 built a **proof system** — everything about a run is sealed,
reproducible, and walkable by hash. But the product a person touches is not a
proof system; it is a *conversation with something working on their behalf*.
Today that conversation cannot happen:

- **No real driver.** `harness: claude-code` validates as a string; nothing
  backs it. The stub proves the loop, not the product.
- **No inputs.** Steering and approvals are plumbed as channels and then
  abandoned at the CLI boundary. A run is fire-and-forget; `ask` means `deny`.
- **No presence.** A run lives and dies with the terminal that started it.
  Nothing lists live sessions; nothing re-attaches.
- **The TUI is a brochure.** It shows what agent types exist. It cannot
  launch, watch, steer, or approve.
- **The cloud tail is a poll.** The console renders relayed rows 5 seconds
  late and cannot talk back — the return queue (design §4.4 of `saas-agents`)
  is specified, unbuilt.

Each gap is the same missing thing viewed from a different seat: **a live
plane**. This epic builds exactly one — not five features.

---

## 2. The model: one session, many heads

Vocabulary this epic makes precise (and code should adopt):

- **The body** — the single runtime process executing the AG2 loop
  (`agent.Run`) for a session. It owns the driver subprocess, enforces tool
  policy, and is the **sole writer** of the session event log. One body per
  session, ever; the body's death is the session's terminal state.
- **A head** — any client attached to a body: the body's own terminal, the
  TUI, the cloud console, a future mobile surface. A head does three things:
  *render* the event stream, *inject* the three inputs (steer / verdict /
  interrupt), and *detach* without consequence. Heads hold no state that
  matters and are freely interchangeable — that is the design's central
  promise, and it is verifiable: two heads on one session must render the
  same conversation and be able to hand inputs off mid-run.
- **The attach plane** — the one protocol (`attach-protocol.md`) heads speak
  to bodies, over three transports:

| Transport | Body side | Head side | Used by |
|---|---|---|---|
| **In-process** | `agent.Run` inside the same process | Go channels (the attach frames as structs) | `orun agent run` rendering its own stream inline |
| **Local socket** | the body serves NDJSON frames on `.orun/agents/live/<sessionId>.sock` | any same-uid process dials the socket | TUI on the same machine; `orun agent attach <id>`; a second terminal |
| **Cloud relay** | `orun agent serve` dials out to the per-session DO (event batches up, return queue down — cloud AL6) | SSE (server→head) + control POSTs (head→body) through api-edge | the console; `orun agent attach as_…` from any laptop |

Same frames end to end. The TUI does not know or care whether the body is a
child process, a daemon on the same machine, or a Daytona sandbox in another
region — it renders `hello`, folds `event`s, paints `delta`s, and posts
`steer`/`verdict`/`interrupt`. **Interchangeability is not a feature; it is
the absence of transport-specific behavior.**

### 2.1 Interactivity is events (the honesty invariant, extended)

Every head input lands in the session log as a first-class event, attributed
to a principal:

- `steer` → `message_user` `{text, principal}` — the human's turn is part of
  the transcript, so `orun agent replay` reproduces the *conversation*, not
  just the agent's half.
- `verdict` → `approval_resolved` `{requestId, approved, reason, principal}` —
  who approved the gated tool is in the tamper-evident chain, not in a side
  table.
- `interrupt` → `harness_event` `{phase: "interrupted", principal}`.

Wire-only `delta` frames (streaming text chunks) are explicitly **not**
session events — they exist for felt latency and are superseded by the final
`message_agent` event at turn end. The sealed log stays bounded; the closed
11-kind vocabulary does not grow. This is how the epic adds a chat product
without touching the proof system.

### 2.2 Multi-head rules (small, load-bearing)

- All attached heads receive all frames. Presence (`presence` frame: heads
  attached, by principal + surface) is advisory UI, not authority.
- A `verdict` binds to one `approval_requested` `requestId`. The first valid
  verdict resolves it; later verdicts for the same id are acknowledged as
  no-ops (and logged as nothing — the log already has the resolution).
- `steer` is never lost: mid-turn steers queue in the body and inject at the
  next turn boundary (or immediately where the harness supports mid-turn
  input, as Claude Code's stream-JSON stdin does). Order of arrival at the
  body is the order in the log.
- `interrupt` stops the current turn, not the session. `end` is the graceful
  terminal ("wrap up and seal"); `kill` (CLI / control plane) is the abrupt
  one. All three are legal from any head.

---

## 3. The session host (the body stays addressable)

### 3.1 Process model

`orun agent run` today executes the loop and exits with the terminal. The
change: **the body always hosts the attach plane while it runs.**

- `orun agent run …` — runs the loop, renders its own stream inline (an
  in-process head), **and** serves the local socket. A second terminal (or
  the TUI) can attach mid-run. Ctrl+C in the hosting terminal interrupts;
  detach is not needed (it *is* the body).
- `orun agent run --detach` — forks the body into its own process group,
  prints the session id, and returns. The run continues headless; attach at
  will. This is what the TUI launch flow uses, so **closing the TUI never
  kills a run** — the tmux discipline, applied to agents.
- The body exits when the session reaches a terminal state (done / error /
  killed), after sealing (AG4 path, unchanged).

The head never runs the loop (locked decision 4). The TUI is *always* a
socket client, even for runs it just launched — one render path, and the
privileged-head asymmetry (where the launching surface has powers an attached
surface lacks) is unrepresentable.

### 3.2 The live registry

`.orun/agents/live/<sessionId>.json` — ephemeral, deliberately **not**
content-addressed (it is the `refs`-vs-objects split: liveness is state, not
content):

```jsonc
{ "sessionId": "as_7f3c…", "pid": 48112, "socket": ".orun/agents/live/as_7f3c….sock",
  "state": "running",          // mirrors the body's last state_changed
  "brief": "sha256:…", "agentType": "implementer", "task": "ORN-142",
  "startedAt": "…Z" }
```

Written by the body on start, updated on state change, removed on clean exit.
Stale entries (pid dead, socket gone) are swept on read by `orun agent ps` —
crash-safe without a daemon. Sockets are `0700`-dir, `0600`, same-uid only;
the local trust model is "you, on your machine," matching `.orun/` itself.

### 3.3 CLI surface

| Command | Behavior |
|---|---|
| `orun agent ps` | live sessions from the registry (id, type, task, state, age, heads attached) |
| `orun agent attach <sessionId>` | local id → dial the socket, open the TUI head directly in the session (replay-from-seq, then follow). `as_…` id not in the registry → remote attach (§6) |
| `orun agent kill <sessionId>` | post `end` (graceful; `--force` for SIGKILL + sweep), body seals what it has |
| `orun agent run` | as today, plus hosts the socket; `--detach` per §3.1; `--driver claude-code` becomes the default once AL1 lands (stub stays for tests) |
| `orun agent` (bare) | opens the TUI in Agent mode — the front door |

---

## 4. The Claude Code driver (`internal/agent/driver/claudecode.go`)

The reference driver, and the proof that the AG4 conformance oracle was worth
building — it must pass `CheckConformance` with zero special cases.

### 4.1 Wire

Launch: `claude -p --input-format stream-json --output-format stream-json
--include-partial-messages` in `Brief.Workdir`, with the brief's rendered
instructions as the system-prompt layer (`--append-system-prompt` from a
file), the MCP config path from `IO.MCPConfigPath`, and permission handling
routed through the control protocol. The binary path and minimum version are
pinned (`orun agent doctor` reports drift); the driver handshakes the
harness's declared protocol version and refuses loud on mismatch — a harness
upgrade must never silently change event semantics.

### 4.2 Event mapping (harness → the normalized vocabulary)

| Harness emission | `driver.Event` |
|---|---|
| assistant text (partial) | wire `delta` (via a new non-logged event kind `EventDelta`, see `attach-protocol.md` §4) |
| assistant text (final) | `EventMessage` |
| `tool_use` block | `EventToolCall` `{tool, argsDigest}` — full args to a transcript chunk, ref on the event |
| tool result | `EventToolResult` (bulk → transcript chunk + ref) |
| permission request (control protocol) | `EventApproval` `{requestId, tool, args}` — blocks the harness until the verdict returns on `IO.Approve` |
| `result` message | `EventCost` `{tokens, durationMs}` then `EventDone` `{outcome}` |
| harness session id (init) | `EventHarnessEvent` `{harnessSession}` — captured so a *paused* session can resume with `--resume <id>` (suspend/resume, cloud AL9) |
| stderr / crash | `EventError` |

Inputs: `IO.Steer` messages are written to the harness stdin as user turns
(mid-turn where supported — this is why steering feels immediate);
`interrupt` maps to the control-protocol interrupt request; verdicts answer
the pending permission request by `requestId`.

### 4.3 Tool policy meets the harness

The runtime remains the enforcement point (AG2 `foldEvent`, unchanged):
`deny` tools are absent from the MCP config the driver writes (unreachable),
`allow` passes, `ask` surfaces as `approval_requested` and blocks on the
verdict. The harness's own permission prompting is set to defer to the
runtime (its prompts become control-protocol requests, never TTY prompts) —
there is exactly one approval authority, and it is orun's, identically local
and cloud.

### 4.4 `internal/agent/mcp.go` (the deferred WP5 hookup, now due)

Writes the driver's MCP config: the **orun MCP** (`internal/workmcp`, stdio,
launched by the body in the workdir) always; the **platform MCP** added when
cloud-attached. Tool policy filters the config at write time (deny =
absent). This closes `orun-agents` design §5 as specified — no design change,
just the unbuilt file.

### 4.5 Testing without the vendor

Recorded stream-JSON fixtures (real captures, committed) drive the mapping
tests; a `fake-claude` script (reads stdin frames, replays a fixture) makes
the full loop — launch, steer, approve, interrupt, done — CI-runnable with no
key and no network. Live smoke behind `ORUN_LIVE_DRIVER_SMOKE=1`. This is the
`local-docker`/recorded-fixture posture the cloud epic already uses, applied
to the harness.

---

## 5. The TUI head (the experience bar is the desktop app)

The bar, stated as product: **attaching the TUI to a session must feel like
the Claude desktop app attached to a Claude Code session.** Concretely, that
means: you see the conversation as it streams; you can type at any time
without waiting for the agent to stop; tool activity is visible but folded
until you want it; a permission request physically cannot be missed; and
leaving costs nothing. Everything below serves one of those five sentences.

`ModeAgent` (key `3`) is elevated in place — same mode, same worked pattern
(`views/agent.go`, the `WaitFor*` re-arm loop, the frame-stability
discipline of `fitToScreen`/`clipBox`, cockpit theme). Layout follows the
sidebar│stage│inspector shell:

### 5.1 Sidebar — sessions first, types second

Live sessions (from the registry + remote list when authenticated), then
recent sealed sessions (`refs/agents/sessions/…`), then agent types (the
existing AG3 list). A live session row shows a state glyph, agent type, task
key, and an **attention badge** when an approval is waiting unattended —
visible from any mode via the tab header, because a blocked agent is a
product failure measured in minutes.

### 5.2 Stage — the conversation

- **Turns**: user turns (attributed — `you`, or another principal when a
  second head steered) and agent turns rendered as markdown, streamed by
  `delta` frames into the in-progress turn, replaced by the final
  `message_agent` on turn end.
- **Tool cards**: one line collapsed — `⚙ Edit internal/agent/mcp.go ✓` —
  expandable (`enter`) to args + result from the transcript chunks. Bursts
  coalesce ("⚙ 4 tool calls ▸"). This is the log-explorer's bounded-scrollback
  + severity discipline applied to tool activity.
- **The activity line**: while the agent works, one live line under the last
  turn — current tool or "thinking…", elapsed, token ticker (from
  `cost_sample`). The felt-progress heartbeat.
- **Approval cards**: inline, impossible to scroll away from while pending
  (sticky above the composer): tool, args digest, the agent's stated reason;
  `y` approve · `n` deny · `e` expand. Answering appends `approval_resolved`
  and unblocks the body.
- **Plan/checklist**: when the harness emits a todo/plan (`harness_event`
  payloads), render it as a pinned, ticking checklist — the single best
  progress affordance the desktop app has.

### 5.3 Composer — always-on input

A textinput pinned at the bottom of the stage, always focused when the stage
is: type → `enter` sends a `steer` (queued mid-turn, injected at the
boundary; the composer shows "queued" until the log echoes it). `esc`
interrupts the current turn (double-`esc` prompts for `end`). `ctrl+d`
detaches — the run continues, the sidebar row keeps streaming its state.
Mode-global keys (`1`/`2`/`3`, palette) keep working; the composer consumes
printable keys only, per the cockpit's key-routing precedence.

### 5.4 Inspector — the proof pane

The sealed brief (id + expandable instructions), the frozen affected set,
tool policy, cost so far, branch/PR when `artifact_produced` lands, and — for
remote bodies — the sandbox facts (provider, box id, lease). The inspector is
where "chat app" meets "proof system": everything the agent was given, by
hash, one keystroke away.

### 5.5 Launch flow

`n` in Agent mode: pick type → pick task/spec (from the work surface; or
"interactive" with a free-form goal) → **brief preview** (the `--dry-run`
render: instructions, affected set, policy — the "here is exactly what the
agent will see" moment) → launch. The TUI spawns `orun agent run --detach
--json`, reads the session id, and attaches through the front door like any
other head.

---

## 6. Remote attach (a Daytona session in the TUI; the console on a local bar)

### 6.1 `orun agent serve` — the real in-sandbox entrypoint

The AG2 loop with its attach plane pointed at the cloud relay: authenticates
with the injected `ORUN_SESSION_TOKEN`, pulls the frozen brief
(`spec pull @hash` + `agent pull <type>@hash`), runs, and speaks attach v1
over the dial-out — event batches up (the shipped ingest route, now
batched by frame), the return queue down (steer/verdict/interrupt from the
DO, cloud AL6). Heartbeat and token rotation as shipped in cloud AG6. This
retires `bootstrapScript`'s bash loop: the supervisor was always meant to be
orun (`saas-agents` design §0), and now it is.

### 6.2 `orun agent attach as_…` — the TUI as a remote head

An `as_` id not found in the local registry resolves remotely: `cliauth`
token → api-edge → `GET …/agents/sessions/{id}/attach` (SSE: `hello`, replay
from cursor, live frames) + `POST …/agents/sessions/{id}/input` (steer /
verdict / interrupt as control frames). The TUI head is byte-for-byte the §5
experience — the transport swap is invisible except for the sandbox facts in
the inspector. RBAC is the cloud's (the same `agent.session.*` actions the
console enforces); attribution is the authenticated principal, so a verdict
granted from a laptop reads identically in the console's log.

### 6.3 The handoff narrative (acceptance test, written as a story)

Dispatch a task from the cloud Work tab → a Daytona body boots. On a laptop:
`orun agent attach as_7f3c` → the TUI replays the session and goes live. The
agent hits an `ask` tool; the approval card renders in the TUI *and* the
console; the TUI answers first; the console shows the resolution attributed
to the laptop's principal within a second. The TUI steers ("also update the
changelog"), detaches. The session completes; both surfaces show the same
sealed replay, conversation included; `orun agent replay as_7f3c` offline
shows it too. **If any sentence of that story fails, AL4/AL8 are not done.**

---

## 7. Product principles carried from Claude Code remote

Adopted deliberately, as the grammar users already know:

1. **Durable session, ephemeral surface.** No head is ever required for
   progress; every head is replay-then-follow.
2. **Steering never blocks.** Type any time; the queue is visible; the turn
   boundary is the injection point. An agent you must wait for is a worse
   colleague, not a safer one.
3. **Permission requests travel.** The ask renders wherever a head is
   attached; unattended asks notify (terminal bell + OS notification locally;
   the cloud's notification plane remotely — cloud AL8) and the session state
   says `awaiting_approval` honestly.
4. **Handoff is a verb.** Session ids are cheap, printable, and pasteable;
   `attach` works from anywhere your identity works. The desktop-app move —
   "continue this in the terminal" — is `orun agent attach`, and its inverse
   is the console URL on any session the TUI prints.
5. **Progress is legible.** Deltas for immediacy, the activity line for
   heartbeat, the checklist for shape, tool cards for depth — four zoom
   levels, one stream.
6. **Leaving is safe, and the seal proves it.** Detach, reattach, replay:
   the sealed session is the same object with or without an audience — now
   including the audience's own words.

---

## 8. What this buys that nobody else has

The interactive-agent grammar (§7) is table stakes by 2026 — Claude Code
remote set it. orun's edge is that the *same* session that behaved like a
desktop-app chat seals into the object graph: the conversation, the
approvals with principals, the tool calls with content-addressed args, the
brief it all ran from — one Merkle chain from "what we said to it" to the PR
to the live revision. Attach-anywhere is the experience; **replay-anywhere,
provably**, is the moat. No bolt-on agent product can copy the second
property without rebuilding orun's substrate, which is exactly why the live
plane belongs in the binary.
