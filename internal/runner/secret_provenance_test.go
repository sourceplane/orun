package runner

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/execmodel"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/redact"
)

// TestResolveResponseProvenanceSealedValueFree drives a run whose resolver
// records the resolve response's resolved[] provenance, and asserts it lands on
// the sealed job record with key/version/decisionId present and no value
// anywhere (Invariant 6).
func TestResolveResponseProvenanceSealedValueFree(t *testing.T) {
	const secretValue = "postgres://real-secret-value"

	exec := &envCapturingExecutor{echo: "ok"}
	r := newSecretTestRunner(exec)
	r.Redactor = redact.New()
	r.Hooks = &RunnerHooks{
		ResolveJobSecrets: func(jobID string, refs []model.PlanSecretRef) (map[string]string, error) {
			// Mirror the remote resolver: return the value map AND record the
			// value-free provenance half onto the runner.
			r.RecordSecretProvenance(jobID, []execmodel.SecretResolution{
				{Key: "DATABASE_URL", Version: 9, Scope: "environment", DecisionID: "dec_abc123"},
			})
			return map[string]string{"DATABASE_URL": secretValue}, nil
		},
	}

	if err := r.Run(secretPlan()); err != nil {
		t.Fatalf("run: %v", err)
	}

	snap := r.SnapshotState()
	if snap == nil {
		t.Fatal("SnapshotState returned nil after run")
	}
	js := snap.Jobs["api@deploy"]
	if js == nil {
		t.Fatal("no job state for api@deploy")
	}
	if len(js.SecretProvenance) != 1 {
		t.Fatalf("expected 1 provenance record, got %d", len(js.SecretProvenance))
	}
	got := js.SecretProvenance[0]
	if got.Key != "DATABASE_URL" || got.Version != 9 || got.DecisionID != "dec_abc123" {
		t.Errorf("provenance metadata missing/wrong: %+v", got)
	}
	if got.Scope != "environment" {
		t.Errorf("expected serving scope, got %q", got.Scope)
	}

	// Structural leak guard: the sealed job record must not carry the value.
	blob, err := json.Marshal(js)
	if err != nil {
		t.Fatalf("marshal job state: %v", err)
	}
	if strings.Contains(string(blob), secretValue) {
		t.Fatalf("sealed job record leaked the secret value: %s", blob)
	}
	if !strings.Contains(string(blob), "decisionId") {
		t.Errorf("sealed job record should carry the decision id: %s", blob)
	}
}
