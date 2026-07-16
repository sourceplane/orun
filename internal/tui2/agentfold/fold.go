// Package agentfold folds an attach-v1 frame stream into a renderable
// conversation — the cockpit v2 counterpart of the console's
// lib/agents/conversation.ts. Both heads fold the same closed 11-kind
// session-event vocabulary (internal/nodes/agents.go) from the same wire
// (internal/agent/attach), and the fixture goldens in this package replay
// the protocol's shared testdata so the two heads cannot silently diverge
// (specs/orun-tui-v2 §9).
//
// The fold is pure: frames in, conversation state out, no I/O, no
// rendering. Surfaces render from it; tests replay into it.
package agentfold

import (
	"fmt"
	"strings"

	"github.com/sourceplane/orun/internal/agent/attach"
)

// ItemKind classifies one conversation line.
type ItemKind int

const (
	// ItemAgent is an agent turn.
	ItemAgent ItemKind = iota
	// ItemUser is an attributed user turn (any head).
	ItemUser
	// ItemTool is a tool-call card (Detail holds the collapsed body).
	ItemTool
	// ItemNote is a quiet system line: state changes, harness phases,
	// artifacts, approvals, errors.
	ItemNote
)

// Item is one folded conversation entry.
type Item struct {
	Kind      ItemKind
	Text      string
	Principal string
	// Detail carries the expandable body: tool output, artifact URL,
	// approval request id.
	Detail string
	// Seq is the session-event sequence that produced the item (0 for
	// wire-only lines).
	Seq int
}

// Approval is a pending ask, oldest first.
type Approval struct {
	RequestID string
	Tool      string
}

// Conversation is the folded head state for one session.
type Conversation struct {
	// Items is the transcript, in arrival order.
	Items []Item
	// Pending holds unanswered approvals, oldest first.
	Pending []Approval
	// Streaming is the in-flight agent text (wire-only deltas; it never
	// enters Items — the sealed message_agent event does, replacing it).
	Streaming string
	// Live is true between the live marker and bye/terminal state.
	Live bool
	// State is the last state_changed value.
	State string
	// Status is a short head-status line ("detached (lagged)").
	Status string
	// Tokens is the latest cost sample, pre-rendered.
	Tokens string
	// LatestSeq tracks the newest folded event for cursor resume.
	LatestSeq int

	rev int
	sb  strings.Builder
}

// New returns an empty conversation.
func New() *Conversation { return &Conversation{} }

// Rev changes whenever folded state changes (memo key material).
func (c *Conversation) Rev() int { return c.rev }

// Fold applies one head-bound frame. Input-direction frames (steer,
// verdict, interrupt, end, attach, detach) are ignored: their effects
// come back as attributed events, which is the whole interactivity-is-
// events contract.
func (c *Conversation) Fold(f attach.Frame) {
	switch f.T {
	case attach.THello:
		c.rev++
		if f.State != "" {
			c.State = f.State
		}
	case attach.TLive:
		c.rev++
		c.Live = true
	case attach.TDelta:
		c.rev++
		c.sb.WriteString(f.Text)
		c.Streaming = c.sb.String()
	case attach.TEvent:
		c.rev++
		if f.Seq != nil && *f.Seq > c.LatestSeq {
			c.LatestSeq = *f.Seq
		}
		c.foldEvent(f)
	case attach.TBye:
		c.rev++
		c.Live = false
		c.Status = "detached (" + f.Reason + ")"
	case attach.TError:
		c.rev++
		c.Items = append(c.Items, Item{Kind: ItemNote, Text: "protocol error: " + f.Code + " " + f.Message})
	}
}

func (c *Conversation) foldEvent(f attach.Frame) {
	payload := f.Payload
	str := func(k string) string { s, _ := payload[k].(string); return s }
	seq := 0
	if f.Seq != nil {
		seq = *f.Seq
	}
	add := func(it Item) {
		it.Seq = seq
		c.Items = append(c.Items, it)
	}

	switch f.Kind {
	case "message_agent":
		c.sb.Reset()
		c.Streaming = ""
		add(Item{Kind: ItemAgent, Text: str("text")})
	case "message_user":
		add(Item{Kind: ItemUser, Text: str("text"), Principal: str("principal")})
	case "tool_call":
		add(Item{Kind: ItemTool, Text: str("tool"), Detail: str("decision")})
	case "tool_result":
		// Attach the result to its call so the card is expandable; a
		// result with no prior call (trimmed replay) becomes a note.
		out := str("output")
		if out == "" {
			out = str("summary")
		}
		for i := len(c.Items) - 1; i >= 0; i-- {
			if c.Items[i].Kind == ItemTool && c.Items[i].Detail == "" {
				c.Items[i].Detail = out
				return
			}
		}
		if out != "" {
			add(Item{Kind: ItemNote, Text: "tool result", Detail: out})
		}
	case "approval_requested":
		req := str("requestId")
		c.Pending = append(c.Pending, Approval{RequestID: req, Tool: str("tool")})
		add(Item{Kind: ItemNote, Text: "approval needed: " + str("tool"), Detail: req})
	case "approval_resolved":
		req := str("requestId")
		c.Pending = removeApproval(c.Pending, req)
		verdict := "denied"
		if b, _ := payload["approved"].(bool); b {
			verdict = "approved"
		}
		by := str("principal")
		if by == "" {
			by = "—"
		}
		add(Item{Kind: ItemNote, Text: fmt.Sprintf("approval %s %s by %s", req, verdict, by)})
	case "artifact_produced":
		pr, _ := payload["pr"].(string)
		add(Item{Kind: ItemNote, Text: "artifact", Detail: pr})
	case "cost_sample":
		c.Tokens = fmt.Sprintf("%v tokens", payload["tokens"])
	case "state_changed":
		st := str("state")
		c.State = st
		add(Item{Kind: ItemNote, Text: "state: " + st})
		if st != "running" {
			c.Live = false
		}
	case "harness_event":
		if phase := str("phase"); phase != "" {
			add(Item{Kind: ItemNote, Text: "harness: " + phase})
		}
	case "error":
		add(Item{Kind: ItemNote, Text: "error: " + str("text")})
	case "child_spawned", "child_completed", "child_failed":
		add(Item{Kind: ItemNote, Text: strings.ReplaceAll(f.Kind, "_", " ") + ": " + str("sessionId")})
	}
}

func removeApproval(list []Approval, requestID string) []Approval {
	out := list[:0]
	for _, a := range list {
		if a.RequestID != requestID {
			out = append(out, a)
		}
	}
	return out
}

// Transcript renders the fold as plain text, one line per item — the
// golden format the parity tests pin. It is intentionally unstyled: the
// goldens assert fold semantics, not paint.
func (c *Conversation) Transcript() string {
	var b strings.Builder
	for _, it := range c.Items {
		switch it.Kind {
		case ItemAgent:
			b.WriteString("agent: " + it.Text)
		case ItemUser:
			b.WriteString("user(" + it.Principal + "): " + it.Text)
		case ItemTool:
			b.WriteString("tool: " + it.Text)
			if it.Detail != "" {
				b.WriteString(" [" + firstLine(it.Detail) + "]")
			}
		case ItemNote:
			b.WriteString("note: " + it.Text)
			if it.Detail != "" {
				b.WriteString(" [" + firstLine(it.Detail) + "]")
			}
		}
		b.WriteString("\n")
	}
	fmt.Fprintf(&b, "-- live=%v state=%s pending=%d latest=%d streaming=%q\n",
		c.Live, c.State, len(c.Pending), c.LatestSeq, c.Streaming)
	return b.String()
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
