package scaffold

import (
	"strings"
	"testing"
)

func fromSpecs() map[string]InputSpec {
	return map[string]InputSpec{
		"reponame":      {Type: InputString, Required: true},
		"orunWorkspace": {Type: InputString, Default: "", From: InputFromWorkspace},
		"other":         {Type: InputString, Default: "x"},
	}
}

func TestFillInputsFromFactsFillsOnlyAGap(t *testing.T) {
	raw := map[string]string{"reponame": "newne"}
	got := FillInputsFromFacts(fromSpecs(), raw, InputFacts{Workspace: " org_1 "})
	if got["orunWorkspace"] != "org_1" {
		t.Errorf("an unset from:workspace input takes the workspace, got %q", got["orunWorkspace"])
	}
	if _, filled := got["other"]; filled {
		t.Errorf("an input with no `from` is not filled, got %q", got["other"])
	}
	if _, touched := raw["orunWorkspace"]; touched {
		t.Error("raw was modified")
	}

	set := FillInputsFromFacts(fromSpecs(), map[string]string{"orunWorkspace": "ws_SET"}, InputFacts{Workspace: "org_1"})
	if set["orunWorkspace"] != "ws_SET" {
		t.Errorf("a value somebody set wins, got %q", set["orunWorkspace"])
	}
	// Present-and-empty is a value too: recovered from a record, or set on
	// purpose. It is not a gap.
	blank := FillInputsFromFacts(fromSpecs(), map[string]string{"orunWorkspace": ""}, InputFacts{Workspace: "org_1"})
	if blank["orunWorkspace"] != "" {
		t.Errorf("an empty value somebody set is kept, got %q", blank["orunWorkspace"])
	}
	none := FillInputsFromFacts(fromSpecs(), map[string]string{}, InputFacts{Workspace: "  "})
	if _, filled := none["orunWorkspace"]; filled {
		t.Error("an empty fact fills nothing, so the default still applies")
	}
}

func TestBlueprintRefusesAnUnknownFrom(t *testing.T) {
	_, err := ParseBlueprint([]byte(`apiVersion: orun.dev/v1
kind: Blueprint
metadata:
  name: bp
inputs:
  ws: { type: string, from: repository }
modules:
  - name: m
    mode: template
    files:
      a.md: "a"
`))
	if err == nil || !strings.Contains(err.Error(), `from must be "workspace"`) {
		t.Fatalf("want a refusal naming the allowed source, got %v", err)
	}
}
