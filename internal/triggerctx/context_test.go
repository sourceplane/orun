package triggerctx

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestTriggerOccurrence_JSONRoundtrip(t *testing.T) {
	t.Parallel()

	occ := TriggerOccurrence{
		APIVersion:      APIVersion,
		Kind:            KindName,
		TriggerID:       "trg_01JABCDEF0",
		TriggerKey:      "trg-pr139-def456a",
		TriggerType:     TriggerTypeDeclared,
		TriggerName:     "github-pull-request",
		Mode:            ModeEventFile,
		Provider:        "github",
		Event:           "pull_request",
		Action:          "synchronize",
		MatchedBindings: []string{"github-pull-request"},
		Source: TriggerSource{
			Repo:         "sourceplane/orun",
			Ref:          "refs/pull/139/head",
			SourceScope:  "pr-139",
			HeadRevision: "def456a1b2c3",
			BaseRevision: "abc1239f8e7d",
			WorkingTree:  WorkingTreeClean,
		},
		PlanScope: PlanScope{
			Mode:               PlanScopeChanged,
			Base:               "abc1239f8e7d",
			Head:               "def456a1b2c3",
			ActiveEnvironments: []string{"development"},
			ChangedComponents:  []string{"api-edge-worker"},
		},
		CreatedAt: time.Date(2026, 5, 29, 0, 0, 0, 0, time.UTC),
	}

	b, err := json.Marshal(occ)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	// data-model.md §2.1 contract — the field name spellings are LOAD-BEARING.
	for _, want := range []string{
		`"apiVersion":"orun.io/v1alpha1"`,
		`"kind":"TriggerOccurrence"`,
		`"triggerId":"trg_01JABCDEF0"`,
		`"triggerKey":"trg-pr139-def456a"`,
		`"triggerType":"declared"`,
		`"triggerName":"github-pull-request"`,
		`"planScope":{`,
		`"sourceScope":"pr-139"`,
		`"workingTree":"clean"`,
	} {
		if !strings.Contains(string(b), want) {
			t.Errorf("marshaled JSON missing %s\ngot: %s", want, string(b))
		}
	}

	var back TriggerOccurrence
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.TriggerKey != occ.TriggerKey || back.PlanScope.Mode != occ.PlanScope.Mode {
		t.Errorf("roundtrip mismatch: %+v vs %+v", back, occ)
	}
}

func TestTriggerOccurrence_OmitEmpty(t *testing.T) {
	t.Parallel()
	occ := TriggerOccurrence{APIVersion: APIVersion, Kind: KindName}
	b, _ := json.Marshal(occ)
	if strings.Contains(string(b), `"action":`) {
		t.Errorf("expected action to be omitted when empty: %s", string(b))
	}
	if strings.Contains(string(b), `"matchedBindings":`) {
		t.Errorf("expected matchedBindings to be omitted when empty: %s", string(b))
	}
}

func TestClone_IndependentSlices(t *testing.T) {
	t.Parallel()
	src := TriggerOccurrence{
		MatchedBindings: []string{"a"},
		PlanScope: PlanScope{
			ActiveEnvironments: []string{"dev"},
			ChangedComponents:  []string{"comp"},
		},
	}
	clone := src.Clone()
	clone.MatchedBindings[0] = "b"
	clone.PlanScope.ActiveEnvironments[0] = "prod"
	clone.PlanScope.ChangedComponents[0] = "other"

	if src.MatchedBindings[0] != "a" || src.PlanScope.ActiveEnvironments[0] != "dev" || src.PlanScope.ChangedComponents[0] != "comp" {
		t.Errorf("Clone shared backing arrays: src=%+v", src)
	}
}

func TestKeyPattern_DataModelContract(t *testing.T) {
	t.Parallel()
	// data-model.md §2.2 — keep this regexp aligned with the doc.
	want := regexp.MustCompile(`^trg-[a-z0-9-]+-([a-f0-9]{7}|local-dirty|no-git)$`)
	if TriggerKeyPattern().String() != want.String() {
		t.Errorf("TriggerKeyPattern drift: got %s want %s", TriggerKeyPattern(), want)
	}
}
