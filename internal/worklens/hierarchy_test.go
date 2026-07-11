package worklens

import (
	_ "embed"
	"encoding/json"
	"testing"
)

//go:embed fixtures/hierarchy-conformance.json
var hierarchyJSON []byte

// hierarchyFixture mirrors fixtures/hierarchy-conformance.json — the shared
// v4 contract the orun-cloud TypeScript fold replays byte-for-byte (WH1).
type hierarchyFixture struct {
	EpicIntentCases []struct {
		Name    string              `json:"name"`
		EpicKey string              `json:"epicKey"`
		Events  []CoordinationEvent `json:"events"`
		Expect  struct {
			State           IntentState `json:"state"`
			CurrentRevision string      `json:"currentRevision"`
			DocDrifted      bool        `json:"docDrifted"`
			LadderDrifted   bool        `json:"ladderDrifted"`
			Approval        *struct {
				Revision string `json:"revision"`
				Snapshot string `json:"snapshot"`
				By       string `json:"by"`
			} `json:"approval"`
			Milestones []string `json:"milestones"`
		} `json:"expect"`
	} `json:"epicIntentCases"`
	DesignIntentCases []struct {
		Name      string              `json:"name"`
		DesignKey string              `json:"designKey"`
		Events    []CoordinationEvent `json:"events"`
		Expect    struct {
			State           IntentState `json:"state"`
			AdoptedRevision string      `json:"adoptedRevision"`
			Minted          []string    `json:"minted"`
			AdoptedBy       string      `json:"adoptedBy"`
			SupersededBy    string      `json:"supersededBy"`
		} `json:"expect"`
	} `json:"designIntentCases"`
	RollupCases []struct {
		Name         string              `json:"name"`
		EpicKey      string              `json:"epicKey"`
		LadderEvents []CoordinationEvent `json:"ladderEvents"`
		Tasks        []Task              `json:"tasks"`
		Events       []CoordinationEvent `json:"events"`
		Observations []Observation       `json:"observations"`
		Expect       struct {
			Milestones []struct {
				Key      string `json:"key"`
				Total    int    `json:"total"`
				Complete int    `json:"complete"`
			} `json:"milestones"`
			Unscheduled *struct {
				Total    int `json:"total"`
				Complete int `json:"complete"`
			} `json:"unscheduled"`
			Total    int `json:"total"`
			Complete int `json:"complete"`
			Blocked  int `json:"blocked"`
		} `json:"expect"`
	} `json:"rollupCases"`
	HealthCases []struct {
		Name          string              `json:"name"`
		InitiativeKey string              `json:"initiativeKey"`
		AsOf          string              `json:"asOf"`
		Epics         []EpicRollup        `json:"epics"`
		Events        []CoordinationEvent `json:"events"`
		Expect        struct {
			Health   Health   `json:"health"`
			Total    int      `json:"total"`
			Complete int      `json:"complete"`
			Evidence []string `json:"evidence"`
			Pinned   *struct {
				Health Health `json:"health"`
				By     string `json:"by"`
			} `json:"pinned"`
		} `json:"expect"`
	} `json:"healthCases"`
}

func loadHierarchyFixture(t *testing.T) hierarchyFixture {
	t.Helper()
	var fx hierarchyFixture
	if err := json.Unmarshal(hierarchyJSON, &fx); err != nil {
		t.Fatalf("parse hierarchy fixtures: %v", err)
	}
	if len(fx.EpicIntentCases) == 0 || len(fx.DesignIntentCases) == 0 || len(fx.RollupCases) == 0 || len(fx.HealthCases) == 0 {
		t.Fatal("hierarchy fixture is missing case groups")
	}
	return fx
}

