package work

import (
	"errors"
	"testing"
)

// TestAllMutatorsRejectInvalidActor asserts the one-write-path actor guard
// (invariant 4) fires on every mutator: an empty actor never produces an event.
func TestAllMutatorsRejectInvalidActor(t *testing.T) {
	st := mustState(t)
	// Seed an entity so the mutators reach (and fail at) the actor check rather
	// than a not-found check — the actor guard is first in every mutator, so a
	// fresh subject is fine, but a real one proves order.
	if _, err := st.CreateTask("seed", ItemOptions{}, human, at0); err != nil {
		t.Fatal(err)
	}
	none := Actor{}
	mutators := map[string]func() (WorkEvent, error){
		"CreateTask":       func() (WorkEvent, error) { return st.CreateTask("t", ItemOptions{}, none, at0) },
		"CreateEpic":       func() (WorkEvent, error) { return st.CreateEpic("e", "t", ItemOptions{}, none, at0) },
		"CreateInitiative": func() (WorkEvent, error) { return st.CreateInitiative("i", "t", ItemOptions{}, none, at0) },
		"EditItem":         func() (WorkEvent, error) { return st.EditItem("ORN-1", strptr("x"), nil, none, at0) },
		"SetStatus":        func() (WorkEvent, error) { return st.SetStatus("ORN-1", StatusTodo, nil, none, at0) },
		"Assign":           func() (WorkEvent, error) { return st.Assign("ORN-1", "prn_a", none, at0) },
		"Unassign":         func() (WorkEvent, error) { return st.Unassign("ORN-1", "prn_a", none, at0) },
		"AddComment":       func() (WorkEvent, error) { return st.AddComment("ORN-1", "c", none, at0) },
		"EditContract":     func() (WorkEvent, error) { return st.EditContract("ORN-1", &Contract{}, none, at0) },
		"AddLink": func() (WorkEvent, error) {
			return st.AddLink(Link{From: "ORN-1", Type: LinkBlocks, To: "ORN-2"}, none, at0)
		},
		"RemoveLink": func() (WorkEvent, error) {
			return st.RemoveLink(Link{From: "ORN-1", Type: LinkBlocks, To: "ORN-2"}, none, at0)
		},
		"Move":     func() (WorkEvent, error) { return st.Move("ORN-1", strptr("p"), nil, none, at0) },
		"SetCycle": func() (WorkEvent, error) { return st.SetCycle("ORN-1", "c", none, at0) },
		"Label":    func() (WorkEvent, error) { return st.Label("ORN-1", "k", "v", none, at0) },
		"Unlabel":  func() (WorkEvent, error) { return st.Unlabel("ORN-1", "k", none, at0) },
		"Cancel":   func() (WorkEvent, error) { return st.Cancel("ORN-1", "r", none, at0) },
		"Seal":     func() (WorkEvent, error) { return st.Seal("ORN-1", "sha256:x", "ref", 1, none, at0) },
		"Import": func() (WorkEvent, error) {
			return st.Import(Item{Kind: KindTask, Key: "ORN-2", Title: "t"}, "s", none, at0)
		},
	}
	startSeq := st.NextSeq()
	for name, fn := range mutators {
		if _, err := fn(); !errors.Is(err, ErrMissingActor) {
			t.Errorf("%s with empty actor: err = %v, want ErrMissingActor", name, err)
		}
	}
	if st.NextSeq() != startSeq {
		t.Fatalf("a rejected mutation advanced the sequence (%d -> %d)", startSeq, st.NextSeq())
	}
}

func TestAssignDeduplicatesAndSorts(t *testing.T) {
	st := mustState(t)
	if _, err := st.CreateTask("t", ItemOptions{}, human, at0); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"prn_c", "prn_a", "prn_c", "prn_b"} {
		if _, err := st.Assign("ORN-1", p, human, at0); err != nil {
			t.Fatal(err)
		}
	}
	got := st.Status["ORN-1"].Assignees
	want := []string{"prn_a", "prn_b", "prn_c"}
	if len(got) != len(want) {
		t.Fatalf("assignees = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("assignees = %v, want sorted+deduped %v", got, want)
		}
	}
	// Unassigning the last assignee clears the slice to nil (not empty).
	for _, p := range want {
		if _, err := st.Unassign("ORN-1", p, human, at0); err != nil {
			t.Fatal(err)
		}
	}
	if st.Status["ORN-1"].Assignees != nil {
		t.Fatalf("assignees not cleared to nil: %v", st.Status["ORN-1"].Assignees)
	}
}

func TestRemoveLinkRejectsBadEdge(t *testing.T) {
	st := mustState(t)
	if _, err := st.RemoveLink(Link{From: "ORN-1", Type: "ghost", To: "x"}, human, at0); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("RemoveLink bad type: err = %v, want ErrInvalidLink", err)
	}
}

// TestImportTaskFromForeignPrefix exercises taskKeySeq's non-matching branch:
// an imported Task whose key does not match the project prefix must not move the
// local key counter.
func TestImportTaskFromForeignPrefix(t *testing.T) {
	st := mustState(t)
	if _, err := st.Import(Item{Kind: KindTask, Key: "XYZ-7", Title: "foreign"}, "specs/", auto, at0); err != nil {
		t.Fatal(err)
	}
	ev, err := st.CreateTask("local", ItemOptions{}, human, at0)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Subject != "ORN-1" {
		t.Fatalf("foreign-prefix import shifted the local counter: next task = %q, want ORN-1", ev.Subject)
	}
}
