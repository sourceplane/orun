package provenance

import (
	_ "embed"
	"encoding/json"
	"testing"
)

// SK5: fixtures/standards-conformance.json is the byte-identical twin of
// orun-cloud packages/db/src/provenance/fixtures/standards-conformance.json.
// `orun pr check --standards` (this engine) and the cloud's orun/compliance
// evaluator replay the same cases and must emit exactly these findings, in
// this order, with these texts. Change the file only in lockstep with both.

//go:embed fixtures/standards-conformance.json
var standardsConformanceJSON []byte

type standardsConformance struct {
	Version int `json:"version"`
	Cases   []struct {
		Name  string `json:"name"`
		Mode  Mode   `json:"mode"`
		Input struct {
			Branch         string                    `json:"branch"`
			TaskKey        string                    `json:"taskKey"`
			CommitMessages []string                  `json:"commitMessages"`
			Manifest       *Manifest                 `json:"manifest"`
			HasSkillPins   bool                      `json:"hasSkillPins"`
			SkillStatus    map[string]SkillPinStatus `json:"skillStatus"`
			Contract       *ContractFact             `json:"contract"`
			AffectsOutside []string                  `json:"affectsOutside"`
			Epic           *EpicFact                 `json:"epic"`
			Work           *WorkFact                 `json:"work"`
		} `json:"input"`
		Findings []Finding `json:"findings"`
	} `json:"cases"`
}

func TestStandardsConformance(t *testing.T) {
	var fx standardsConformance
	if err := json.Unmarshal(standardsConformanceJSON, &fx); err != nil {
		t.Fatalf("parse fixtures: %v", err)
	}
	if fx.Version != 1 {
		t.Fatalf("fixture version = %d, want 1", fx.Version)
	}
	if len(fx.Cases) == 0 {
		t.Fatal("no fixture cases")
	}
	for _, tc := range fx.Cases {
		t.Run(tc.Name, func(t *testing.T) {
			got := CheckStandards(StandardsInput{
				CheckInput: CheckInput{
					Branch:         tc.Input.Branch,
					TaskKey:        tc.Input.TaskKey,
					CommitMessages: tc.Input.CommitMessages,
					Manifest:       tc.Input.Manifest,
					HasSkillPins:   tc.Input.HasSkillPins,
				},
				SkillStatus:    tc.Input.SkillStatus,
				Contract:       tc.Input.Contract,
				AffectsOutside: tc.Input.AffectsOutside,
				Epic:           tc.Input.Epic,
				Work:           tc.Input.Work,
			}, tc.Mode)
			if len(got) != len(tc.Findings) {
				t.Fatalf("findings = %+v, want %+v", got, tc.Findings)
			}
			for i, want := range tc.Findings {
				if got[i] != want {
					t.Errorf("finding[%d] = %+v, want %+v", i, got[i], want)
				}
			}
		})
	}
}

func TestStandardsMode(t *testing.T) {
	for _, c := range []struct {
		in   string
		want Mode
		ok   bool
	}{
		{"", "", true}, {"warn", ModeWarn, true}, {"ENFORCE", ModeEnforce, true}, {" off ", ModeOff, true}, {"strict", "", false},
	} {
		got, err := ParseMode(c.in)
		if (err == nil) != c.ok || got != c.want {
			t.Errorf("ParseMode(%q) = %q, %v; want %q, ok=%v", c.in, got, err, c.want, c.ok)
		}
	}
	// warn never fails what passes today; enforce does; off says nothing.
	wrong := StandardsInput{
		CheckInput:     CheckInput{Branch: "orun/SK-5-setup"},
		SkillStatus:    map[string]SkillPinStatus{"orunbase": SkillUnknown},
		Contract:       &ContractFact{Present: false},
		AffectsOutside: []string{"db"},
		Epic:           &EpicFact{ClosesMilestone: true},
	}
	if HasErrors(CheckStandards(wrong, ModeWarn)) {
		t.Error("warn must not fail")
	}
	if !HasErrors(CheckStandards(wrong, ModeEnforce)) {
		t.Error("enforce must fail")
	}
	if len(CheckStandards(wrong, ModeOff)) != 0 {
		t.Error("off must be silent")
	}
	// A rule with no fact says nothing, even under enforce.
	clean := StandardsInput{CheckInput: CheckInput{Branch: "orun/SK-5-setup",
		Manifest: &Manifest{Version: 1, Task: "SK-5", Skills: []SkillPin{{Name: "orunbase", Rev: "sha256:aa"}}}}}
	if got := CheckStandards(clean, ModeEnforce); len(got) != 0 {
		t.Errorf("no facts, no findings; got %+v", got)
	}
}
