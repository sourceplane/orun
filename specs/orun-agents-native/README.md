# Spec: orun-agents-native — the attach binding v2 (AN0)

**The smallest cross-repo surface yet.** The paired cloud epic
(`orun-cloud/specs/epics/saas-agents-native/`, cluster **AN**) re-platforms
the per-session relay onto the Cloudflare Agents SDK and adds a cloud-side
conversational orchestrator. orun's share is exactly one milestone: **AN0**,
the transport swap `attach-protocol.md` §6.3 reserved on day one — the body's
relay dial-out collapses its two HTTP legs (batched `POST /events` up, the
`GET /inputs` long-poll down) into **one outbound WebSocket**, carrying the
same attach-v1 frames it carries today. No frame changes, no vocabulary
changes, no session-log changes; the golden fixtures are untouched, which is
the point — the protocol froze two epics ago precisely so a carriage could be
swapped without a contract conversation.

## Status

| Field | Value |
|-------|-------|
| Status | **Shipped** (2026-07-17) — single-README spec (holding-register convention) |
| Cluster | **AN** (agents native — cross-repo; orun owns **AN0**, orun-cloud owns **AN1–AN7**) |
| Target branch | `claude/orun-cloudflare-architecture-da6uad` (design), then `main` |
| Builds on | `orun-agents-live` AL0/AL4 as-built (`internal/agent/attach/relay.go` — the dial-out relay client this milestone gives a second binding; `frames.go` — unchanged) · cloud AN1 (the WS-capable relay this binding dials; **soft dependency** — the HTTP binding remains valid indefinitely, so AN0 ships behind capability detection) |
| Decisions locked | (1) **Frames are frozen** — AN0 is a binding, not a protocol revision; a fixture diff in this milestone is a bug. (1a, **amendment, as-built**) **The durable log keeps its confirmed HTTP carriage**: durability needs delivery confirmation + retry-and-drop-loudly, the HTTP response IS that confirmation, and a batch-ack frame would be a vocabulary revision lock 1 forbids — so the socket collapses the *chatty* legs (the input long-poll → push; the per-delta POST + input acks → inline), while `POST /events` stays. The cloud-side log is transport-invariant either way (proven in the conformance suite). (2) **HTTP stays** — the WS binding is preferred-with-fallback (dial WS, fall back to POST/long-poll on failure or downgrade mid-session); a body must never strand on transport. (3) **Same auth, same lease** — the WS dial presents the session token exactly as the HTTP legs do, re-dials on rotation, and dies within one TTL on refusal, preserving the kill/runaway posture. |
| Gate | Buildable against a fake WS relay speaking the golden fixtures; live verification rides cloud AN1's staging relay. |

## AN0 — one socket, same frames — ✅ Shipped

As-built (`internal/agent/attach/relay_ws.go` + `relay.go`, dep
`github.com/coder/websocket`):

- The **wire**: one outbound WS dialed at `{BaseURL}/wire` with the live
  session bearer. Down come head input frames at push latency (no poll
  interval in the path); up go the body's acks (inline, HTTP ack door as
  emergency fallback) and best-effort deltas. Durable event batches keep
  `POST /events` (amended decision 1a). Pings answered with pongs; a relay
  bye ends the pumps cleanly.
- Capability detection + fallback (lock 2): a 404/405/426/501/400 on the
  dial pins the session to the HTTP long-poll binding; a transient failure or
  mid-session drop runs the long-poll for `WSRetryEvery` (default 30s) and
  re-probes. `DisableWS` is the hard opt-out.
- Token-rotation re-dial (lock 3): the live bearer is watched on-wire
  (`TokenCheckEvery`, default 30s); a change closes and re-dials with the
  fresh token.

**Done-when, proven in `relay_ws_test.go`** (a fixture WS relay per the
gate): a steer acks with the long-poll asleep for 10 minutes (push-latency
proof); a forced socket drop mid-run downgrades to HTTP and the input queued
during the outage is delivered and acked (no gap); the body re-dials on its
own after a drop and rotation re-dials present the fresh bearer without
dropping frames; a wire-less relay falls back with zero wire connections; the
durable `/events` log is byte-identical with the wire up and disabled; the
golden fixtures are bit-for-bit untouched by the entire diff (`testdata/`
clean). Full suite + `-race` green. Live verification against the cloud
relay's body-wire door rides the cloud AN2 slice that lands it.

## What this is not

Not a new protocol, not a chance to grow the frame vocabulary, not the place
the cloud's Workspace Agent reaches into orun — that agent is just another
head, attributed like any other (`surface: "workspace-agent"`), and the body
neither knows nor cares that a durable object rather than a human is
watching. Execution truth, the driver seam, tool policy, and the sealed
session remain exactly where AG0–AG4 put them.
