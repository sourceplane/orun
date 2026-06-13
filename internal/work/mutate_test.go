package work

import (
	"errors"
	"testing"
)

func TestCreateItemErrors(t *testing.T) {
	st := mustState(t)

	if _, err := st.CreateTask("", ItemOptions{}, human, at0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty title: err = %v, want ErrInvalidArgument", err)
	}
	if _, err := st.CreateEpic("Bad Slug", "t", ItemOptions{}, human, at0); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("bad slug: err = %v, want ErrInvalidKey", err)
	}
	if _, err := st.CreateInitiative("UPPER", "t", ItemOptions{}, human, at0); !errors.Is(err, ErrInvalidKey) {
		t.Errorf("bad initiative slug: err = %v, want ErrInvalidKey", err)
	}
	if _, err := st.CreateEpic("epic-a", "t", ItemOptions{Contract: &Contract{}}, human, at0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("contract on epic: err = %v, want ErrInvalidArgument", err)
	}

	if _, err := st.CreateEpic("dup", "first", ItemOptions{}, human, at0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateEpic("dup", "second", ItemOptions{}, human, at0); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate key: err = %v, want ErrConflict", err)
	}
}

func TestCreateTaskAllocatesSequentialKeys(t *testing.T) {
	st := mustState(t)
	for i, want := range []string{"ORN-1", "ORN-2", "ORN-3"} {
		ev, err := st.CreateTask("t", ItemOptions{}, human, at0)
		if err != nil {
			t.Fatalf("create %d: %v", i, err)
		}
		if ev.Subject != want {
			t.Fatalf("task %d key = %q, want %q", i, ev.Subject, want)
		}
	}
}

func TestMutatorsRequireExistingEntity(t *testing.T) {
	st := mustState(t)
	cases := map[string]func() (WorkEvent, error){
		"EditItem":     func() (WorkEvent, error) { return st.EditItem("ORN-9", strptr("x"), nil, human, at0) },
		"SetStatus":    func() (WorkEvent, error) { return st.SetStatus("ORN-9", StatusTodo, nil, human, at0) },
		"Assign":       func() (WorkEvent, error) { return st.Assign("ORN-9", "prn_a", human, at0) },
		"Unassign":     func() (WorkEvent, error) { return st.Unassign("ORN-9", "prn_a", human, at0) },
		"AddComment":   func() (WorkEvent, error) { return st.AddComment("ORN-9", "c", human, at0) },
		"EditContract": func() (WorkEvent, error) { return st.EditContract("ORN-9", &Contract{}, human, at0) },
		"Move":         func() (WorkEvent, error) { return st.Move("ORN-9", strptr("p"), nil, human, at0) },
		"SetCycle":     func() (WorkEvent, error) { return st.SetCycle("ORN-9", "c", human, at0) },
		"Label":        func() (WorkEvent, error) { return st.Label("ORN-9", "k", "v", human, at0) },
		"Unlabel":      func() (WorkEvent, error) { return st.Unlabel("ORN-9", "k", human, at0) },
		"Cancel":       func() (WorkEvent, error) { return st.Cancel("ORN-9", "r", human, at0) },
		"Seal":         func() (WorkEvent, error) { return st.Seal("ORN-9", "sha256:x", "ref", 1, human, at0) },
	}
	for name, fn := range cases {
		if _, err := fn(); !errors.Is(err, ErrNotFound) {
			t.Errorf("%s on absent entity: err = %v, want ErrNotFound", name, err)
		}
	}
}

func TestMutatorArgumentValidation(t *testing.T) {
	st := mustState(t)
	if _, err := st.CreateTask("t", ItemOptions{}, human, at0); err != nil {
		t.Fatal(err)
	}

	if _, err := st.EditItem("ORN-1", nil, nil, human, at0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty edit: err = %v", err)
	}
	if _, err := st.SetStatus("ORN-1", "teleported", nil, human, at0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("bad status: err = %v", err)
	}
	if _, err := st.Assign("ORN-1", "", human, at0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty principal: err = %v", err)
	}
	if _, err := st.Unassign("ORN-1", "", human, at0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty unassign principal: err = %v", err)
	}
	if _, err := st.AddComment("ORN-1", "", human, at0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty comment: err = %v", err)
	}
	if _, err := st.Move("ORN-1", nil, nil, human, at0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty move: err = %v", err)
	}
	if _, err := st.Label("ORN-1", "", "v", human, at0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty label key: err = %v", err)
	}
	if _, err := st.Unlabel("ORN-1", "", human, at0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty unlabel key: err = %v", err)
	}
	if _, err := st.Seal("ORN-1", "", "ref", 1, human, at0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("empty seal object: err = %v", err)
	}
}

