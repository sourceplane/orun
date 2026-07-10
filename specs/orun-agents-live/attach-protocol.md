# orun-agents-live — The attach protocol (v1)

Status: Draft (normative once AL0 lands)

The one wire between a session **body** and its **heads**. Three transports,
identical frames. Implemented in Go (`internal/agent/attach`) and TypeScript
(the cloud relay + console), kept honest by shared golden fixtures — the
`worklens` Go↔TS conformance discipline applied to a live protocol.

Design constraints, in order: (1) the sealed session log stays the single
source of truth and its 11-kind vocabulary does not grow; (2) a head must be
implementable in an afternoon (NDJSON, no binary framing, no required state
beyond a cursor); (3) every frame that matters survives resume — anything
that doesn't (deltas, presence) is explicitly cosmetic.

---

## 1. Framing

Newline-delimited JSON, UTF-8, one frame per line. Every frame:

```jsonc
{ "v": 1, "t": "<frame type>", ... }
```

Unknown frame types MUST be ignored (forward compatibility); unknown fields
MUST be preserved-if-relayed, ignored-if-consumed. `v` bumps only on
incompatible change; a body refuses an `attach` with a `v` it does not speak
(`error{code:"version"}`) — loud, never lossy.

---

## 2. Body → head frames

| `t` | Payload | Semantics |
|---|---|---|
| `hello` | `sessionId, state, briefId, agentType, task, runKind, latestSeq, harness{driver,model}, resumedFrom?` | First frame after `attach`. `latestSeq` tells the head how much replay to expect before `live`. |
| `event` | `seq, kind, at, payload, ref?` | One session-log event — exactly the sealed `AgentSessionEvent` shape (`nodes/agents.go:227`), the closed 11-kind vocabulary. Replay and live are the same frame; a head folds by `seq` and needs no other ordering. |
| `live` | `fromSeq` | Replay complete; subsequent `event`s are real-time. The head may render a "live" affordance. |
| `delta` | `turn, text` | Streaming text for the in-progress agent turn. **Wire-only, never logged, never replayed**; superseded by the turn's final `message_agent` event. A head that ignores `delta` is merely less pleasant, not less correct. |
| `presence` | `heads: [{principal, surface, attachedAt}]` | Advisory. Emitted on attach/detach. Never authority for anything. |
| `ack` | `ref, ok, reason?` | Answers a head input frame by its `ref` (§3). `ok:false` carries a machine reason (`stale_request`, `not_pending`, `terminal`). |
| `ping` | `at` | Keepalive; heads answer `pong`. Transport-level liveness only. |
| `bye` | `reason` | Body is closing the connection (terminal state, kill, shutdown). The registry/state carry the rest. |
| `error` | `code, message` | Protocol-level failure (bad frame, version, unauthorized). |

The `event` frame is deliberately a re-serialization of the sealed event, not
a parallel schema — a head that can render a replayed session can render a
live one by construction, and the cloud relay can mirror frames to storage
without translation.

## 3. Head → body frames

Every input frame carries a client-chosen `ref` (idempotency + `ack`
correlation) and is attributed by the transport's authenticated principal —
locally the uid's user (`usr_cli`), remotely the bearer's principal. A head
never self-declares identity in the frame.

| `t` | Payload | Body behavior |
|---|---|---|
| `attach` | `from?` (seq cursor), `surface` (`tui`/`console`/`cli`) | Replay events `> from` (default 0 = full), then `live`. Multiple concurrent attaches per session are the normal case. |
| `steer` | `ref, text` | Enqueue as the next user turn (mid-turn injection where the driver supports it). Appends `message_user{text, principal}` to the log **when injected** — the log records what the agent actually received, in order. `ack` on enqueue. |
| `verdict` | `ref, requestId, approved, reason?` | Resolves a pending `approval_requested`. First valid verdict wins → `approval_resolved{requestId, approved, reason, principal}` + unblocks the driver; a verdict for a non-pending id gets `ack{ok:false, reason:"not_pending"}`. |
| `interrupt` | `ref` | Stop the current turn (driver control request). Logs `harness_event{phase:"interrupted", principal}`. The session stays live and steerable. |
| `end` | `ref` | Graceful terminal: the body asks the driver to conclude, seals, exits. |
| `detach` | — | Close politely (a dropped connection is equivalent; detach just skips the keepalive timeout). |
| `pong` | `at` | Answers `ping`. |

