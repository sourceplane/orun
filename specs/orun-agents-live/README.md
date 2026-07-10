# Spec: orun-agents-live — one session, many heads

**The runtime shipped; this epic gives it presence.** AG0–AG4
(`../orun-agents/`) built the proof system: sealed agent types, frozen briefs,
a driver seam with a conformance oracle, tamper-evident session logs, replay.
What it cannot do yet is *be talked to*. There is no real coding-agent driver
(only the deterministic stub), the steer channel exists but nothing writes to
it, `ask`-gated tools auto-deny because no approver is ever attached, and the
TUI Agent mode is a read-only catalog browser with a transcript pane no
producer feeds. This epic ships the missing half of the product: the **Claude
Code driver** (real hands), a **live attach plane** (one protocol, three
transports), and an **interactive TUI head** whose experience matches a Claude
desktop app attached to a session — watch the stream, chat mid-run, approve
tools, detach and come back. The same protocol is what the cloud console
speaks, so a session running in a Daytona sandbox can be attached from the
terminal or the browser interchangeably.

## Status

| Field | Value |
|-------|-------|
| Status | **Draft** — authored, ready for review; open decisions in `risks-and-open-questions.md` |
| Cluster | **AL** (agents live — cross-repo; orun owns the **protocol, driver, session host, TUI head** [AL0–AL5], orun-cloud owns the **relay + console head + handoff** [AL6–AL9] in `orun-cloud/specs/epics/saas-agents-live/`) |
| Target branch | `claude/orun-agents-evolution-e1eyx5` (design), then `main` (PRs merged incrementally) |
| Builds on | `orun-agents/` AG0–AG4 as-built (the loop `internal/agent/runtime.go`, the driver seam `internal/agent/driver`, `SessionLog` + seal/replay, the closed 11-kind event vocabulary, `RunOptions.Observe`) · `orun-work` WP5 (`internal/workmcp` — the MCP hands) · `internal/tui` (the cockpit: `ModeAgent`, the `WaitFor*` channel-to-msg stream pattern, the frame-stability discipline) · `internal/cliauth` + `internal/remotestate` (how a local head authenticates to the cloud relay) · cloud `saas-agents` AG5/AG6 as-built (Daytona provisioning, session tokens, event ingest, lease/sweep) |
| Decisions locked | (1) **One session, many heads** — a session is owned by exactly one runtime process (the *body*, sole writer of the session log); any number of *heads* (TUI, console, future surfaces) attach, render the same event stream, and inject the same three inputs (steer, verdict, interrupt). Heads are ephemeral and hold no authoritative state; killing a head never kills the session. (2) **One protocol, three transports** — the attach protocol (`attach-protocol.md`) is the *only* way a head talks to a body: in-process channels (the body's own terminal), a local unix socket (another process on the machine), and the cloud relay (SSE + control POSTs through api-edge). Frames are identical; only the carriage differs. (3) **Interactivity is events** — every head input becomes a session-log event (`message_user`, `approval_resolved`) with principal attribution, so replay reproduces the whole conversation and the proof system absorbs interactivity instead of leaking around it. Wire-only `delta` frames carry streaming text but never enter the sealed log. (4) **The head never runs the loop** — the TUI always attaches to a body process it spawned (or found); local and remote heads are symmetric clients, one render path. (5) **The Claude Code driver is the reference driver** — headless bidirectional stream-JSON, mapped onto the existing normalized `driver.Event` vocabulary, passing the AG4 conformance oracle untouched. (6) **The sealed vocabulary does not grow** — the 11 event kinds are already sufficient; this epic adds wire frames, not object kinds. |
| Gate | **Human-independent through AL3.** Protocol, session host, and TUI head run against the stub driver with no credentials. AL1 (Claude Code driver) needs an `ANTHROPIC_API_KEY` for live smoke only — conformance and mapping tests run against recorded fixtures. AL4 pairs with cloud AL6 (the relay) but is developed against a local fake relay speaking the same fixtures. |

## The one-paragraph thesis

Every serious agent product converged on the same shape in 2025–26: the
session became the durable thing and the surfaces became clients — Claude
Code's remote sessions attach from web, desktop, and terminal identically,
and handoff between them is the feature people actually feel. orun is
structurally better-placed to do this than anyone, because its session is
already **content** — an append-only, tamper-evident, replayable event log —
and its runtime is already **one binary in two contexts**. What's missing is
only the live plane: a body that stays addressable while it runs, heads that
attach by replay-then-follow, and inputs that flow back as attributed events.
Once that plane exists, "the TUI Agent tab," "the console session page," and
"attach to the Daytona box from my laptop" stop being three features and
become one protocol rendered three ways — and the sealed session that falls
out at the end is *still* the same proof object AG4 ships, now with the human
conversation inside it.

## What changes for a user

| Today (AG4 as-built) | After this epic |
|---|---|
| `orun agent run --driver stub` prints a canned transcript and exits | `orun agent run --type implementer --task ORN-142` delegates to Claude Code, streams live, and can be steered from the keyboard |
| `ask` tools auto-deny (no approver exists) | Approval cards render in whatever head is attached; a keystroke answers; unattended asks queue and notify |
| TUI Agent mode lists agent types, transcript pane is dead | TUI Agent mode is a chat: composer at the bottom, streaming turns, collapsible tool cards, inline approvals, launch flow, sessions survive detach |
| A run dies with its terminal | The body hosts a socket; `orun agent ps` lists live sessions; `orun agent attach <id>` rejoins from any terminal |
| Cloud sessions are visible via a 5s poll, read-only | `orun agent attach as_…` puts the *same TUI head* on a Daytona session; the console does the same in the browser; both steer and approve, attributed |

## Read order

1. This README.
2. [`design.md`](./design.md) — the body/head model, the session host, the
   Claude Code driver, the TUI head (the desktop-app bar), remote attach, and
   the product principles carried over from Claude Code remote.
3. [`attach-protocol.md`](./attach-protocol.md) — the wire: frames, the three
   transports, multi-head semantics, resume, conformance fixtures, and the
   (small) data-model deltas.
4. [`implementation-plan.md`](./implementation-plan.md) — AL0–AL5 (orun) with
   "done when"; cloud AL6–AL9 live in the paired epic.
5. [`risks-and-open-questions.md`](./risks-and-open-questions.md).

## Milestones at a glance (orun-owned; AL6–AL9 in the cloud epic)

| ID | Milestone | Status |
|----|-----------|--------|
| AL0 | The attach plane: `internal/agent/attach` — frame types, Go encode/decode, golden fixtures (shared with the cloud, worklens-style); runtime input injection (`Inputs`: steer/verdict/interrupt → attributed session events); the in-process head contract | ✅ Shipped |
| AL1 | The Claude Code driver: `driver/claudecode.go` — bidirectional stream-JSON, permission bridge → approval events, interrupt, harness-session capture for resume; MCP config wiring (`internal/agent/mcp.go`, the deferred WP5 hookup); passes `CheckConformance` + recorded-fixture suite | ✅ Shipped |
| AL2 | The session host: the body serves attach v1 on a unix socket; the live registry (`.orun/agents/live/`); `orun agent ps` / `attach <id>` / `kill <id>`; detach-safe process model | ✅ Shipped |
| AL3 | The TUI head: ModeAgent becomes the interactive surface — session sidebar, conversation stage (turns, tool cards, approval cards, progress line), composer (steer/interrupt/detach), launch flow with brief preview | ✅ Shipped |
| AL4 | Remote attach: `orun agent serve` (the real in-sandbox entrypoint, retiring the cloud's bash stand-in) speaks attach v1 over the dial-out; `orun agent attach as_…` — the same TUI head over SSE + control POSTs via `cliauth` | ✅ Shipped |
| AL5 | Hardening + evals: multi-head arbitration fixtures, interrupt/steer semantics under load, replay-with-conversation goldens, delta backpressure, docs | 🗓️ Planned |

## Scope boundary

| In scope (orun) | Out of scope (→ elsewhere) |
|---|---|
| The attach protocol + Go implementation; the Claude Code driver; MCP config wiring; the session host + live registry + `ps/attach/kill`; the interactive TUI head; `orun agent serve`; the remote-attach client | The per-session DO relay, R2 mirror, SSE fan-out, api-edge attach routes, the console session head, presence/notifications, suspend/resume choreography, metering from relayed cost samples — all `orun-cloud/specs/epics/saas-agents-live/` (**AL6–AL9**); MCP tool *definitions* (`orun-work` WP5, shipped); sandbox provisioning + identity (cloud `saas-agents` AG5/AG6, shipped) |

## Relationship to existing work

- **`orun-agents` (AG0–AG4)** — the substrate. This epic adds no object
  kinds: the session log, seal, and replay are reused byte-identically. The
  `RunOptions.Observe` seam AG3 landed "for the TUI + cloud DO relay" is
  exactly the seam the attach server consumes.
- **`orun-work` WP5 (`internal/workmcp`)** — the hands. AL1 finally writes
  the driver's MCP config pointing at it, closing the loop design §5 of
  `orun-agents` specified.
- **cloud `saas-agents` (AG5–AG12)** — the box and the wire. Provisioning,
  tokens, ingest, lease, sweep are shipped there; this epic's AL4 replaces
  the bootstrap stand-in with `orun agent serve`, and the paired
  `saas-agents-live` epic upgrades the relay to attach v1 so the console and
  the TUI are the same head.
- **Claude Code remote (prior art, studied deliberately)** — the product
  grammar this epic adopts: durable session / ephemeral surface, attach =
  replay + follow, steering queues into the next turn boundary, permission
  prompts travel to whichever surface is watching, handoff is a first-class
  verb, and notifications cover the gap when no head is attached.
