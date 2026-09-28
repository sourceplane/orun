package normalize

import (
	"reflect"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"gopkg.in/yaml.v3"
)

func intentWithInputs(inputs []string) *model.Intent {
	return &model.Intent{
		Metadata:     model.Metadata{Name: "test"},
		Environments: map[string]model.Environment{"dev": {}},
		Components: []model.Component{
			{Name: "web", Type: "app", Inputs: inputs},
		},
	}
}

func TestValidateAcceptsInputGlobs(t *testing.T) {
	inputs := []string{"pnpm-lock.yaml", "turbo.json", "tooling/eslint/**"}
	n, err := NormalizeIntent(intentWithInputs(inputs))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual([]string(n.Components["web"].Inputs), inputs) {
		t.Fatalf("inputs not preserved: %v", n.Components["web"].Inputs)
	}
}

func TestValidateRejectsInvalidInputGlobs(t *testing.T) {
	for _, bad := range []string{"", "/abs/path", "../escape/**", "a/../b", "tooling/[x"} {
		_, err := NormalizeIntent(intentWithInputs([]string{"turbo.json", bad}))
		if err == nil {
			t.Errorf("expected error for input glob %q", bad)
			continue
		}
		if !strings.Contains(err.Error(), "invalid spec.inputs[1]") {
			t.Errorf("unexpected error for %q: %v", bad, err)
		}
	}
}

func TestComponentManifestParsesInputs(t *testing.T) {
	var m model.ComponentManifest
	src := "apiVersion: orun.io/v1alpha1\nkind: Component\nmetadata:\n  name: web\nspec:\n  type: app\n  inputs:\n    - pnpm-lock.yaml\n    - tooling/**\n"
	if err := yaml.Unmarshal([]byte(src), &m); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual([]string(m.Spec.Inputs), []string{"pnpm-lock.yaml", "tooling/**"}) {
		t.Fatalf("spec.inputs = %v", m.Spec.Inputs)
	}
}
