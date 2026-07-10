// Package attach implements the attach protocol v1
// (specs/orun-agents-live/attach-protocol.md): the one wire between a session
// body (the runtime process, sole writer of the session log) and its heads
// (TUI, console, a second terminal). Frames are newline-delimited JSON,
// identical across the three transports — in-process, local unix socket, and
// the cloud relay. The sealed session log stays the single source of truth;
// everything that matters survives resume, and anything that doesn't (deltas,
// presence) is explicitly cosmetic.
package attach

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
)

// Version is the protocol version this package speaks. A body refuses an
// attach with a version it does not speak — loud, never lossy.
const Version = 1

// Frame types, body → head.
const (
	THello    = "hello"
	TEvent    = "event"
	TLive     = "live"
	TDelta    = "delta"
	TPresence = "presence"
	TAck      = "ack"
	TPing     = "ping"
	TBye      = "bye"
	TError    = "error"
)

// Frame types, head → body.
const (
	TAttach    = "attach"
	TSteer     = "steer"
	TVerdict   = "verdict"
	TInterrupt = "interrupt"
	TEnd       = "end"
	TDetach    = "detach"
	TPong      = "pong"
)

// Ack reasons (machine-readable, attach-protocol.md §2).
const (
	ReasonNotPending = "not_pending"
	ReasonTerminal   = "terminal"
	ReasonLagged     = "lagged"
)

// Error codes.
const (
	CodeVersion = "version"
	CodeBadFrame = "bad_frame"
)

// Head describes one attached head in a presence frame.
type Head struct {
	Principal  string `json:"principal"`
	Surface    string `json:"surface"`
	AttachedAt string `json:"attachedAt,omitempty"`
}

