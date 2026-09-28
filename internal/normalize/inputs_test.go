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
			{Name: "web", Type: "app", Change: model.ComponentChange{Inputs: inputs}},
		},
	}
}

func TestValidateAcceptsChangeInputGlobs(t *testing.T) {
	inputs := []string{"pnpm-lock.yaml", "turbo.json", "tooling/eslint/**"}
	n, err := NormalizeIntent(intentWithInputs(inputs))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !reflect.DeepEqual(n.Components["web"].Change.Inputs, inputs) {
		t.Fatalf("inputs not preserved: %v", n.Components["web"].Change.Inputs)
	}
}

func TestValidateRejectsInvalidChangeInputGlobs(t *testing.T) {
	for _, bad := range []string{"", "/abs/path", "../escape/**", "a/../b", "tooling/[x"} {
		_, err := NormalizeIntent(intentWithInputs([]string{"turbo.json", bad}))
		if err == nil {
			t.Errorf("expected error for input glob %q", bad)
			continue
		}
		if !strings.Contains(err.Error(), "invalid change.inputs[1]") {
			t.Errorf("unexpected error for %q: %v", bad, err)
		}
	}
}

func TestComponentManifestParsesChangeInputs(t *testing.T) {
	var m model.ComponentManifest
	src := "apiVersion: orun.io/v1alpha1\nkind: Component\nmetadata:\n  name: web\nspec:\n  type: app\n  change:\n    watches: [env]\n    inputs:\n      - pnpm-lock.yaml\n      - tooling/**\n"
	if err := yaml.Unmarshal([]byte(src), &m); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(m.Spec.Change.Inputs, []string{"pnpm-lock.yaml", "tooling/**"}) {
		t.Fatalf("spec.change.inputs = %v", m.Spec.Change.Inputs)
	}
	if !reflect.DeepEqual(m.Spec.Change.Watches, []string{"env"}) {
		t.Fatalf("spec.change.watches = %v", m.Spec.Change.Watches)
	}
}
