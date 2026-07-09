package agent

import (
	"context"
	"testing"

	"github.com/sourceplane/orun/internal/agent/driver"
	"github.com/sourceplane/orun/internal/nodes"
	"github.com/sourceplane/orun/internal/objectstore"
	"github.com/sourceplane/orun/internal/objectstore/refstore"
)

func newStores(t *testing.T) (*objectstore.MemStore, refstore.RefStore) {
	t.Helper()
	refs, err := refstore.NewLocalRefStore(refstore.LocalConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	return objectstore.NewMemStore(objectstore.AlgoSHA256), refs
}

func TestRunSealsSessionAndReplays(t *testing.T) {
	ctx := context.Background()
	store, refs := newStores(t)

	// A pinned agent type + brief stand in for the run identity.
	typeID := mustBlob(t, store, "agent-type")
	brief, err := AssembleBrief(ctx, store, BriefInput{RunKind: nodes.RunKindImplementation, Task: "ORN-42"})
	if err != nil {
		t.Fatal(err)
	}

	res, err := Run(ctx, store, RunOptions{
		SessionID: "as_seal1",
		Driver:    &driver.Stub{},
		Brief:     brief,
		Branch:    "agent/ORN-42-x",
		Refs:      refs,
		Seal: &SealInput{
			RunKind:   nodes.RunKindImplementation,
			AgentType: typeID,
			Principal: "usr_rahul",
		},
	})
	if err != nil {
		t.Fatalf("run: %v", err)
	}
	if res.SnapshotID == "" {
		t.Fatal("run did not seal a session snapshot")
	}
	// The session ref points at the snapshot.
	ref, err := refs.Read(ctx, SessionRef("as_seal1"))
	if err != nil || ref.Target != res.SnapshotID {
		t.Fatalf("session ref = %v (%v), want %s", ref, err, res.SnapshotID)
	}

	// Replay reconstructs the transcript from content alone.
	snap, lines, err := Replay(ctx, store, objectstore.ObjectID(res.SnapshotID))
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if snap.AgentType != typeID || snap.Brief != brief.ID || snap.Principal != "usr_rahul" {
		t.Fatalf("snapshot identity not pinned: %+v", snap)
	}
	if snap.Outcome == nil || snap.Outcome.Status != "completed" || snap.Outcome.PR == "" {
		t.Fatalf("outcome not sealed: %+v", snap.Outcome)
	}
	var sawArtifact, sawRunning bool
	for _, l := range lines {
		if l.Kind == nodes.SessionEventArtifactProduced {
			sawArtifact = true
		}
		if l.Kind == nodes.SessionEventStateChanged {
			if s, _ := l.Payload["state"].(string); s == "running" {
				sawRunning = true
			}
		}
	}
	if !sawArtifact || !sawRunning {
		t.Fatalf("replayed transcript incomplete: artifact=%v running=%v (%d lines)", sawArtifact, sawRunning, len(lines))
	}
}

func TestSealSessionIdempotent(t *testing.T) {
	ctx := context.Background()
	store, refs := newStores(t)
	typeID := mustBlob(t, store, "t")
	briefID := mustBlob(t, store, "b")
	in := SealInput{
		SessionID: "as_idem",
		RunKind:   nodes.RunKindDesign,
		AgentType: typeID,
		Brief:     briefID,
		Outcome:   nodes.AgentOutcome{Status: "completed"},
	}
	id1, err := SealSession(ctx, store, refs, in)
	if err != nil {
		t.Fatal(err)
	}
	id2, err := SealSession(ctx, store, refs, in)
	if err != nil || id1 != id2 {
		t.Fatalf("re-seal not idempotent: %s vs %s (%v)", id1, id2, err)
	}
}

// TestProvenanceWalk proves the chain: from a sealed session, walk back to the
// exact agent type and brief (data-model.md §7). This is the AG4 provenance
// property in miniature — the objects resolve by content hash alone.
func TestProvenanceWalk(t *testing.T) {
	ctx := context.Background()
	store, refs := newStores(t)
	typeID := mustBlob(t, store, "the-agent-type")
	brief, _ := AssembleBrief(ctx, store, BriefInput{RunKind: nodes.RunKindImplementation, Task: "ORN-7"})

	res, err := Run(ctx, store, RunOptions{
		SessionID: "as_prov",
		Driver:    &driver.Stub{},
		Brief:     brief,
		Refs:      refs,
		Seal:      &SealInput{RunKind: nodes.RunKindImplementation, AgentType: typeID},
	})
	if err != nil {
		t.Fatal(err)
	}
	// Walk: session → brief → (its instructions blob resolves).
	snap, _, err := Replay(ctx, store, objectstore.ObjectID(res.SnapshotID))
	if err != nil {
		t.Fatal(err)
	}
	_, briefBody, err := store.Get(ctx, objectstore.ObjectID(snap.Brief))
	if err != nil {
		t.Fatalf("brief not resolvable from session: %v", err)
	}
	briefNode, err := nodes.Decode[nodes.AgentBrief](briefBody)
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := store.Get(ctx, objectstore.ObjectID(briefNode.Instructions)); err != nil {
		t.Fatalf("instructions not resolvable from brief: %v", err)
	}
	if _, _, err := store.Get(ctx, objectstore.ObjectID(snap.AgentType)); err != nil {
		t.Fatalf("agent type not resolvable from session: %v", err)
	}
}

func mustBlob(t *testing.T, s *objectstore.MemStore, data string) string {
	t.Helper()
	id, err := s.PutBlob(context.Background(), []byte(data))
	if err != nil {
		t.Fatal(err)
	}
	return string(id)
}
