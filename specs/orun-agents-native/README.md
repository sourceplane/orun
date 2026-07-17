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
| Status | **Draft** — single-README spec (holding-register convention); promoted to a full doc set if AN0's slice grows |
| Cluster | **AN** (agents native — cross-repo; orun owns **AN0**, orun-cloud owns **AN1–AN7**) |
| Target branch | `claude/orun-cloudflare-architecture-da6uad` (design), then `main` |
| Builds on | `orun-agents-live` AL0/AL4 as-built (`internal/agent/attach/relay.go` — the dial-out relay client this milestone gives a second binding; `frames.go` — unchanged) · cloud AN1 (the WS-capable relay this binding dials; **soft dependency** — the HTTP binding remains valid indefinitely, so AN0 ships behind capability detection) |
| Decisions locked | (1) **Frames are frozen** — AN0 is a binding, not a protocol revision; a fixture diff in this milestone is a bug. (2) **HTTP stays** — the WS binding is preferred-with-fallback (dial WS, fall back to POST/long-poll on failure or downgrade mid-session); a body must never strand on transport. (3) **Same auth, same lease** — the WS dial presents the session token exactly as the HTTP legs do, re-dials on rotation, and dies within one TTL on refusal, preserving the kill/runaway posture. |
| Gate | Buildable against a fake WS relay speaking the golden fixtures; live verification rides cloud AN1's staging relay. |

## AN0 — one socket, same frames — 🗓️ Planned

- `internal/agent/attach/relay.go`: a WS binding beside the HTTP one —
  durable event batches and best-effort deltas multiplexed up, head input
  frames down (replacing the long-poll's up-to-interval latency with push),
  acks inline. Backoff/redial/drop discipline carried over unchanged
  (resilient, loud, never wedging the loop).
- Capability detection + fallback (lock 2); token-rotation re-dial (lock 3).
- Conformance: the fixture suite driven over both bindings produces
  byte-identical relayed frame logs; a mid-session downgrade WS→HTTP loses
  nothing sealed (seq dedupe absorbs the seam).

**Done when:** a fixture session relayed over WS and over HTTP yields
identical cloud-side logs; verdict delivery over WS is push-latency (no poll
interval in the path); a forced socket drop mid-run downgrades to HTTP and
the sealed session shows no gap; a rotated token re-dials without dropping
frames; the golden fixtures are bit-for-bit untouched by the entire diff.

## What this is not

Not a new protocol, not a chance to grow the frame vocabulary, not the place
the cloud's Workspace Agent reaches into orun — that agent is just another
head, attributed like any other (`surface: "workspace-agent"`), and the body
neither knows nor cares that a durable object rather than a human is
watching. Execution truth, the driver seam, tool policy, and the sealed
session remain exactly where AG0–AG4 put them.