func TestEditContractRejectsNonTask(t *testing.T) {
	st := mustState(t)
	if _, err := st.CreateEpic("epic-a", "E", ItemOptions{}, human, at0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.EditContract("epic-a", &Contract{}, human, at0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("contract on epic: err = %v, want ErrInvalidArgument", err)
	}
}

func TestAddLinkValidation(t *testing.T) {
	st := mustState(t)
	if _, err := st.CreateTask("t", ItemOptions{}, human, at0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddLink(Link{From: "ORN-1", Type: "teleports", To: "x"}, human, at0); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("bad link type: err = %v, want ErrInvalidLink", err)
	}
	if _, err := st.AddLink(Link{From: "", Type: LinkBlockedBy, To: "ORN-2"}, human, at0); !errors.Is(err, ErrInvalidLink) {
		t.Errorf("missing from: err = %v, want ErrInvalidLink", err)
	}
	ev, err := st.AddLink(Link{From: "ORN-1", FromKind: "Task", Type: LinkBlockedBy, To: "ORN-2", ToKind: "Task"}, human, at0)
	if err != nil {
		t.Fatalf("valid link: %v", err)
	}
	if ev.Kind != EventLinkAdded || len(st.Links) != 1 {
		t.Fatalf("link not recorded: kind=%s links=%d", ev.Kind, len(st.Links))
	}
	// Re-adding the same identity is an idempotent upsert (still one row).
	if _, err := st.AddLink(Link{From: "ORN-1", FromKind: "Task", Type: LinkBlockedBy, To: "ORN-2", ToKind: "Task"}, human, at0); err != nil {
		t.Fatal(err)
	}
	if len(st.Links) != 1 {
		t.Fatalf("duplicate link identity created a second row: %d", len(st.Links))
	}
}

func TestImportValidation(t *testing.T) {
	st := mustState(t)
	if _, err := st.Import(Item{Kind: KindTask, Title: "no key"}, "specs/", auto, at0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("missing key: err = %v", err)
	}
	if _, err := st.Import(Item{Kind: "Widget", Key: "ORN-5", Title: "t"}, "specs/", auto, at0); !errors.Is(err, ErrInvalidArgument) {
		t.Errorf("bad kind: err = %v", err)
	}
	ev, err := st.Import(Item{Kind: KindEpic, Key: "imported-epic", Title: "Imported"}, "specs/", auto, at0)
	if err != nil {
		t.Fatalf("valid import: %v", err)
	}
	if ev.Kind != EventImported {
		t.Fatalf("import event kind = %s", ev.Kind)
	}
	if it := st.Items["imported-epic"]; it.APIVersion != APIVersion || it.Project != "acme/platform" {
		t.Fatalf("import did not backfill envelope defaults: %+v", it)
	}
	if _, err := st.Import(Item{Kind: KindEpic, Key: "imported-epic", Title: "dup"}, "specs/", auto, at0); !errors.Is(err, ErrConflict) {
		t.Errorf("duplicate import: err = %v, want ErrConflict", err)
	}
}

func TestUnlabelClearsEmptyLabelMap(t *testing.T) {
	st := mustState(t)
	if _, err := st.CreateTask("t", ItemOptions{}, human, at0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Label("ORN-1", "area", "catalog", human, at0); err != nil {
		t.Fatal(err)
	}
	if st.Items["ORN-1"].Labels["area"] != "catalog" {
		t.Fatal("label not set")
	}
	if _, err := st.Unlabel("ORN-1", "area", human, at0); err != nil {
		t.Fatal(err)
	}
	if st.Items["ORN-1"].Labels != nil {
		t.Fatalf("labels not cleared: %v", st.Items["ORN-1"].Labels)
	}
}

func TestRemoveLinkOnlyDropsMatchingIdentity(t *testing.T) {
	st := mustState(t)
	if _, err := st.CreateTask("t", ItemOptions{}, human, at0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddLink(Link{From: "ORN-1", Type: LinkBlockedBy, To: "ORN-2"}, human, at0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddLink(Link{From: "ORN-1", Type: LinkAffects, To: "sourceplane/orun/api-edge"}, human, at0); err != nil {
		t.Fatal(err)
	}
	if _, err := st.RemoveLink(Link{From: "ORN-1", Type: LinkBlockedBy, To: "ORN-2"}, human, at0); err != nil {
		t.Fatal(err)
	}
	if len(st.Links) != 1 || st.Links[0].Type != LinkAffects {
		t.Fatalf("remove dropped the wrong edge: %+v", st.Links)
	}
}
