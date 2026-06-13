package work

import (
	"errors"
	"strings"
	"testing"
)

func sampleEpic() Item {
	return Item{
		APIVersion: APIVersion, Kind: KindEpic, ID: "epc_1", Key: "orun-work",
		Project: "acme/platform", Title: "Orun Work", Doc: "# spec body",
		CreatedBy: Actor{Type: ActorUser, ID: "prn_h"}, CreatedAt: "2026-06-11T09:00:00Z",
	}
}

func sampleTasks() []Item {
	return []Item{
		{
			APIVersion: APIVersion, Kind: KindTask, ID: "tsk_1", Key: "ORN-1",
			Project: "acme/platform", Title: "Route catalog reads",
			Contract:  &Contract{Goal: "g", Affects: []string{"sourceplane/orun/api-edge"}, DoneWhen: []string{"x"}, Gates: []string{"tests"}},
			CreatedBy: Actor{Type: ActorUser, ID: "prn_h"}, CreatedAt: "2026-06-11T09:00:00Z",
		},
	}
}

func sampleLinks() []Link {
	return []Link{{Project: "acme/platform", From: "ORN-1", FromKind: "Task", Type: LinkAffects, To: "sourceplane/orun/api-edge", ToKind: "Component", CreatedBy: Actor{Type: ActorUser, ID: "prn_h"}, CreatedAt: "2026-06-11T09:00:00Z"}}
}

// TestSealSpecSnapshotIsDeterministic is invariant 6: sealing the same inputs
// twice with no intervening events is byte-identical, so the ObjectID is stable.
func TestSealSpecSnapshotIsDeterministic(t *testing.T) {
	a, err := SealSpecSnapshot(sampleEpic(), sampleTasks(), sampleLinks(), "sha256:cat", 42)
	if err != nil {
		t.Fatal(err)
	}
	b, err := SealSpecSnapshot(sampleEpic(), sampleTasks(), sampleLinks(), "sha256:cat", 42)
	if err != nil {
		t.Fatal(err)
	}
	ida, err := a.ObjectID()
	if err != nil {
		t.Fatal(err)
	}
	idb, err := b.ObjectID()
	if err != nil {
		t.Fatal(err)
	}
	if ida != idb {
		t.Fatalf("reseal not byte-identical: %s vs %s", ida, idb)
	}
	if !strings.HasPrefix(ida, "sha256:") || len(ida) != len("sha256:")+64 {
		t.Fatalf("ObjectID malformed: %q", ida)
	}
	// A changed input shifts the id.
	c, _ := SealSpecSnapshot(sampleEpic(), sampleTasks(), sampleLinks(), "sha256:cat", 43)
	idc, _ := c.ObjectID()
	if idc == ida {
		t.Fatal("ledgerSeq change did not shift the ObjectID")
	}
}

// TestSpecSnapshotHasNoHotState is invariant 1: a sealed snapshot carries no
// status/assignees/board ordering — only envelopes and links.
func TestSpecSnapshotHasNoHotState(t *testing.T) {
	s, err := SealSpecSnapshot(sampleEpic(), sampleTasks(), sampleLinks(), "", 1)
	if err != nil {
		t.Fatal(err)
	}
	b, err := Canonical(s)
	if err != nil {
		t.Fatal(err)
	}
	body := string(b)
	for _, hot := range []string{"\"status\"", "\"assignees\"", "\"boardOrder\"", "\"updatedSeq\""} {
		if strings.Contains(body, hot) {
			t.Errorf("sealed snapshot leaked hot state %s: %s", hot, body)
		}
	}
}

func TestSealSpecSnapshotRejectsWrongKinds(t *testing.T) {
	task := sampleTasks()[0]
	if _, err := SealSpecSnapshot(task, nil, nil, "", 0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("non-Epic head: err = %v, want ErrInvalidArgument", err)
	}
	if _, err := SealSpecSnapshot(sampleEpic(), []Item{sampleEpic()}, nil, "", 0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("non-Task member: err = %v, want ErrInvalidArgument", err)
	}
}

func TestSealLedgerSegmentChainAndValidation(t *testing.T) {
	evs := []WorkEvent{
		{EventID: "wev_1", Project: "acme/platform", Subject: "ORN-1", Kind: EventItemCreated, Actor: Actor{Type: ActorUser, ID: "prn_h"}, At: "2026-06-11T09:00:00Z", Seq: 10},
		{EventID: "wev_2", Project: "acme/platform", Subject: "ORN-1", Kind: EventStatusChanged, Actor: Actor{Type: ActorUser, ID: "prn_h"}, At: "2026-06-11T09:00:01Z", Seq: 11},
	}
	seg1, err := SealLedgerSegment("acme/platform", 10, 11, evs, "")
	if err != nil {
		t.Fatal(err)
	}
	id1, err := seg1.ObjectID()
	if err != nil {
		t.Fatal(err)
	}
	// The next segment chains on the prior's id; changing prev shifts the id
	// (tamper-evidence).
	seg2, err := SealLedgerSegment("acme/platform", 12, 12, []WorkEvent{{EventID: "wev_3", Project: "acme/platform", Subject: "ORN-1", Kind: EventSealed, Actor: Actor{Type: ActorAutomation, ID: "seal"}, At: "2026-06-11T09:00:02Z", Seq: 12}}, id1)
	if err != nil {
		t.Fatal(err)
	}
	seg2alt, _ := SealLedgerSegment("acme/platform", 12, 12, seg2.Events, "sha256:tampered")
	ida, _ := seg2.ObjectID()
	idb, _ := seg2alt.ObjectID()
	if ida == idb {
		t.Fatal("prev change did not shift the segment id (chain not tamper-evident)")
	}

	// Validation: out-of-range seq, reversed range, non-increasing.
	if _, err := SealLedgerSegment("acme/platform", 10, 11, []WorkEvent{{Seq: 99}}, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("out-of-range seq: err = %v", err)
	}
	if _, err := SealLedgerSegment("acme/platform", 11, 10, nil, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("reversed range: err = %v", err)
	}
	if _, err := SealLedgerSegment("acme/platform", 10, 12, []WorkEvent{{Seq: 11}, {Seq: 11}}, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("non-increasing: err = %v", err)
	}
	if _, err := SealLedgerSegment("", 0, 0, nil, ""); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty project: err = %v", err)
	}
}

func TestContentIDStableAndErrors(t *testing.T) {
	id1, err := ContentID(map[string]any{"b": 1, "a": 2})
	if err != nil {
		t.Fatal(err)
	}
	id2, err := ContentID(map[string]any{"a": 2, "b": 1})
	if err != nil {
		t.Fatal(err)
	}
	if id1 != id2 {
		t.Fatalf("ContentID not order-invariant: %s vs %s", id1, id2)
	}
	if _, err := ContentID(make(chan int)); err == nil {
		t.Error("ContentID(unmarshalable) should error")
	}
}
