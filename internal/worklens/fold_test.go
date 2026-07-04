package worklens

import (
	_ "embed"
	"encoding/json"
	"testing"
)

//go:embed fixtures/conformance.json
var conformanceJSON []byte

// ConformanceFixture mirrors fixtures/conformance.json — the shared
// contract the orun-cloud TypeScript fold replays byte-for-byte.
type conformanceFixture struct {
	Cases []struct {
		Name         string              `json:"name"`
		Workspace    string              `json:"workspace"`
		Tasks        []Task              `json:"tasks"`
		Events       []CoordinationEvent `json:"events"`
		Observations []Observation       `json:"observations"`
		Expect       struct {
			Lifecycles map[string]struct {
				Rung     Rung     `json:"rung"`
				Ready    bool     `json:"ready"`
				Blocked  bool     `json:"blocked"`
				Evidence []string `json:"evidence"`
				Pinned   *struct {
					Rung Rung   `json:"rung"`
					By   string `json:"by"`
				} `json:"pinned"`
			} `json:"lifecycles"`
			Drift       []DriftItem  `json:"drift"`
			Suggestions []Suggestion `json:"suggestions"`
		} `json:"expect"`
	} `json:"cases"`
}

func TestFoldConformance(t *testing.T) {
	var fx conformanceFixture
	if err := json.Unmarshal(conformanceJSON, &fx); err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	if len(fx.Cases) == 0 {
		t.Fatal("no fixture cases")
	}
	for _, tc := range fx.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			got := Fold(WorkSet{Tasks: tc.Tasks, Events: tc.Events, Observations: tc.Observations})

			if len(got.Lifecycles) != len(tc.Tasks) {
				t.Fatalf("lifecycles for %d tasks, want %d", len(got.Lifecycles), len(tc.Tasks))
			}
			for key, want := range tc.Expect.Lifecycles {
				lc, ok := got.Lifecycles[key]
				if !ok {
					t.Fatalf("no lifecycle for %s", key)
				}
				if lc.Rung != want.Rung {
					t.Errorf("%s rung = %s, want %s", key, lc.Rung, want.Rung)
				}
				if lc.Ready != want.Ready {
					t.Errorf("%s ready = %v, want %v", key, lc.Ready, want.Ready)
				}
				if lc.Blocked != want.Blocked {
					t.Errorf("%s blocked = %v, want %v", key, lc.Blocked, want.Blocked)
				}
				if want.Evidence != nil && !equalStrings(lc.Evidence, want.Evidence) {
					t.Errorf("%s evidence = %v, want %v", key, lc.Evidence, want.Evidence)
				}
				switch {
				case want.Pinned == nil && lc.Pinned != nil:
					t.Errorf("%s unexpectedly pinned to %s", key, lc.Pinned.Rung)
				case want.Pinned != nil && lc.Pinned == nil:
					t.Errorf("%s not pinned, want pin to %s", key, want.Pinned.Rung)
				case want.Pinned != nil && lc.Pinned != nil:
					if lc.Pinned.Rung != want.Pinned.Rung || lc.Pinned.By.ID != want.Pinned.By {
						t.Errorf("%s pin = %s by %s, want %s by %s", key, lc.Pinned.Rung, lc.Pinned.By.ID, want.Pinned.Rung, want.Pinned.By)
					}
				}
			}

			if !equalDrift(got.Drift, tc.Expect.Drift) {
				t.Errorf("drift = %+v, want %+v", got.Drift, tc.Expect.Drift)
			}
			if !equalSuggestions(got.Suggestions, tc.Expect.Suggestions) {
				t.Errorf("suggestions = %+v, want %+v", got.Suggestions, tc.Expect.Suggestions)
			}
		})
	}
}

// TestFoldDeterminism replays every fixture twice and requires identical
// serialized output — the droppable-cache guarantee (invariant 1).
func TestFoldDeterminism(t *testing.T) {
	var fx conformanceFixture
	if err := json.Unmarshal(conformanceJSON, &fx); err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	for _, tc := range fx.Cases {
		ws := WorkSet{Tasks: tc.Tasks, Events: tc.Events, Observations: tc.Observations}
		a, err := json.Marshal(Fold(ws))
		if err != nil {
			t.Fatal(err)
		}
		b, err := json.Marshal(Fold(ws))
		if err != nil {
			t.Fatal(err)
		}
		if string(a) != string(b) {
			t.Fatalf("%s: fold is not deterministic", tc.Name)
		}
	}
}

func TestProgress(t *testing.T) {
	tasks := []Task{
		{APIVersion: APIVersion, Kind: KindTask, Key: "ORN-1", Workspace: "ws", Spec: "spec-a", Title: "a", CreatedBy: Actor{Type: ActorUser, ID: "u"}},
		{APIVersion: APIVersion, Kind: KindTask, Key: "ORN-2", Workspace: "ws", Spec: "spec-a", Title: "b", Contract: &Contract{Goal: "g", Affects: []string{"x/y/z"}, DoneWhen: []string{"d"}, Gates: []string{"tests"}}, CreatedBy: Actor{Type: ActorUser, ID: "u"}},
		{APIVersion: APIVersion, Kind: KindTask, Key: "ORN-3", Workspace: "ws", Spec: "spec-b", Title: "c", CreatedBy: Actor{Type: ActorUser, ID: "u"}},
	}
	ws := WorkSet{Tasks: tasks}
	counts := Progress(ws, "spec-a", Fold(ws))
	if counts[RungDraft] != 1 || counts[RungReady] != 1 {
		t.Fatalf("progress = %v", counts)
	}
	if total := counts[RungDraft] + counts[RungReady]; total != 2 {
		t.Fatalf("spec-b task leaked into spec-a progress: %v", counts)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func equalDrift(a, b []DriftItem) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].PR != b[i].PR || !equalStrings(a[i].Affected, b[i].Affected) {
			return false
		}
	}
	return true
}

func equalSuggestions(a, b []Suggestion) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i].PR != b[i].PR || !equalStrings(a[i].TaskKeys, b[i].TaskKeys) {
			return false
		}
	}
	return true
}
