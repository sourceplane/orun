package catalogresolve

import (
	"context"
	"errors"
	"testing"
)

// TestResolve_UnknownFields_Tolerated proves the forward-compatibility
// contract: a manifest carrying an unmodeled spec key and a typo'd subscribe
// field still resolves (no hard error in default mode), and each unrecognized
// key surfaces as a component.field.unknown warning at its exact pointer.
func TestResolve_UnknownFields_Tolerated(t *testing.T) {
	root := fixturePath(t, "unknown_fields")
	rc, issues, err := Resolve(context.Background(), Options{WorkspaceRoot: root})
	if err != nil {
		t.Fatalf("Resolve hard error (issues=%v): %v", issues, err)
	}
	if rc == nil || len(rc.Manifests) != 1 {
		t.Fatalf("want 1 manifest, got %v", rc)
	}

	wantPtrs := map[string]bool{
		"/spec/futureField":                     false,
		"/spec/subscribe/environments/0/profle": false,
	}
	for _, is := range issues {
		if is.Code != "component.field.unknown" {
			continue
		}
		if is.Severity != SeverityWarning {
			t.Errorf("unknown-field severity = %v, want Warning (default mode)", is.Severity)
		}
		if _, ok := wantPtrs[is.Pointer]; ok {
			wantPtrs[is.Pointer] = true
		}
	}
	for ptr, seen := range wantPtrs {
		if !seen {
			t.Errorf("missing component.field.unknown warning for %q", ptr)
		}
	}

	// The known sibling keys are still resolved despite the unknown ones.
	svc := findByName(rc.Manifests, "svc")
	if svc.Spec.Domain != "edge" {
		t.Errorf("svc Spec.Domain = %q, want edge", svc.Spec.Domain)
	}
	if got := svc.Spec.Environments["production"]; got.Profile != "release" {
		t.Errorf("svc production profile = %q, want release", got.Profile)
	}

	// Non-scalar parameter value is encoded as canonical (sorted-key) JSON,
	// not Go %v map syntax.
	if got, want := svc.Spec.Parameters["config"], `{"regions":["us-east-1","eu-west-1"],"retries":3}`; got != want {
		t.Errorf("nested param encoding = %q, want %q", got, want)
	}
}

// TestResolve_UnknownFields_StrictPromotes confirms strict mode turns the
// unknown-field warning into a hard error — the lint gate for CI.
func TestResolve_UnknownFields_StrictPromotes(t *testing.T) {
	root := fixturePath(t, "unknown_fields")
	_, issues, err := Resolve(context.Background(), Options{WorkspaceRoot: root, Strict: true})
	if err == nil {
		t.Fatal("expected strict-mode error for unknown field")
	}
	var vi ValidationIssue
	if !errors.As(err, &vi) {
		t.Fatalf("error type = %T, want ValidationIssue: %v", err, err)
	}
	sawError := false
	for _, is := range issues {
		if is.Code == "component.field.unknown" {
			if is.Severity != SeverityError {
				t.Errorf("strict unknown-field severity = %v, want Error", is.Severity)
			}
			sawError = true
		}
	}
	if !sawError {
		t.Error("expected a promoted component.field.unknown error")
	}
}

// TestUnknownFields_Unit covers the detector directly, including that
// free-form maps and bare-string subscribe entries are not linted.
func TestUnknownFields_Unit(t *testing.T) {
	raw := map[string]any{
		"apiVersion": "sourceplane.io/v1",
		"kind":       "Component",
		"bogusRoot":  1,
		"metadata":   map[string]any{"name": "x", "weird": 2},
		"spec": map[string]any{
			"type":    "t",
			"mystery": 3,
			// free-form maps must NOT be linted for inner keys.
			"parameters": map[string]any{"anything": "ok"},
			"labels":     map[string]any{"any-key": "ok"},
			"subscribe": map[string]any{
				"environments": []any{
					"prod-shorthand", // bare string: skipped
					map[string]any{"name": "dev", "oops": true},
				},
			},
		},
	}
	got := unknownFields(raw)
	want := []string{
		"/bogusRoot",
		"/metadata/weird",
		"/spec/mystery",
		"/spec/subscribe/environments/1/oops",
	}
	if len(got) != len(want) {
		t.Fatalf("unknownFields = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("unknownFields[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}
