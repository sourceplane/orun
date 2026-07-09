package agent

import (
	"context"
	"fmt"

	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

// seal.go — AG4 session sealing + replay (specs/orun-agents/data-model.md §3).
// On terminal state a run seals an AgentSessionSnapshot pinning every input by
// hash (agent type, brief, catalog) plus its sealed segment chain; the ref
// refs/agents/sessions/<sessionId> makes the run discoverable and replayable.

// SessionRef is the ref that tracks a sealed session.
func SessionRef(sessionID string) string { return "agents/sessions/" + sessionID }

// SealInput carries the run identity a session snapshot pins.
type SealInput struct {
	SessionID string
	RunKind   string
	AgentType string // AgentTypeSnapshot id (frozen at dispatch)
	Brief     string // AgentBrief id
	WorkRef   string // work://… pointer (optional)
	Catalog   string // CatalogSnapshot id (optional)
	Principal string // sp_… (cloud) or usr_… (local)
	Segments  []string
	Outcome   nodes.AgentOutcome
}

// SealSession writes the AgentSessionSnapshot and points refs/agents/sessions/
// <sessionId> at it. Idempotent: an identical run re-seals to the same id.
func SealSession(ctx context.Context, store objectstore.ObjectStore, refs refstore.RefStore, in SealInput) (objectstore.ObjectID, error) {
	snap := nodes.AgentSessionSnapshot{
		Kind:       nodes.KindAgentSessionSnapshot,
		APIVersion: "orun.io/v1",
		SessionID:  in.SessionID,
		RunKind:    in.RunKind,
		AgentType:  in.AgentType,
		Brief:      in.Brief,
		WorkRef:    in.WorkRef,
		Catalog:    in.Catalog,
		Principal:  in.Principal,
		Segments:   in.Segments,
	}
	if in.Outcome.Status != "" {
		snap.Outcome = &in.Outcome
	}
	id, err := nodes.AssembleAgentSession(ctx, store, snap)
	if err != nil {
		return "", err
	}
	name := SessionRef(in.SessionID)
	cur, rErr := refs.Read(ctx, name)
	switch {
	case rErr == nil:
		if cur.Target != string(id) {
			if err := refs.Update(ctx, name, cur.Target, string(id)); err != nil {
				return "", fmt.Errorf("agent: move session ref: %w", err)
			}
		}
	default:
		if err := refs.Update(ctx, name, "", string(id)); err != nil {
			return "", fmt.Errorf("agent: create session ref: %w", err)
		}
	}
	return id, nil
}

// ReplayLine is one rendered transcript line reconstructed from a sealed
// session — content alone, no live process.
type ReplayLine struct {
	Seq     int
	Kind    string
	Payload map[string]any
}

// Replay reconstructs a session's transcript from its sealed snapshot by
// resolving and folding its segment chain in order. Byte-for-byte deterministic
// because it reads only content.
func Replay(ctx context.Context, store objectstore.ObjectStore, snapshotID objectstore.ObjectID) (nodes.AgentSessionSnapshot, []ReplayLine, error) {
	_, body, err := store.Get(ctx, snapshotID)
	if err != nil {
		return nodes.AgentSessionSnapshot{}, nil, err
	}
	snap, err := nodes.Decode[nodes.AgentSessionSnapshot](body)
	if err != nil {
		return nodes.AgentSessionSnapshot{}, nil, err
	}
	var lines []ReplayLine
	for _, segID := range snap.Segments {
		_, sb, err := store.Get(ctx, objectstore.ObjectID(segID))
		if err != nil {
			return snap, nil, fmt.Errorf("agent replay: segment %s: %w", segID, err)
		}
		seg, err := nodes.Decode[nodes.AgentSessionSegment](sb)
		if err != nil {
			return snap, nil, err
		}
		for _, e := range seg.Entries {
			lines = append(lines, ReplayLine{Seq: e.Seq, Kind: e.Kind, Payload: e.Payload})
		}
	}
	return snap, lines, nil
}
