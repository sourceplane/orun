package catalogmodel_test

import (
	"encoding/json"
	"testing"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// TestSubscribeEnvironment_UnmarshalJSON_ObjectForm decodes the full object
// form and confirms every modeled field round-trips.
func TestSubscribeEnvironment_UnmarshalJSON_ObjectForm(t *testing.T) {
	const in = `{
	  "name": "production",
	  "profile": "release",
	  "profileRules": [{"profile": "deploy", "when": {"triggerRef": "github-push-main"}}],
	  "dependencyMode": "enforced",
	  "dependencyRules": [{"mode": "advisory", "when": {"triggerRef": "manual"}}],
	  "env": {"REGION": "us-east-1"},
	  "parameters": {"replicas": 3}
	}`
	var e catalogmodel.ComponentYAMLSubscribeEnvironment
	if err := json.Unmarshal([]byte(in), &e); err != nil {
		t.Fatalf("UnmarshalJSON(object): %v", err)
	}
	if e.Name != "production" || e.Profile != "release" {
		t.Errorf("name/profile = %q/%q", e.Name, e.Profile)
	}
	if len(e.ProfileRules) != 1 || e.ProfileRules[0].Profile != "deploy" ||
		e.ProfileRules[0].When.TriggerRef != "github-push-main" {
		t.Errorf("profileRules = %+v", e.ProfileRules)
	}
	if e.DependencyMode != "enforced" {
		t.Errorf("dependencyMode = %q", e.DependencyMode)
	}
	if len(e.DependencyRules) != 1 || e.DependencyRules[0].Mode != "advisory" ||
		e.DependencyRules[0].When.TriggerRef != "manual" {
		t.Errorf("dependencyRules = %+v", e.DependencyRules)
	}
	if e.Env["REGION"] != "us-east-1" {
		t.Errorf("env = %v", e.Env)
	}
	if e.Parameters["replicas"] != float64(3) {
		t.Errorf("parameters = %v", e.Parameters)
	}
}

// TestSubscribeEnvironment_UnmarshalJSON_StringForm decodes the bare-string
// shorthand into the environment name with no profile.
func TestSubscribeEnvironment_UnmarshalJSON_StringForm(t *testing.T) {
	var e catalogmodel.ComponentYAMLSubscribeEnvironment
	if err := json.Unmarshal([]byte(`"dev"`), &e); err != nil {
		t.Fatalf("UnmarshalJSON(string): %v", err)
	}
	if e.Name != "dev" || e.Profile != "" || e.ProfileRules != nil {
		t.Errorf("string form = %+v, want {Name:dev}", e)
	}
}

// TestSubscribeEnvironment_UnmarshalJSON_Errors covers the malformed cases for
// both branches.
func TestSubscribeEnvironment_UnmarshalJSON_Errors(t *testing.T) {
	cases := []string{
		`"unterminated`,      // string branch, bad JSON string
		`{"name": "x", bad}`, // object branch, malformed object
	}
	for _, in := range cases {
		var e catalogmodel.ComponentYAMLSubscribeEnvironment
		if err := json.Unmarshal([]byte(in), &e); err == nil {
			t.Errorf("UnmarshalJSON(%q) = nil error, want error", in)
		}
	}
}

// TestSubscribeEnvironment_JSONSchemaOverride asserts the override is a oneOf
// admitting a string or an object that requires `name`.
func TestSubscribeEnvironment_JSONSchemaOverride(t *testing.T) {
	s := catalogmodel.ComponentYAMLSubscribeEnvironment{}.JSONSchemaOverride()
	oneOf, ok := s["oneOf"].([]any)
	if !ok || len(oneOf) != 2 {
		t.Fatalf("oneOf = %v, want 2 branches", s["oneOf"])
	}
	str, _ := oneOf[0].(map[string]any)
	if str["type"] != "string" {
		t.Errorf("branch 0 = %v, want string", oneOf[0])
	}
	obj, _ := oneOf[1].(map[string]any)
	if obj["type"] != "object" {
		t.Errorf("branch 1 type = %v, want object", obj["type"])
	}
	req, _ := obj["required"].([]any)
	if len(req) != 1 || req[0] != "name" {
		t.Errorf("object required = %v, want [name]", obj["required"])
	}
}

// TestComponentYAML_OpenSchema confirms the authored structs opt into
// additional-property tolerance.
func TestComponentYAML_OpenSchema(t *testing.T) {
	if !(catalogmodel.ComponentYAML{}).OpenSchema() ||
		!(catalogmodel.ComponentYAMLMetadata{}).OpenSchema() ||
		!(catalogmodel.ComponentYAMLSpec{}).OpenSchema() ||
		!(catalogmodel.ComponentYAMLDependency{}).OpenSchema() {
		t.Error("expected every authored struct to report OpenSchema() == true")
	}
}
