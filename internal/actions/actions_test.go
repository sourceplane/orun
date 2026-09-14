package actions

import (
	"strings"
	"testing"
)

func TestRegistryIDsAreWellFormed(t *testing.T) {
	ids := IDs()
	if len(ids) == 0 {
		t.Fatal("registry is empty — at least one action must be registered")
	}
	for _, id := range ids {
		if !idPattern.MatchString(id) {
			t.Errorf("action id %q does not match <namespace>/<verb>@v<major>", id)
		}
		spec, ok := Lookup(id)
		if !ok {
			t.Errorf("IDs() listed %q but Lookup does not resolve it", id)
			continue
		}
		if spec.Summary == "" {
			t.Errorf("action %q has no summary — the registry is a contract a blueprint author reads", id)
		}
		for _, p := range spec.Params {
			if p.Description == "" {
				t.Errorf("action %q parameter %q has no description", id, p.Name)
			}
			switch p.Type {
			case ParamString, ParamBool, ParamInt, ParamStringList:
			default:
				t.Errorf("action %q parameter %q declares unknown type %q", id, p.Name, p.Type)
			}
			if p.Required && p.Default != nil {
				t.Errorf("action %q parameter %q is required and also has a default — one or the other", id, p.Name)
			}
		}
	}
}

func TestValidateUnknownAction(t *testing.T) {
	err := Validate("orun.nope/missing@v1", nil)
	if err == nil {
		t.Fatal("expected an error for an unregistered action")
	}
	if !strings.Contains(err.Error(), "known actions:") {
		t.Errorf("the error should list what IS available; got %v", err)
	}
}

func TestValidateUnknownParameterNamesTheAlternatives(t *testing.T) {
	err := Validate("orun.http/probe@v1", map[string]any{"urlz": []any{"https://example.test"}})
	if err == nil {
		t.Fatal("expected an error for a misspelled parameter")
	}
	msg := err.Error()
	if !strings.Contains(msg, `"urlz"`) {
		t.Errorf("error should quote the offending parameter; got %v", err)
	}
	if !strings.Contains(msg, "urls") {
		t.Errorf("error should name the parameters the action does take; got %v", err)
	}
}

func TestValidateMissingRequiredParameter(t *testing.T) {
	if err := Validate("orun.http/probe@v1", map[string]any{"expectStatus": 200}); err == nil {
		t.Fatal("expected an error when a required parameter is absent")
	}
}

func TestValidateTypeMismatch(t *testing.T) {
	err := Validate("orun.http/probe@v1", map[string]any{"urls": "https://example.test"})
	if err == nil {
		t.Fatal("expected an error when a stringList parameter is given a bare string")
	}
	if !strings.Contains(err.Error(), "list of strings") {
		t.Errorf("error should say what the parameter expects; got %v", err)
	}
}

// A templated value cannot be type-checked before placement, so validation must
// accept it — while still catching a misspelled NAME, which is the mistake that
// actually happens.
func TestValidateDefersTemplatedValues(t *testing.T) {
	with := map[string]any{
		"urls":         []any{"https://example.test"},
		"expectStatus": "{{ .hooks.land.outputs.status }}",
	}
	if err := Validate("orun.http/probe@v1", with); err != nil {
		t.Fatalf("a templated value should be deferred, not rejected: %v", err)
	}
	bad := map[string]any{
		"urls":          []any{"https://example.test"},
		"expectStatuss": "{{ .hooks.land.outputs.status }}",
	}
	if err := Validate("orun.http/probe@v1", bad); err == nil {
		t.Fatal("a templated value must not excuse a misspelled parameter name")
	}
}

func TestResolveAppliesDefaults(t *testing.T) {
	got, err := Resolve("orun.http/probe@v1", map[string]any{"urls": []any{"https://example.test"}})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if got["expectStatus"] != 200 {
		t.Errorf("expectStatus default should be 200, got %v", got["expectStatus"])
	}
	if got["timeoutSeconds"] != 10 {
		t.Errorf("timeoutSeconds default should be 10, got %v", got["timeoutSeconds"])
	}
}

// Resolve runs AFTER templates are rendered, so it is the last gate before an
// action acts on a value whose expression produced the wrong shape.
func TestResolveRejectsRenderedValueOfWrongType(t *testing.T) {
	_, err := Resolve("orun.http/probe@v1", map[string]any{
		"urls":         []any{"https://example.test"},
		"expectStatus": "two hundred",
	})
	if err == nil {
		t.Fatal("expected a type error for a rendered value that is not a number")
	}
}

func TestDuplicateRegistrationPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("registering a duplicate id must panic at startup, not ship a lying registry")
		}
	}()
	register(Spec{ID: "orun.http/probe@v1", Summary: "dup"}, nil)
}

func TestMalformedIDPanics(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("a malformed action id must panic at startup")
		}
	}()
	register(Spec{ID: "NotAnActionID", Summary: "bad"}, nil)
}