func TestEpicIntentConformance(t *testing.T) {
	fx := loadHierarchyFixture(t)
	for _, tc := range fx.EpicIntentCases {
		t.Run(tc.Name, func(t *testing.T) {
			got := FoldEpicIntent(tc.EpicKey, tc.Events)
			if got.State != tc.Expect.State {
				t.Errorf("state = %s, want %s", got.State, tc.Expect.State)
			}
			if tc.Expect.CurrentRevision != "" && got.CurrentRevision != tc.Expect.CurrentRevision {
				t.Errorf("currentRevision = %s, want %s", got.CurrentRevision, tc.Expect.CurrentRevision)
			}
			if got.DocDrifted != tc.Expect.DocDrifted {
				t.Errorf("docDrifted = %v, want %v", got.DocDrifted, tc.Expect.DocDrifted)
			}
			if got.LadderDrifted != tc.Expect.LadderDrifted {
				t.Errorf("ladderDrifted = %v, want %v", got.LadderDrifted, tc.Expect.LadderDrifted)
			}
			if w := tc.Expect.Approval; w != nil {
				if got.Approval == nil {
					t.Fatalf("no approval, want approval of %s", w.Revision)
				}
				if got.Approval.Revision != w.Revision || got.Approval.By.ID != w.By {
					t.Errorf("approval = %s by %s, want %s by %s", got.Approval.Revision, got.Approval.By.ID, w.Revision, w.By)
				}
				if w.Snapshot != "" && got.Approval.Snapshot != w.Snapshot {
					t.Errorf("approval snapshot = %s, want %s", got.Approval.Snapshot, w.Snapshot)
				}
			}
			if tc.Expect.Milestones != nil {
				var keys []string
				for _, m := range got.Milestones {
					keys = append(keys, m.Key)
				}
				if !equalStrings(keys, tc.Expect.Milestones) {
					t.Errorf("milestones = %v, want %v", keys, tc.Expect.Milestones)
				}
			}
		})
	}
}

func TestDesignIntentConformance(t *testing.T) {
	fx := loadHierarchyFixture(t)
	for _, tc := range fx.DesignIntentCases {
		t.Run(tc.Name, func(t *testing.T) {
			got := FoldDesignIntent(tc.DesignKey, tc.Events)
			if got.State != tc.Expect.State {
				t.Errorf("state = %s, want %s", got.State, tc.Expect.State)
			}
			if got.AdoptedRevision != tc.Expect.AdoptedRevision {
				t.Errorf("adoptedRevision = %s, want %s", got.AdoptedRevision, tc.Expect.AdoptedRevision)
			}
			if tc.Expect.Minted != nil && !equalStrings(got.Minted, tc.Expect.Minted) {
				t.Errorf("minted = %v, want %v", got.Minted, tc.Expect.Minted)
			}
			if tc.Expect.AdoptedBy != "" && (got.AdoptedBy == nil || got.AdoptedBy.ID != tc.Expect.AdoptedBy) {
				t.Errorf("adoptedBy = %v, want %s", got.AdoptedBy, tc.Expect.AdoptedBy)
			}
			if got.SupersededBy != tc.Expect.SupersededBy {
				t.Errorf("supersededBy = %s, want %s", got.SupersededBy, tc.Expect.SupersededBy)
			}
		})
	}
}

func TestRollupConformance(t *testing.T) {
	fx := loadHierarchyFixture(t)
	for _, tc := range fx.RollupCases {
		t.Run(tc.Name, func(t *testing.T) {
			ws := WorkSet{Tasks: tc.Tasks, Events: tc.Events, Observations: tc.Observations}
			ladder := FoldMilestones(tc.EpicKey, tc.LadderEvents)
			got := FoldEpicExecution(ws, tc.EpicKey, ladder, Fold(ws))

			if len(got.Milestones) != len(tc.Expect.Milestones) {
				t.Fatalf("milestone buckets = %d, want %d", len(got.Milestones), len(tc.Expect.Milestones))
			}
			for i, w := range tc.Expect.Milestones {
				g := got.Milestones[i]
				if g.Key != w.Key || g.Total != w.Total || g.Complete != w.Complete {
					t.Errorf("milestone[%d] = %s %d/%d, want %s %d/%d", i, g.Key, g.Complete, g.Total, w.Key, w.Complete, w.Total)
				}
			}
			switch {
			case tc.Expect.Unscheduled == nil && got.Unscheduled != nil:
				t.Errorf("unexpected unscheduled bucket: %+v", got.Unscheduled)
			case tc.Expect.Unscheduled != nil && got.Unscheduled == nil:
				t.Error("missing unscheduled bucket")
			case tc.Expect.Unscheduled != nil:
				if got.Unscheduled.Total != tc.Expect.Unscheduled.Total || got.Unscheduled.Complete != tc.Expect.Unscheduled.Complete {
					t.Errorf("unscheduled = %d/%d, want %d/%d", got.Unscheduled.Complete, got.Unscheduled.Total, tc.Expect.Unscheduled.Complete, tc.Expect.Unscheduled.Total)
				}
			}
			if got.Total != tc.Expect.Total || got.Complete != tc.Expect.Complete || got.Blocked != tc.Expect.Blocked {
				t.Errorf("totals = %d/%d blocked %d, want %d/%d blocked %d", got.Complete, got.Total, got.Blocked, tc.Expect.Complete, tc.Expect.Total, tc.Expect.Blocked)
			}
		})
	}
}

