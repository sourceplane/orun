package workbridge

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/sourceplane/orun/internal/affected"
)

// TestFromResultMirrorsBlastRadius is the parity assertion: the AffectedSet the
// cloud consumes carries exactly the engine's blast radius (Result.Affected) and
// reverse-dep closure (Result.Dependents) — the same values `orun catalog
// affected` reports. No second closure implementation can drift from the engine.
func TestFromResultMirrorsBlastRadius(t *testing.T) {
	r := affected.Result{
		DirectlyChanged: []string{"sourceplane/orun/api-edge"},
		Dependents:      []string{"sourceplane/orun/web"},
		Affected:        []string{"sourceplane/orun/api-edge", "sourceplane/orun/web"},
	}
	got := FromResult("sourceplane/orun#412", r)

	if got.PR != "sourceplane/orun#412" {
		t.Errorf("PR = %q", got.PR)
	}
	if !reflect.DeepEqual(got.Components, r.Affected) {
		t.Errorf("Components = %v, want the engine's Affected %v", got.Components, r.Affected)
	}
	if !reflect.DeepEqual(got.Dependents, r.Dependents) {
		t.Errorf("Dependents = %v, want %v", got.Dependents, r.Dependents)
	}
}

// TestFromResultNilSafe ensures the wire shape is stable: empty closures emit
// `[]`, never `null`, so the cloud consumer never has to distinguish them.
func TestFromResultNilSafe(t *testing.T) {
	got := FromResult("x", affected.Result{})
	if got.Components == nil || got.Dependents == nil {
		t.Fatalf("slices must be non-nil: %+v", got)
	}
	if len(got.Components) != 0 || len(got.Dependents) != 0 {
		t.Fatalf("empty Result should yield empty slices: %+v", got)
	}
}

// TestAffectedSetWireShape pins the lowerCamelCase JSON the cloud auto-linker
// deserializes.
func TestAffectedSetWireShape(t *testing.T) {
	b, err := json.Marshal(FromResult("pr#1", affected.Result{Affected: []string{"a/b/c"}}))
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	const want = `{"pr":"pr#1","components":["a/b/c"],"dependents":[]}`
	if string(b) != want {
		t.Fatalf("wire shape:\n got  %s\n want %s", b, want)
	}
}