// Frame is one protocol frame — a flat tagged union: T selects the type and
// the per-type fields below apply (unknown fields are ignored on consume,
// preserved on relay; unknown T MUST be ignored). The `ref` key is shared
// deliberately: on an event frame it is the transcript-chunk ref; on an
// input/ack frame it is the client-chosen correlation id — disjoint frame
// types, one JSON key.
type Frame struct {
	V int    `json:"v"`
	T string `json:"t"`

	// hello
	SessionID string `json:"sessionId,omitempty"`
	State     string `json:"state,omitempty"`
	BriefID   string `json:"briefId,omitempty"`
	AgentType string `json:"agentType,omitempty"`
	Task      string `json:"task,omitempty"`
	RunKind   string `json:"runKind,omitempty"`
	Harness   string `json:"harness,omitempty"`
	Model     string `json:"model,omitempty"`
	LatestSeq *int   `json:"latestSeq,omitempty"`

	// event (the sealed AgentSessionEvent shape, re-serialized)
	Seq     *int           `json:"seq,omitempty"`
	Kind    string         `json:"kind,omitempty"`
	At      string         `json:"at,omitempty"`
	Payload map[string]any `json:"payload,omitempty"`

	// live / attach cursor
	FromSeq *int `json:"fromSeq,omitempty"`
	From    *int `json:"from,omitempty"`

	// delta
	Turn int    `json:"turn,omitempty"`
	Text string `json:"text,omitempty"`

	// presence
	Heads []Head `json:"heads,omitempty"`

	// input correlation / event transcript-chunk ref (see type doc)
	Ref string `json:"ref,omitempty"`

	// ack
	OK     *bool  `json:"ok,omitempty"`
	Reason string `json:"reason,omitempty"`

	// verdict
	RequestID string `json:"requestId,omitempty"`
	Approved  *bool  `json:"approved,omitempty"`

	// attach
	Surface string `json:"surface,omitempty"`

	// error / bye
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

// intp/boolp — tiny literal-pointer helpers for frame construction.
func intp(v int) *int    { return &v }
func boolp(v bool) *bool { return &v }

// HelloFrame builds the first body→head frame after an attach.
func HelloFrame(info SessionInfo, state string, latestSeq int) Frame {
	return Frame{V: Version, T: THello,
		SessionID: info.SessionID, State: state, BriefID: info.BriefID,
		AgentType: info.AgentType, Task: info.Task, RunKind: info.RunKind,
		Harness: info.Harness, Model: info.Model, LatestSeq: intp(latestSeq)}
}

// EventFrame re-serializes one session-log event.
func EventFrame(seq int, kind, at string, payload map[string]any, ref string) Frame {
	return Frame{V: Version, T: TEvent, Seq: intp(seq), Kind: kind, At: at, Payload: payload, Ref: ref}
}

// LiveFrame marks the replay/live boundary.
func LiveFrame(fromSeq int) Frame { return Frame{V: Version, T: TLive, FromSeq: intp(fromSeq)} }

// DeltaFrame carries streaming text for the in-progress turn (wire-only).
func DeltaFrame(turn int, text string) Frame {
	return Frame{V: Version, T: TDelta, Turn: turn, Text: text}
}

// PresenceFrame lists the currently attached heads (advisory).
func PresenceFrame(heads []Head) Frame { return Frame{V: Version, T: TPresence, Heads: heads} }

// AckFrame answers a head input frame by its ref.
func AckFrame(ref string, ok bool, reason string) Frame {
	return Frame{V: Version, T: TAck, Ref: ref, OK: boolp(ok), Reason: reason}
}

// PingFrame is the transport keepalive; heads answer with a pong.
func PingFrame(at string) Frame { return Frame{V: Version, T: TPing, At: at} }

// PongFrame answers a ping.
func PongFrame(at string) Frame { return Frame{V: Version, T: TPong, At: at} }

// ByeFrame announces the body is closing this connection.
func ByeFrame(reason string) Frame { return Frame{V: Version, T: TBye, Reason: reason} }

// ErrorFrame reports a protocol-level failure.
func ErrorFrame(code, message string) Frame {
	return Frame{V: Version, T: TError, Code: code, Message: message}
}

// AttachFrame opens a head connection with a replay cursor.
func AttachFrame(from int, surface string) Frame {
	return Frame{V: Version, T: TAttach, From: intp(from), Surface: surface}
}

// SteerFrame queues a user turn.
func SteerFrame(ref, text string) Frame { return Frame{V: Version, T: TSteer, Ref: ref, Text: text} }

// VerdictFrame answers a pending approval request.
func VerdictFrame(ref, requestID string, approved bool, reason string) Frame {
	return Frame{V: Version, T: TVerdict, Ref: ref, RequestID: requestID, Approved: boolp(approved), Reason: reason}
}

// InterruptFrame stops the current turn.
func InterruptFrame(ref string) Frame { return Frame{V: Version, T: TInterrupt, Ref: ref} }

// EndFrame requests the graceful terminal.
func EndFrame(ref string) Frame { return Frame{V: Version, T: TEnd, Ref: ref} }

// DetachFrame closes politely.
func DetachFrame() Frame { return Frame{V: Version, T: TDetach} }

// WriteFrame encodes one frame as a single NDJSON line.
func WriteFrame(w io.Writer, f Frame) error {
	b, err := json.Marshal(f)
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}

// Decoder reads frames off an NDJSON stream. Blank lines are skipped; a
// malformed line is an error (the transport is ours end to end).
type Decoder struct{ r *bufio.Reader }

// NewDecoder wraps r for frame reading.
func NewDecoder(r io.Reader) *Decoder { return &Decoder{r: bufio.NewReader(r)} }

// Next returns the next frame, or io.EOF at end of stream.
func (d *Decoder) Next() (Frame, error) {
	for {
		line, err := d.r.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			return Frame{}, err
		}
		trimmed := trimSpace(line)
		if len(trimmed) == 0 {
			if err != nil {
				return Frame{}, err
			}
			continue
		}
		var f Frame
		if uerr := json.Unmarshal(trimmed, &f); uerr != nil {
			return Frame{}, fmt.Errorf("attach: bad frame: %w", uerr)
		}
		return f, nil
	}
}

func trimSpace(b []byte) []byte {
	start, end := 0, len(b)
	for start < end && (b[start] == ' ' || b[start] == '\t' || b[start] == '\r' || b[start] == '\n') {
		start++
	}
	for end > start && (b[end-1] == ' ' || b[end-1] == '\t' || b[end-1] == '\r' || b[end-1] == '\n') {
		end--
	}
	return b[start:end]
}