func TestInitiativeHealthConformance(t *testing.T) {
	fx := loadHierarchyFixture(t)
	for _, tc := range fx.HealthCases {
		t.Run(tc.Name, func(t *testing.T) {
			got := FoldInitiativeStatus(tc.InitiativeKey, tc.Epics, tc.Events, tc.AsOf)
			if got.Health != tc.Expect.Health {
				t.Errorf("health = %s, want %s", got.Health, tc.Expect.Health)
			}
			if tc.Expect.Total != 0 && (got.Total != tc.Expect.Total || got.Complete != tc.Expect.Complete) {
				t.Errorf("progress = %d/%d, want %d/%d", got.Complete, got.Total, tc.Expect.Complete, tc.Expect.Total)
			}
			if tc.Expect.Evidence != nil && !equalStrings(got.Evidence, tc.Expect.Evidence) {
				t.Errorf("evidence = %v, want %v", got.Evidence, tc.Expect.Evidence)
			}
			switch {
			case tc.Expect.Pinned == nil && got.Pinned != nil:
				t.Errorf("unexpectedly pinned to %s", got.Pinned.Health)
			case tc.Expect.Pinned != nil && got.Pinned == nil:
				t.Errorf("not pinned, want pin to %s", tc.Expect.Pinned.Health)
			case tc.Expect.Pinned != nil:
				if got.Pinned.Health != tc.Expect.Pinned.Health || got.Pinned.By.ID != tc.Expect.Pinned.By {
					t.Errorf("pin = %s by %s, want %s by %s", got.Pinned.Health, got.Pinned.By.ID, tc.Expect.Pinned.Health, tc.Expect.Pinned.By)
				}
			}
		})
	}
}

// TestHierarchyFoldDeterminism replays every hierarchy fixture twice and
// requires identical serialized output (invariant 1 extends to v4 folds).
func TestHierarchyFoldDeterminism(t *testing.T) {
	fx := loadHierarchyFixture(t)
	for _, tc := range fx.EpicIntentCases {
		a, _ := json.Marshal(FoldEpicIntent(tc.EpicKey, tc.Events))
		b, _ := json.Marshal(FoldEpicIntent(tc.EpicKey, tc.Events))
		if string(a) != string(b) {
			t.Fatalf("%s: epic intent fold is not deterministic", tc.Name)
		}
	}
	for _, tc := range fx.HealthCases {
		a, _ := json.Marshal(FoldInitiativeStatus(tc.InitiativeKey, tc.Epics, tc.Events, tc.AsOf))
		b, _ := json.Marshal(FoldInitiativeStatus(tc.InitiativeKey, tc.Epics, tc.Events, tc.AsOf))
		if string(a) != string(b) {
			t.Fatalf("%s: health fold is not deterministic", tc.Name)
		}
	}
}

func TestV4VocabularyAndGuards(t *testing.T) {
	// The coordination vocabulary grows 19 → 27 (design §1.3); the
	// observation vocabulary is frozen (V4-1); there is still no
	// delivery-lifecycle-write kind.
	if got := len(EventKinds); got != 27 {
		t.Fatalf("EventKinds = %d, want 27", got)
	}
	if got := len(ObservationKinds); got != 6 {
		t.Fatalf("ObservationKinds = %d, want 6 (frozen — V4-1)", got)
	}

	// Human-only decisions (V4-2): agents AND automation are rejected at
	// write time; users pass.
	for _, k := range HumanOnlyEventKinds {
		for _, actor := range []Actor{{Type: ActorAgent, ID: "sp_1"}, {Type: ActorAutomation, ID: "auto"}} {
			e := CoordinationEvent{Workspace: "ws", Subject: "sourceplane/specs/x", Kind: k, Actor: actor, At: "2026-07-11T00:00:00Z", Seq: 1}
			if err := e.Validate(); err == nil {
				t.Errorf("%s by %s accepted — human-only decision (V4-2)", k, actor.Type)
			}
		}
		e := CoordinationEvent{Workspace: "ws", Subject: "sourceplane/specs/x", Kind: k, Actor: Actor{Type: ActorUser, ID: "usr_1"}, At: "2026-07-11T00:00:00Z", Seq: 1}
		if err := e.Validate(); err != nil {
			t.Errorf("%s by user rejected: %v", k, err)
		}
	}

	// Non-decision v4 kinds accept agents (review verdicts are advice;
	// milestone edits and milestone_set are intent agents may author).
	for _, k := range []EventKind{EventMilestoneEdited, EventMilestoneSet, EventReviewRequested, EventReviewSubmitted} {
		e := CoordinationEvent{Workspace: "ws", Subject: "sourceplane/specs/x", Kind: k, Actor: Actor{Type: ActorAgent, ID: "sp_1"}, At: "2026-07-11T00:00:00Z", Seq: 1}
		if err := e.Validate(); err != nil {
			t.Errorf("v4 kind %q rejected for agent at write time: %v", k, err)
		}
	}
}

