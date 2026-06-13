package work

import (
	"errors"
	"testing"
	"time"
)

func strptr(s string) *string { return &s }

func mustState(t *testing.T) *State {
	t.Helper()
	s, err := NewState("acme/platform", "ORN")
	if err != nil {
		t.Fatalf("NewState: %v", err)
	}
	return s
}

var (
	human = Actor{Type: ActorUser, ID: "prn_human", Via: "ui"}
	auto  = Actor{Type: ActorAutomation, ID: "bridge/pr-linker", Via: "github-webhook"}
	at0   = time.Date(2026, 6, 11, 9, 0, 0, 0, time.UTC)
)

// TestReplayReproducesProjection is the W0 invariant-2 proof: a run that
// exercises every event kind, then dropping the projection and replaying the
// log via Reduce, yields a byte-for-byte identical projection. It doubles as
// the "mutator fixtures cover the closed event-kind set" check.
func TestReplayReproducesProjection(t *testing.T) {
	st := mustState(t)
	var log []WorkEvent
	add := func(ev WorkEvent, err error) {
		t.Helper()
		if err != nil {
			t.Fatalf("mutator: %v", err)
		}
		log = append(log, ev)
	}

	add(st.CreateInitiative("portal-ga", "Portal GA", ItemOptions{}, human, at0))
	add(st.CreateEpic("orun-work", "Orun Work", ItemOptions{Doc: "# spec body"}, human, at0))
	contract := &Contract{
		Goal:     "Route catalog reads through the objcatalog view",
		Affects:  []string{"sourceplane/orun/api-edge"},
		DoneWhen: []string{"parity green"},
		Gates:    []string{"tests"},
	}
	add(st.CreateTask("Route catalog reads", ItemOptions{
		Parent:   "acme/platform/epics/orun-work",
		Contract: contract,
	}, human, at0))

	if _, ok := st.Items["ORN-1"]; !ok {
		t.Fatalf("expected task key ORN-1, items=%v", keysOf(st.Items))
	}

	add(st.EditItem("ORN-1", strptr("Route catalog reads (v2)"), nil, human, at0))
	add(st.SetStatus("ORN-1", StatusInProgress, nil, human, at0))
	add(st.Assign("ORN-1", "prn_a", human, at0))
	add(st.Assign("ORN-1", "prn_b", human, at0))
	add(st.Unassign("ORN-1", "prn_a", human, at0))
	add(st.AddComment("ORN-1", "looks good", human, at0))
	add(st.AddLink(Link{From: "ORN-1", FromKind: "Task", Type: LinkImplementedBy, To: "sourceplane/orun#412", ToKind: "pr"}, auto, at0))
	add(st.RemoveLink(Link{From: "ORN-1", Type: LinkImplementedBy, To: "sourceplane/orun#412"}, auto, at0))
	add(st.EditContract("ORN-1", &Contract{Goal: "narrowed goal"}, human, at0))
	bo := 1.5
	add(st.Move("ORN-1", strptr("acme/platform/epics/orun-work"), &bo, human, at0))
	add(st.SetCycle("ORN-1", "2026-W24", human, at0))
	add(st.Label("ORN-1", "area", "catalog", human, at0))
	add(st.Unlabel("ORN-1", "area", human, at0))
	add(st.Seal("orun-work", "sha256:abc123", "refs/work/epics/orun-work/latest", 99, auto, at0))
	add(st.Import(Item{Kind: KindTask, Key: "ORN-99", Title: "Imported task", Project: "acme/platform"}, "specs/", auto, at0))
	add(st.Cancel("ORN-1", "superseded", human, at0))

	// Every kind in the closed set must have been produced.
	seen := map[EventKind]bool{}
	for _, e := range log {
		seen[e.Kind] = true
	}
	for k := range EventKinds {
		if !seen[k] {
			t.Errorf("event kind %q was never exercised by the fixtures", k)
		}
	}

	// Replay the log into a fresh projection and compare byte-for-byte.
	replayed, err := Reduce("acme/platform", "ORN", log)
	if err != nil {
		t.Fatalf("Reduce: %v", err)
	}
	assertCanonicalEqual(t, "status", st.Status, replayed.Status)
	assertCanonicalEqual(t, "items", st.Items, replayed.Items)
	assertCanonicalEqual(t, "links", st.Links, replayed.Links)

	// Spot-check the derived state is what we expect.
	row := replayed.Status["ORN-1"]
	if row.Status != StatusCanceled {
		t.Errorf("ORN-1 status = %q, want canceled", row.Status)
	}
	if len(row.Assignees) != 1 || row.Assignees[0] != "prn_b" {
		t.Errorf("ORN-1 assignees = %v, want [prn_b]", row.Assignees)
	}
	if row.BoardOrder != 1.5 {
		t.Errorf("ORN-1 boardOrder = %v, want 1.5", row.BoardOrder)
	}
	if it := replayed.Items["ORN-1"]; it.Cycle != "2026-W24" || it.Labels != nil || it.Contract.Goal != "narrowed goal" {
		t.Errorf("ORN-1 envelope not as expected: %+v", it)
	}
}

