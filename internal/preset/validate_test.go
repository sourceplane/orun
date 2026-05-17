package preset

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestValidateExtendsRefsRejectsUnknownSource(t *testing.T) {
	intent := &model.Intent{
		Extends: []model.ExtendsRef{
			{Source: "unknown", Preset: "standard"},
		},
		Compositions: model.CompositionConfig{
			Sources: []model.CompositionSource{
				{Name: "aws-platform", Kind: "oci"},
			},
		},
	}

	err := ValidateExtendsRefs(intent)
	if err == nil {
		t.Fatal("expected error for unknown source")
	}
}

func TestValidateExtendsRefsAcceptsValidSource(t *testing.T) {
	intent := &model.Intent{
		Extends: []model.ExtendsRef{
			{Source: "aws-platform", Preset: "standard"},
		},
		Compositions: model.CompositionConfig{
			Sources: []model.CompositionSource{
				{Name: "aws-platform", Kind: "oci"},
			},
		},
	}

	err := ValidateExtendsRefs(intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidateExtendsRefsNoExtends(t *testing.T) {
	intent := &model.Intent{}
	err := ValidateExtendsRefs(intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestValidatePresetSpecRejectsWrongKind(t *testing.T) {
	preset := &model.IntentPreset{Kind: "Wrong"}
	prov := model.PresetProvenance{Source: "src", Preset: "p"}

	err := ValidatePresetSpec(preset, prov)
	if err == nil {
		t.Fatal("expected error for wrong kind")
	}
}

func TestValidatePresetSpecAcceptsCorrectKind(t *testing.T) {
	preset := &model.IntentPreset{Kind: "IntentPreset"}
	prov := model.PresetProvenance{Source: "src", Preset: "p"}

	err := ValidatePresetSpec(preset, prov)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}