func TestMilestoneSubjects(t *testing.T) {
	subj := MilestoneSubject("sourceplane/specs/orun-work-v4", "WH2")
	if subj != "sourceplane/specs/orun-work-v4#WH2" {
		t.Fatalf("subject = %s", subj)
	}
	epic, ms, ok := ParseMilestoneSubject(subj)
	if !ok || epic != "sourceplane/specs/orun-work-v4" || ms != "WH2" {
		t.Fatalf("parse = %s %s %v", epic, ms, ok)
	}
	for _, bad := range []string{"no-hash", "#WH2", "epic#", "epic#lowercase", "epic#TOOLONGKEY1"} {
		if _, _, ok := ParseMilestoneSubject(bad); ok {
			t.Errorf("ParseMilestoneSubject(%q) accepted", bad)
		}
	}
	if !ValidMilestoneKey("WH0") || !ValidMilestoneKey("M1") || !ValidMilestoneKey("PM5b") {
		t.Error("valid milestone keys rejected")
	}
	if ValidMilestoneKey("wh0") || ValidMilestoneKey("W") || ValidMilestoneKey("1") {
		t.Error("invalid milestone keys accepted")
	}
}

func TestLadderHashDeterminism(t *testing.T) {
	a := []Milestone{{Key: "M1", Title: "One", Ordinal: 0}, {Key: "M2", Title: "Two", Ordinal: 1}}
	b := []Milestone{{Key: "M1", Title: "One", Ordinal: 0}, {Key: "M2", Title: "Two", Ordinal: 1}}
	ha, err := LadderHash(a)
	if err != nil {
		t.Fatal(err)
	}
	hb, _ := LadderHash(b)
	if ha != hb {
		t.Fatal("identical ladders hash differently")
	}
	c := []Milestone{{Key: "M1", Title: "One (renamed)", Ordinal: 0}, {Key: "M2", Title: "Two", Ordinal: 1}}
	hc, _ := LadderHash(c)
	if ha == hc {
		t.Fatal("different ladders hash identically")
	}
	empty, _ := LadderHash(nil)
	emptySlice, _ := LadderHash([]Milestone{})
	if empty != emptySlice {
		t.Fatal("nil and empty ladders hash differently")
	}
}

func TestHierarchyEnvelopeValidation(t *testing.T) {
	in := Initiative{APIVersion: APIVersion, Kind: KindInitiative, Key: "sourceplane/initiatives/x", Workspace: "ws", Title: "t", CreatedBy: Actor{Type: ActorUser, ID: "u"}}
	if err := ValidateInitiative(in); err != nil {
		t.Errorf("valid initiative rejected: %v", err)
	}

	d := Design{APIVersion: APIVersion, Kind: KindDesign, Key: "DSG-1", Workspace: "ws", Initiative: "sourceplane/initiatives/x", Title: "t", CreatedBy: Actor{Type: ActorAgent, ID: "sp_1"}}
	if err := ValidateDesign(d); err != nil {
		t.Errorf("valid design rejected: %v", err)
	}
	d.Initiative = ""
	if err := ValidateDesign(d); err == nil {
		t.Error("initiative-less design accepted (hasDesign is exactly-one)")
	}
	d.Initiative = "sourceplane/initiatives/x"
	d.Proposal = &Proposal{Epics: []ProposalEpic{{
		Slug: "epic-a", Title: "A",
		Milestones:    []Milestone{{Key: "M1", Title: "One"}},
		TaskSkeletons: []ProposalTaskSkeleton{{Milestone: "M9", Title: "orphan"}},
	}}}
	if err := ValidateDesign(d); err == nil {
		t.Error("task skeleton naming a milestone outside the ladder accepted")
	}

	// A task with a milestone but no spec is invalid (design §1.2).
	task := Task{APIVersion: APIVersion, Kind: KindTask, Key: "ORN-1", Workspace: "ws", Milestone: "M1", Title: "t", CreatedBy: Actor{Type: ActorUser, ID: "u"}}
	if err := ValidateTask(task); err == nil {
		t.Error("milestone without spec accepted")
	}
	task.Spec = "sourceplane/specs/x"
	if err := ValidateTask(task); err != nil {
		t.Errorf("valid milestone task rejected: %v", err)
	}
}
