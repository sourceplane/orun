package actions

import (
	"strings"
	"testing"
)

// A TEMPLATE CAN ONLY PRODUCE TEXT. renderValue returns a string, so a
// blueprint that wires a boolean input into a boolean parameter —
//
//	private: "{{ .inputs.repoPrivate }}"
//
// used to be refused outright: `expects a boolean, got string`. That left
// `orun.repo/ensure@v1`'s `private` hard-coded wherever it was used, so a
// baseline could not instantiate a public product without editing the
// blueprint. What the action reads back must still be a real bool.
func TestABoolParamAcceptsWhatATemplateRenders(t *testing.T) {
	for _, text := range []string{"false", "true", "False", "TRUE", " true "} {
		resolved, err := Resolve("orun.repo/ensure@v1", map[string]any{
			"owner": "sourceplane", "name": "x", "private": text,
		})
		if err != nil {
			t.Fatalf("private=%q: %v", text, err)
		}
		got, ok := resolved["private"].(bool)
		if !ok {
			t.Fatalf("private=%q resolved to %T, want a real bool", text, resolved["private"])
		}
		want := strings.EqualFold(strings.TrimSpace(text), "true")
		if got != want {
			t.Fatalf("private=%q resolved to %v, want %v", text, got, want)
		}
		// The accessor every action uses must agree.
		if BoolParam(Input{Params: resolved}, "private") != want {
			t.Fatalf("private=%q: BoolParam disagrees with the resolved value", text)
		}
	}
}

// A real bool is still a real bool — and the declared default still arrives
// untouched when nothing is passed.
func TestABoolParamStillTakesABoolAndItsDefault(t *testing.T) {
	resolved, err := Resolve("orun.repo/ensure@v1", map[string]any{
		"owner": "o", "name": "n", "private": false,
	})
	if err != nil || resolved["private"] != false {
		t.Fatalf("resolved = %#v, err = %v", resolved["private"], err)
	}
	resolved, err = Resolve("orun.repo/ensure@v1", map[string]any{"owner": "o", "name": "n"})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if resolved["private"] != true {
		t.Fatalf("the declared default should stand, got %#v", resolved["private"])
	}
}

// COERCION IS NOT PERMISSIVENESS. Text that does not read as a boolean is
// still an error — silently treating "maybe" as false is how a repository
// meant to be private is born public.
func TestABoolParamRefusesTextThatIsNotABoolean(t *testing.T) {
	for _, text := range []string{"maybe", "", "yes please", "2"} {
		_, err := Resolve("orun.repo/ensure@v1", map[string]any{
			"owner": "o", "name": "n", "private": text,
		})
		if err == nil {
			t.Fatalf("private=%q was accepted", text)
		}
		if !strings.Contains(err.Error(), "boolean") {
			t.Fatalf("private=%q: error = %v, want it to say what was expected", text, err)
		}
	}
	// A wrong type that is not text is refused as before.
	if _, err := Resolve("orun.repo/ensure@v1", map[string]any{
		"owner": "o", "name": "n", "private": 5,
	}); err == nil {
		t.Fatal("a number was accepted for a boolean")
	}
}

// An int parameter has exactly the same problem and the same answer: every
// waitSeconds a blueprint wants to drive from an input arrives as text.
func TestAnIntParamAcceptsWhatATemplateRenders(t *testing.T) {
	resolved, err := Resolve("orun.run/watch@v1", map[string]any{
		"repo": "o/n", "waitSeconds": "600",
	})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if IntParam(Input{Params: resolved}, "waitSeconds") != 600 {
		t.Fatalf("waitSeconds = %#v, want 600", resolved["waitSeconds"])
	}
	if _, err := Resolve("orun.run/watch@v1", map[string]any{
		"repo": "o/n", "waitSeconds": "soon",
	}); err == nil {
		t.Fatal(`waitSeconds "soon" was accepted`)
	}
}
