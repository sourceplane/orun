package scaffold

import (
	"errors"
	"testing"
)

func TestCollectInputs_TypesAndValidation(t *testing.T) {
	inputs := map[string]InputSpec{
		"serviceName": {Type: InputString, Pattern: "^[a-z][a-z0-9-]*$", Required: true},
		"runtime":     {Type: InputEnum, Values: []string{"node", "python"}, Default: "node"},
		"replicas":    {Type: InputNumber, Default: float64(1)},
		"public":      {Type: InputBoolean},
		"orgName":     {Type: InputString, Required: true},
	}
	v, err := CollectInputs(inputs, map[string]string{
		"serviceName": "billing-api",
		"orgName":     "acme",
		"replicas":    "3",
		"public":      "true",
	})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	if v.Fields["serviceName"] != "billing-api" {
		t.Errorf("serviceName = %v", v.Fields["serviceName"])
	}
	if v.Fields["runtime"] != "node" { // default applied
		t.Errorf("runtime = %v", v.Fields["runtime"])
	}
	if v.Fields["replicas"] != float64(3) {
		t.Errorf("replicas = %v (%T)", v.Fields["replicas"], v.Fields["replicas"])
	}
	if v.Fields["public"] != true {
		t.Errorf("public = %v", v.Fields["public"])
	}
}

func TestCollectInputs_RequiredMissing(t *testing.T) {
	inputs := map[string]InputSpec{"name": {Type: InputString, Required: true}}
	_, err := CollectInputs(inputs, map[string]string{})
	assertExit(t, err, 1)
}

func TestCollectInputs_PatternFail(t *testing.T) {
	inputs := map[string]InputSpec{"name": {Type: InputString, Pattern: "^[a-z]+$", Required: true}}
	_, err := CollectInputs(inputs, map[string]string{"name": "Bad-Name"})
	assertExit(t, err, 1)
}

func TestCollectInputs_EnumFail(t *testing.T) {
	inputs := map[string]InputSpec{"c": {Type: InputEnum, Values: []string{"a", "b"}, Required: true}}
	_, err := CollectInputs(inputs, map[string]string{"c": "z"})
	assertExit(t, err, 1)
}

func TestCollectInputs_NumberFail(t *testing.T) {
	inputs := map[string]InputSpec{"n": {Type: InputNumber, Required: true}}
	_, err := CollectInputs(inputs, map[string]string{"n": "notnum"})
	assertExit(t, err, 1)
}

func TestCollectInputs_UnknownInput(t *testing.T) {
	inputs := map[string]InputSpec{"a": {Type: InputString}}
	_, err := CollectInputs(inputs, map[string]string{"typo": "x"})
	assertExit(t, err, 1)
}

func TestCollectInputs_SecretNeverInNonSecretState(t *testing.T) {
	inputs := map[string]InputSpec{
		"apiToken": {Type: InputString, Secret: true, Required: true},
		"name":     {Type: InputString, Required: true},
	}
	v, err := CollectInputs(inputs, map[string]string{"apiToken": "s3cr3t-value", "name": "svc"})
	if err != nil {
		t.Fatalf("collect: %v", err)
	}
	// The secret is in Fields (a bind template may reference it) but the
	// provenance projection must redact it and never leak the literal.
	ns := v.nonSecretFields()
	if ns["apiToken"] == "s3cr3t-value" {
		t.Fatal("secret leaked into non-secret provenance state")
	}
	if ns["apiToken"] != "<secret>" {
		t.Errorf("secret not redacted: %v", ns["apiToken"])
	}
	found := false
	for _, s := range v.SecretValues() {
		if s == "s3cr3t-value" {
			found = true
		}
	}
	if !found {
		t.Fatal("secret value not tracked for the copy-mode sweep")
	}
}

func assertExit(t *testing.T, err error, code int) {
	t.Helper()
	if err == nil {
		t.Fatalf("expected error with exit code %d, got nil", code)
	}
	var ee *ExitError
	if !errors.As(err, &ee) {
		t.Fatalf("expected *ExitError, got %T: %v", err, err)
	}
	if ee.Code != code {
		t.Fatalf("exit code = %d, want %d (err: %v)", ee.Code, code, err)
	}
}