## 4. The driver-seam delta (one addition, wire-only)

`driver.EventKind` gains `EventDelta` ("delta"): partial assistant text with
a turn marker. The runtime forwards deltas to attached heads and **does not
fold them into the session log** — `foldEvent` treats `EventDelta` as
observe-only. This is the only vocabulary change in the epic, and it is
below the sealed layer by construction. (`SessionLog.Append` already drops
unknown kinds; AL0 makes the skip explicit and tested rather than incidental.)

## 5. Resume, exactly

A head's durable state is one integer: the highest `seq` folded. Reconnect =
`attach{from: seq}`. Deltas for a turn already finalized are never re-sent
(they no longer exist); a turn in progress at attach time streams deltas from
its next chunk — the head shows the final message correctly either way. This
is what makes flaky SSH, laptop sleep, and SSE reconnects boring.

## 6. Transport bindings

### 6.1 In-process (the body's own head)

The frames as Go structs over channels — `attach.Client` implemented by a
direct pair with the server core. Exists so `orun agent run`'s inline
rendering and the TUI share one head implementation, and so tests drive the
protocol without sockets.

### 6.2 Local unix socket

`.orun/agents/live/<sessionId>.sock`, `0600`, dir `0700`, same-uid. Raw
NDJSON both directions. The body is the server; it multiplexes heads
internally (per-head send queues; a slow head is disconnected past a bounded
buffer — `bye{reason:"lagged"}` — and simply re-attaches from its cursor;
deltas are dropped first under pressure since they are cosmetic).

### 6.3 Cloud relay (the split binding)

The body (`orun agent serve`) dials **out** (NAT-safe, the shipped posture):

- Up: `POST …/sessions/{id}/events` — the existing ingest route, its batch
  items now exactly `event` frames (they already share the event shape;
  ingest keeps its ≤100/batch + seq dedupe). Deltas ride a separate
  best-effort `POST …/sessions/{id}/stream` the DO fans out but never
  stores.
- Down: the return queue — long-poll `GET …/sessions/{id}/inputs?cursor=` in
  v1 (boringly reliable through proxies; an upgrade to a bidirectional
  stream is a transport swap, not a protocol change). Items are head input
  frames verbatim; the body `ack`s by posting acks upstream.

The head side is the DO relay's client surface (cloud AL6): `GET …/attach`
(SSE; frames as SSE `data:` lines — `hello`, replayed `event`s from the R2
mirror + Postgres index, `live`, then live fan-out) and `POST …/input` (one
head frame per call, authenticated, principal-stamped by api-edge). The
console and the remote TUI are indistinguishable to the relay.

## 7. Conformance (shared fixtures, two implementations)

`internal/agent/attach/testdata/*.ndjson` — golden frame sequences covering:
attach-replay-live, mid-turn steer ordering, verdict races (two heads, one
approval), interrupt during tool call, resume-after-drop mid-delta, lagged
head disconnect, version refusal. The fixtures are copied verbatim into
`orun-cloud` (`packages/contracts/src/agents-attach/fixtures/`) and both the
Go and TS codecs must round-trip them byte-identically — the cross-repo seam
is a file diff, testable with neither Daytona nor a browser.

## 8. Data-model deltas (small, and no new sealed kinds)

- **Live registry** `.orun/agents/live/<sessionId>.json` (design §3.2) —
  ephemeral state, not content; never synced, never sealed.
- **Attribution payloads** — `message_user` and `approval_resolved` payloads
  gain a `principal` field (payloads are open maps already; no schema
  break). Local bodies stamp `usr_cli`; serve bodies stamp the verdict's
  authenticated principal relayed in the input frame envelope.
- **`AgentSessionSnapshot`, `AgentSessionSegment`, `AgentBrief`,
  `AgentTypeSnapshot`: unchanged.** The epic's entire persistence footprint
  is two payload fields and a temp directory — by design.
