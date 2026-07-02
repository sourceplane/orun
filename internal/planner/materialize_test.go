package planner

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/render"
)

func TestResolveMaterialize_SubsetOfBindingsPasses(t *testing.T) {
	bindings := []model.ResolvedSecretBinding{
		{Key: "DATABASE_URL", AsEnv: "DATABASE_URL", Required: true},
		{Key: "STRIPE_KEY", AsEnv: "STRIPE_KEY", Required: true},
	}
	spec := &model.MaterializeSpec{Target: "cloudflare-worker", Secrets: []string{"STRIPE_KEY", "DATABASE_URL"}}
	got, err := resolveMaterialize(spec, bindings, nil)
	if err != nil {
		t.Fatalf("resolveMaterialize: %v", err)
	}
	if got.Target != "cloudflare-worker" {
		t.Errorf("target: %q", got.Target)
	}
	// Sorted for a stable plan.
	if len(got.Secrets) != 2 || got.Secrets[0] != "DATABASE_URL" || got.Secrets[1] != "STRIPE_KEY" {
		t.Errorf("expected sorted subset, got %v", got.Secrets)
	}
}

func TestResolveMaterialize_TranslatesBindingKeyToAsEnv(t *testing.T) {
	bindings := []model.ResolvedSecretBinding{
		{Key: "TF_API_TOKEN", AsEnv: "TERRAFORM_TOKEN", Required: true},
	}
	spec := &model.MaterializeSpec{Target: "cloudflare-worker", Secrets: []string{"TF_API_TOKEN"}}
	got, err := resolveMaterialize(spec, bindings, nil)
	if err != nil {
		t.Fatalf("resolveMaterialize: %v", err)
	}
	if len(got.Secrets) != 1 || got.Secrets[0] != "TERRAFORM_TOKEN" {
		t.Errorf("materialize.secrets must key by the injected AsEnv, got %v", got.Secrets)
	}
}

func TestResolveMaterialize_SecretEnvKeyAllowed(t *testing.T) {
	secretEnv := map[string]string{"DATABASE_URL": "secret://acme/api/prod/DATABASE_URL"}
	spec := &model.MaterializeSpec{Target: "cloudflare-worker", Secrets: []string{"DATABASE_URL"}}
	got, err := resolveMaterialize(spec, nil, secretEnv)
	if err != nil {
		t.Fatalf("resolveMaterialize: %v", err)
	}
	if len(got.Secrets) != 1 || got.Secrets[0] != "DATABASE_URL" {
		t.Errorf("secretEnv AsEnv should be accepted, got %v", got.Secrets)
	}
}

func TestResolveMaterialize_NonSubsetKeyIsCompileError(t *testing.T) {
	bindings := []model.ResolvedSecretBinding{{Key: "DATABASE_URL", AsEnv: "DATABASE_URL", Required: true}}
	spec := &model.MaterializeSpec{Target: "cloudflare-worker", Secrets: []string{"NOT_A_BINDING"}}
	_, err := resolveMaterialize(spec, bindings, nil)
	if err == nil {
		t.Fatal("expected compile error for a non-subset materialize.secrets key")
	}
	if !strings.Contains(err.Error(), "NOT_A_BINDING") {
		t.Errorf("error should name the offending key, got: %v", err)
	}
}

func TestResolveMaterialize_NilSpecIsNil(t *testing.T) {
	got, err := resolveMaterialize(nil, nil, nil)
	if err != nil || got != nil {
		t.Fatalf("nil materialize must resolve to nil, nil; got %v, %v", got, err)
	}
}

