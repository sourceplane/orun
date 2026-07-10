package agent

import (
	"context"
	"testing"
	"time"

	"github.com/sourceplane/orun/internal/agent/driver"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
)

// TestReplayReproducesTheConversation is the headline AL5 property: a sealed
// interactive session replays the WHOLE conversation — the human's attributed
// turns and the attributed approval resolution, not just the agent's half.
// This is what makes the live plane's interactivity part of the proof system
// rather than a side channel (design §8).
func TestReplayReproducesTheConversation(t *testing.T) {
	ctx := context.Background()
	store, refs := newStores(t)
	typeID := mustBlob(t, store, "implementer-type")
	brief, err := AssembleBrief(ctx, store, BriefInput{RunKind: nodes.RunKindInteractive})
	if err != nil {
		t.Fatal(err)
	}

	q := NewInputQueue()
	var approvalReq string
	// A goroutine plays the human: steer, then approve the gated tool, then end.
	go func() {
		// Wait for the session to be live enough to accept input.
		_ = q.Steer("implement the lease sweep", "usr_rahul")
		// The stub emits an approval when asked; drive that path.
		_ = q.Steer("/ask contract_propose", "usr_rahul")
		// Poll until the approval is pending (the stub assigns req-1).
		deadline := time.After(3 * time.Second)
		for {
			if err := q.Verdict("req-1", true, "scope looks right", "usr_priya"); err == nil {
				approvalReq = "req-1"
				break
			}
			select {
			case <-deadline:
				return
			case <-time.After(10 * time.Millisecond):
			}
		}
		_ = q.Steer("/done", "usr_rahul")
	}()

	res, err := Run(ctx, store, RunOptions{
		SessionID: "as_convo",
		Driver:    &driver.Stub{Interactive: true},
		Brief:     brief,
		Inputs:    q,
		Refs:      refs,
		Seal:      &SealInput{RunKind: nodes.RunKindInteractive, AgentType: typeID, Principal: "usr_rahul"},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if approvalReq == "" {
		t.Fatal("the approval was never resolved (the human goroutine timed out)")
	}
	if res.SnapshotID == "" {
		t.Fatal("interactive session did not seal")
	}

	// Replay from content alone.
	_, lines, err := Replay(ctx, store, objectstore.ObjectID(res.SnapshotID))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}

	var userTurn, resolution bool
	var userAttrib, verdictAttrib string
	for _, l := range lines {
		switch l.Kind {
		case nodes.SessionEventMessageUser:
			if l.Payload["text"] == "implement the lease sweep" {
				userTurn = true
				userAttrib, _ = l.Payload["principal"].(string)
			}
		case nodes.SessionEventApprovalResolved:
			if l.Payload["requestId"] == "req-1" {
				resolution = true
				verdictAttrib, _ = l.Payload["principal"].(string)
			}
		}
	}
	if !userTurn || userAttrib != "usr_rahul" {
		t.Fatalf("replay missing the attributed user turn: seen=%v attrib=%q", userTurn, userAttrib)
	}
	if !resolution || verdictAttrib != "usr_priya" {
		t.Fatalf("replay missing the attributed approval: seen=%v attrib=%q", resolution, verdictAttrib)
	}
	// The honesty invariant survives replay: no status/lifecycle kind ever
	// appears, even with a full conversation folded in.
	for _, l := range lines {
		if l.Kind == "status_asserted" || l.Kind == "lifecycle_set" {
			t.Fatalf("forbidden event kind in replayed conversation: %s", l.Kind)
		}
	}
}