// TestSeqAssignmentIsMonotonicAndOnePerMutation asserts each mutator appends
// exactly one event (invariant 3) and seq advances by one each time.
func TestSeqAssignmentIsMonotonicAndOnePerMutation(t *testing.T) {
	st := mustState(t)
	ev, err := st.CreateTask("first", ItemOptions{}, human, at0)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Seq != 1 {
		t.Fatalf("first event seq = %d, want 1", ev.Seq)
	}
	before := st.NextSeq()
	if _, err := st.SetStatus("ORN-1", StatusTodo, nil, human, at0); err != nil {
		t.Fatal(err)
	}
	if got := st.NextSeq(); got != before+1 {
		t.Fatalf("NextSeq advanced by %d, want 1", got-before)
	}
}

// TestEventWithoutActorRejected covers the W0 "an event without an actor is
// rejected" requirement, on both the mutator path and the replay path.
func TestEventWithoutActorRejected(t *testing.T) {
	st := mustState(t)
	// Mutator path: an empty actor never commits.
	if _, err := st.CreateTask("x", ItemOptions{}, Actor{}, at0); !errors.Is(err, ErrMissingActor) {
		t.Fatalf("CreateTask with empty actor: err = %v, want ErrMissingActor", err)
	}
	if st.NextSeq() != 1 {
		t.Fatalf("a rejected mutation advanced the sequence: NextSeq = %d", st.NextSeq())
	}
	// Replay path: a hand-built actor-less event fails validation.
	bad := WorkEvent{Project: "acme/platform", Subject: "ORN-1", Kind: EventStatusChanged, At: "2026-06-11T09:00:00Z"}
	if err := bad.Validate(); !errors.Is(err, ErrMissingActor) {
		t.Fatalf("Validate actor-less event: err = %v, want ErrMissingActor", err)
	}
	if _, err := Reduce("acme/platform", "ORN", []WorkEvent{bad}); err == nil {
		t.Fatal("Reduce accepted an actor-less event")
	}
}

func TestReduceRejectsUnknownKindAndMissingSubject(t *testing.T) {
	unknown := WorkEvent{Project: "acme/platform", Subject: "ORN-1", Kind: "teleported", Actor: human}
	if err := unknown.Validate(); !errors.Is(err, ErrUnknownEventKind) {
		t.Fatalf("unknown kind: err = %v, want ErrUnknownEventKind", err)
	}
	noSubject := WorkEvent{Project: "acme/platform", Kind: EventCommentAdded, Actor: human}
	if err := noSubject.Validate(); !errors.Is(err, ErrInvalidEvent) {
		t.Fatalf("missing subject: err = %v, want ErrInvalidEvent", err)
	}
}

func TestReduceRejectsEventForMissingEntity(t *testing.T) {
	ev := WorkEvent{
		Project: "acme/platform", Subject: "ORN-7", Kind: EventStatusChanged,
		Actor: human, At: "2026-06-11T09:00:00Z",
		Payload: mustMarshal(statusChangedPayload{From: StatusBacklog, To: StatusTodo}),
		Seq:     1,
	}
	if _, err := Reduce("acme/platform", "ORN", []WorkEvent{ev}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Reduce status_changed for absent entity: err = %v, want ErrNotFound", err)
	}
}

func assertCanonicalEqual(t *testing.T, label string, a, b any) {
	t.Helper()
	eq, err := CanonicalEqual(a, b)
	if err != nil {
		t.Fatalf("%s: CanonicalEqual: %v", label, err)
	}
	if !eq {
		ab, _ := Canonical(a)
		bb, _ := Canonical(b)
		t.Fatalf("%s: projection diverged on replay\n live:    %s\n replay:  %s", label, ab, bb)
	}
}

func keysOf[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
