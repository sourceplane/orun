package worklens

import (
	"strings"
	"testing"
)

func snapshotFixture() SpecSnapshot {
	spec := Spec{APIVersion: APIVersion, Kind: KindSpec, Key: "demo-epic", Workspace: "ws_1", Title: "Demo", DocRef: "sha256:doc", CreatedBy: Actor{Type: ActorUser, ID: "usr_1"}, CreatedAt: "2026-07-04T12:00:00Z"}
	tasks := []Task{
		{APIVersion: APIVersion, Kind: KindTask, Key: "ORN-2", Workspace: "ws_1", Spec: "demo-epic", Title: "b", Contract: &Contract{Goal: "g", Affects: []string{"a/b/c"}, DoneWhen: []string{"d"}, Gates: []string{"tests"}}, CreatedBy: Actor{Type: ActorUser, ID: "usr_1"}},
		{APIVersion: APIVersion, Kind: KindTask, Key: "ORN-1", Workspace: "ws_1", Spec: "demo-epic", Title: "a", CreatedBy: Actor{Type: ActorUser, ID: "usr_1"}},
	}
	return NewSpecSnapshot(spec, tasks, "sha256:catalog", 42, 7)
}

func TestSealDeterminism(t *testing.T) {
	a := snapshotFixture()
	b := snapshotFixture()
	idA, bytesA, err := SealSpecSnapshot(a)
	if err != nil {
		t.Fatal(err)
	}
	idB, bytesB, err := SealSpecSnapshot(b)
	if err != nil {
		t.Fatal(err)
	}
	if idA != idB || string(bytesA) != string(bytesB) {
		t.Fatal("resealing identical inputs is not byte-identical (invariant 7)")
	}
	if !strings.HasPrefix(idA, "sha256:") || len(idA) != 7+64 {
		t.Fatalf("content id shape: %s", idA)
	}

	// Task input order must not shift the id.
	c := snapshotFixture()
	c.Tasks[0], c.Tasks[1] = c.Tasks[1], c.Tasks[0]
	idC, _, err := ContentID(c)
	if err != nil {
		t.Fatal(err)
	}
	if idC != idA {
		// NewSpecSnapshot sorts; a hand-shuffled snapshot differs — assert
		// the constructor's ordering is what sealed above.
		rebuilt := NewSpecSnapshot(c.Spec, c.Tasks, c.Catalog, c.CoordSeq, c.ObsSeq)
		idR, _, _ := ContentID(rebuilt)
		if idR != idA {
			t.Fatal("constructor ordering is not canonical")
		}
	}

	// Any input change shifts the id.
	d := snapshotFixture()
	d.CoordSeq = 43
	idD, _, err := ContentID(d)
	if err != nil {
		t.Fatal(err)
	}
	if idD == idA {
		t.Fatal("changed input sealed to the same id")
	}
}

func TestSnapshotCarriesNoHotState(t *testing.T) {
	_, b, err := SealSpecSnapshot(snapshotFixture())
	if err != nil {
		t.Fatal(err)
	}
	for _, tok := range []string{"\"rung\"", "\"lifecycle\"", "\"assignees\"", "\"pinned\"", "\"status\""} {
		if strings.Contains(string(b), tok) {
			t.Fatalf("snapshot bytes carry %s — invariant 1", tok)
		}
	}
	// Escaped text inside a string field is inert data, not hot state —
	// the guard must NOT trip on it (it scans structural keys only).
	inert := snapshotFixture()
	inert.Spec.Title = `{"rung":"done"}`
	if _, _, err := SealSpecSnapshot(inert); err != nil {
		t.Fatalf("guard tripped on inert string content: %v", err)
	}
	// The structural guard itself: a raw canonical body carrying a rung key.
	if !containsToken([]byte(`{"rung":"done"}`), `"rung"`) {
		t.Fatal("structural hot-state token not detected")
	}
}

func TestSegmentChains(t *testing.T) {
	seg1 := CoordinationSegment{Kind: "WorkCoordinationSegment", APIVersion: APIVersion, Workspace: "ws_1", FromSeq: 1, ToSeq: 2, Events: []CoordinationEvent{
		{Workspace: "ws_1", Subject: "ORN-1", Kind: EventItemCreated, Actor: Actor{Type: ActorUser, ID: "u"}, At: "2026-07-04T12:00:00Z", Seq: 1},
		{Workspace: "ws_1", Subject: "ORN-1", Kind: EventCommentAdded, Actor: Actor{Type: ActorUser, ID: "u"}, At: "2026-07-04T12:01:00Z", Seq: 2},
	}}
	id1, _, err := ContentID(seg1)
	if err != nil {
		t.Fatal(err)
	}
	seg2 := CoordinationSegment{Kind: "WorkCoordinationSegment", APIVersion: APIVersion, Workspace: "ws_1", FromSeq: 3, ToSeq: 3, Prev: id1, Events: []CoordinationEvent{
		{Workspace: "ws_1", Subject: "ORN-1", Kind: EventCanceled, Actor: Actor{Type: ActorUser, ID: "u"}, At: "2026-07-04T12:02:00Z", Seq: 3},
	}}
	id2, _, err := ContentID(seg2)
	if err != nil {
		t.Fatal(err)
	}
	// Tampering with the chain shifts every downstream id.
	tampered := seg2
	tampered.Prev = "sha256:0000000000000000000000000000000000000000000000000000000000000000"
	idT, _, err := ContentID(tampered)
	if err != nil {
		t.Fatal(err)
	}
	if idT == id2 {
		t.Fatal("prev-chain tamper did not shift the id (tamper evidence broken)")
	}
}

func TestCanonicalJSONSortsKeys(t *testing.T) {
	b, err := CanonicalJSON(map[string]interface{}{"b": 1, "a": map[string]interface{}{"z": true, "y": []int{2, 1}}})
	if err != nil {
		t.Fatal(err)
	}
	want := `{"a":{"y":[2,1],"z":true},"b":1}`
	if string(b) != want {
		t.Fatalf("canonical = %s, want %s", b, want)
	}
}
