package catalogext_test

import (
	"errors"
	"testing"

	"github.com/sourceplane/orun/internal/catalogext"
)

func TestRegistry_ValidateRegisteredAndUnknown(t *testing.T) {
	r := catalogext.NewRegistry()
	r.Register("x-datadog", func(block any) error {
		m, ok := block.(map[string]any)
		if !ok || m["service"] == nil {
			return errors.New("missing service")
		}
		return nil
	})
	if !r.Known("x-datadog") || r.Known("x-pagerduty") {
		t.Fatal("Known wrong")
	}

	// A valid registered block + an unknown (but namespaced) block: no errors,
	// the unknown is preserved (the caller's map is never mutated).
	ext := map[string]any{
		"x-datadog":   map[string]any{"service": "api"},
		"x-pagerduty": map[string]any{"serviceId": "PX"}, // unknown → preserved, not validated
	}
	if errs := r.Validate(ext); len(errs) != 0 {
		t.Fatalf("unexpected errors: %v", errs)
	}
	if _, ok := ext["x-pagerduty"]; !ok {
		t.Error("unknown extension was dropped")
	}

	// A registered-but-invalid block yields one error.
	bad := map[string]any{"x-datadog": map[string]any{"team": "x"}}
	if errs := r.Validate(bad); len(errs) != 1 {
		t.Fatalf("expected 1 error, got %v", errs)
	}
}

func TestRegistry_NonNamespacedIsError(t *testing.T) {
	r := catalogext.NewRegistry()
	errs := r.Validate(map[string]any{"datadog": map[string]any{}})
	if len(errs) != 1 {
		t.Fatalf("non-namespaced key should error: %v", errs)
	}
}

func TestRegistry_EmptyAndPanic(t *testing.T) {
	r := catalogext.NewRegistry()
	if errs := r.Validate(nil); errs != nil {
		t.Errorf("nil extensions = %v", errs)
	}
	defer func() {
		if recover() == nil {
			t.Error("Register of a non-namespaced key should panic")
		}
	}()
	r.Register("datadog", func(any) error { return nil })
}