// TestPlanJobs_MaterializeFlowsOntoPlanJobValueFree is the end-to-end assertion
// that a profile's materialize block lands on PlanJob.Materialize value-free.
func TestPlanJobs_MaterializeFlowsOntoPlanJobValueFree(t *testing.T) {
	job := &model.JobSpec{Name: "deploy", Steps: []model.Step{{Name: "apply", Run: "echo hi"}}}
	info := &CompositionInfo{
		Type:       "cloudflare-worker",
		DefaultJob: job,
		JobMap:     map[string]*model.JobSpec{"deploy": job},
		ExecutionProfiles: map[string]model.ExecutionProfile{
			"worker-deploy": {
				Jobs: map[string]model.ProfileJobSpec{"deploy": {}},
				SecretBindings: map[string]model.SecretBinding{
					"DATABASE_URL": {Required: true},
					"STRIPE_KEY":   {Required: true},
				},
				Materialize: &model.MaterializeSpec{
					Target:   "cloudflare-worker",
					Secrets:  []string{"DATABASE_URL", "STRIPE_KEY"},
					OnRotate: "redeploy",
				},
			},
		},
	}
	jp := NewJobPlanner(map[string]*CompositionInfo{"cloudflare-worker": info})
	jp.Workspace = "acme"
	jp.Project = "api"

	instances := map[string][]*model.ComponentInstance{
		"prod": {{
			ComponentName: "api",
			Environment:   "prod",
			Type:          "cloudflare-worker",
			ProfileName:   "worker-deploy",
			ProfileSource: "subscription",
			Enabled:       true,
		}},
	}

	jobInstances, err := jp.PlanJobs(instances)
	if err != nil {
		t.Fatalf("PlanJobs: %v", err)
	}
	var ji *model.JobInstance
	for _, j := range jobInstances {
		ji = j
	}
	if ji == nil || ji.Materialize == nil {
		t.Fatal("materialize did not flow onto the job instance")
	}

	plan := render.NewRenderer().RenderPlan(model.Metadata{Name: "p"}, jobInstances, nil)
	if len(plan.Jobs) != 1 {
		t.Fatalf("expected 1 plan job, got %d", len(plan.Jobs))
	}
	mat := plan.Jobs[0].Materialize
	if mat == nil {
		t.Fatal("PlanJob.Materialize is nil")
	}
	if mat.Target != "cloudflare-worker" {
		t.Errorf("target: %q", mat.Target)
	}
	if len(mat.Secrets) != 2 || mat.Secrets[0] != "DATABASE_URL" || mat.Secrets[1] != "STRIPE_KEY" {
		t.Errorf("unexpected materialize secrets: %v", mat.Secrets)
	}

	// Structural leak guard: the rendered materialize block carries key names
	// only — no value field, no onRotate leaking a value, nothing.
	blob, _ := json.Marshal(mat)
	if strings.Contains(string(blob), "value") {
		t.Errorf("plan materialize must be value-free: %s", blob)
	}
}

func TestPlanJobs_NonSubsetMaterializeIsCompileError(t *testing.T) {
	job := &model.JobSpec{Name: "deploy", Steps: []model.Step{{Name: "apply", Run: "echo hi"}}}
	info := &CompositionInfo{
		Type:       "cloudflare-worker",
		DefaultJob: job,
		JobMap:     map[string]*model.JobSpec{"deploy": job},
		ExecutionProfiles: map[string]model.ExecutionProfile{
			"worker-deploy": {
				Jobs:           map[string]model.ProfileJobSpec{"deploy": {}},
				SecretBindings: map[string]model.SecretBinding{"DATABASE_URL": {Required: true}},
				Materialize: &model.MaterializeSpec{
					Target:  "cloudflare-worker",
					Secrets: []string{"DATABASE_URL", "STRIPE_KEY"}, // STRIPE_KEY is not a binding
				},
			},
		},
	}
	jp := NewJobPlanner(map[string]*CompositionInfo{"cloudflare-worker": info})
	jp.Workspace = "acme"
	jp.Project = "api"

	instances := map[string][]*model.ComponentInstance{
		"prod": {{
			ComponentName: "api", Environment: "prod", Type: "cloudflare-worker",
			ProfileName: "worker-deploy", ProfileSource: "subscription", Enabled: true,
		}},
	}
	if _, err := jp.PlanJobs(instances); err == nil {
		t.Fatal("expected compile error for a non-subset materialize key")
	}
}
