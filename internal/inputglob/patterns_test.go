package inputglob

import (
	"encoding/json"
	"reflect"
	"testing"

	"gopkg.in/yaml.v3"
)

func TestPatterns_UnmarshalYAML(t *testing.T) {
	var v struct {
		Inputs Patterns `yaml:"inputs"`
	}
	if err := yaml.Unmarshal([]byte("inputs:\n  - turbo.json\n  - tooling/**\n"), &v); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual([]string(v.Inputs), []string{"turbo.json", "tooling/**"}) {
		t.Fatalf("list form = %v", v.Inputs)
	}
	v.Inputs = nil
	if err := yaml.Unmarshal([]byte("inputs:\n  releaseName: api\n"), &v); err != nil || v.Inputs != nil {
		t.Fatalf("legacy mapping form = %v, %v; want nil, nil", v.Inputs, err)
	}
	if err := yaml.Unmarshal([]byte("inputs: turbo.json\n"), &v); err == nil {
		t.Fatal("scalar form accepted; want an error")
	}
	if err := yaml.Unmarshal([]byte("inputs:\n  - a: b\n"), &v); err == nil {
		t.Fatal("list of mappings accepted; want an error")
	}
}

func TestPatterns_UnmarshalJSON(t *testing.T) {
	var v struct {
		Inputs Patterns `json:"inputs,omitempty"`
	}
	if err := json.Unmarshal([]byte(`{"inputs":["pnpm-lock.yaml"]}`), &v); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual([]string(v.Inputs), []string{"pnpm-lock.yaml"}) {
		t.Fatalf("list form = %v", v.Inputs)
	}
	v.Inputs = nil
	if err := json.Unmarshal([]byte(`{"inputs":{"registry":"ghcr.io/x"}}`), &v); err != nil || v.Inputs != nil {
		t.Fatalf("legacy object form = %v, %v; want nil, nil", v.Inputs, err)
	}
	if err := json.Unmarshal([]byte(`{"inputs":"turbo.json"}`), &v); err == nil {
		t.Fatal("string form accepted; want an error")
	}
	out, _ := json.Marshal(struct {
		Inputs Patterns `json:"inputs,omitempty"`
	}{})
	if string(out) != `{}` {
		t.Fatalf("empty Patterns not omitted: %s", out)
	}
}
