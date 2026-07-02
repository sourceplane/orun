package render

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestBuildPlanJobSecretRefsSortedByAsEnv(t *testing.T) {
	job := &model.JobInstance{
		SecretRefs: map[string]string{
			"STRIPE_KEY":   "secret://acme/api/prod/STRIPE_KEY@7",
			"DATABASE_URL": "secret://acme/api/prod/DATABASE_URL",
		},
	}

	refs := buildPlanJobSecretRefs(job)

	if len(refs) != 2 {
		t.Fatalf("expected 2 refs, got %d", len(refs))
	}
	if refs[0].AsEnv != "DATABASE_URL" || refs[1].AsEnv != "STRIPE_KEY" {
		t.Errorf("refs must be sorted by AsEnv, got %q, %q", refs[0].AsEnv, refs[1].AsEnv)
	}
	if refs[1].Ref != "secret://acme/api/prod/STRIPE_KEY@7" {
		t.Errorf("ref carried wrong value: %q", refs[1].Ref)
	}
}

func TestBuildPlanJobSecretRefsNilWhenEmpty(t *testing.T) {
	if got := buildPlanJobSecretRefs(&model.JobInstance{}); got != nil {
		t.Errorf("expected nil for a job without secret refs, got %v", got)
	}
}

// The structural half of Invariant 1: a serialized PlanJob has no field that
// could hold a secret value — secretRefs entries carry exactly {asEnv, ref}.
func TestPlanJobSecretRefsSerializeValueFree(t *testing.T) {
	job := model.PlanJob{
		ID: "deploy-api-prod",
		SecretRefs: []model.PlanSecretRef{
			{AsEnv: "DATABASE_URL", Ref: "secret://acme/api/prod/DATABASE_URL"},
		},
	}
	raw, err := json.Marshal(job)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]interface{}
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	refs, ok := decoded["secretRefs"].([]interface{})
	if !ok || len(refs) != 1 {
		t.Fatalf("expected one serialized secretRef, got %v", decoded["secretRefs"])
	}
	entry := refs[0].(map[string]interface{})
	for field := range entry {
		if field != "asEnv" && field != "ref" {
			t.Errorf("unexpected field %q on a serialized secretRef — no value-shaped fields allowed", field)
		}
	}
	if !strings.HasPrefix(entry["ref"].(string), "secret://") {
		t.Errorf("ref must stay reference-shaped, got %v", entry["ref"])
	}
}
