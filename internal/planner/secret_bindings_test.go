package planner

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/render"
)

func TestMergeBindingRefs_MapsToProjectEnvWithAsEnvDefault(t *testing.T) {
	bindings := []model.ResolvedSecretBinding{
		{Key: "AWS_ROLE_ARN", AsEnv: "AWS_ROLE_ARN", Required: true},
		{Key: "TF_API_TOKEN", AsEnv: "TERRAFORM_TOKEN", Required: true}, // AsEnv override
	}
	got, err := mergeBindingRefs(nil, bindings, "acme", "api", "prod")
	if err != nil {
		t.Fatalf("mergeBindingRefs: %v", err)
	}
	if got["AWS_ROLE_ARN"] != "secret://acme/api/prod/AWS_ROLE_ARN" {
		t.Errorf("AsEnv default / scope mapping wrong: %q", got["AWS_ROLE_ARN"])
	}
	if got["TERRAFORM_TOKEN"] != "secret://acme/api/prod/TF_API_TOKEN" {
		t.Errorf("AsEnv override should key by AsEnv but ref by Key: %q", got["TERRAFORM_TOKEN"])
	}
}

func TestMergeBindingRefs_RequiredUnmappableIsError(t *testing.T) {
	bindings := []model.ResolvedSecretBinding{{Key: "AWS_ROLE_ARN", AsEnv: "AWS_ROLE_ARN", Required: true}}
	// No workspace scope resolvable.
	_, err := mergeBindingRefs(nil, bindings, "", "api", "prod")
	if err == nil {
		t.Fatal("expected compile error for required-but-unmappable binding")
	}
	if !strings.Contains(err.Error(), "AWS_ROLE_ARN") {
		t.Errorf("error should name the binding, got: %v", err)
	}
}

func TestMergeBindingRefs_OptionalUnmappableIsDropped(t *testing.T) {
	bindings := []model.ResolvedSecretBinding{{Key: "SLACK_WEBHOOK", AsEnv: "SLACK_WEBHOOK", Required: false}}
	got, err := mergeBindingRefs(nil, bindings, "", "", "prod")
	if err != nil {
		t.Fatalf("optional unmappable binding must not error: %v", err)
	}
	if _, ok := got["SLACK_WEBHOOK"]; ok {
		t.Errorf("optional unmappable binding should be dropped, got %v", got)
	}
}

func TestMergeBindingRefs_AgreeingSecretEnvIsOK(t *testing.T) {
	secretEnv := map[string]string{"DATABASE_URL": "secret://acme/api/prod/DATABASE_URL"}
	bindings := []model.ResolvedSecretBinding{{Key: "DATABASE_URL", AsEnv: "DATABASE_URL", Required: true}}
	got, err := mergeBindingRefs(secretEnv, bindings, "acme", "api", "prod")
	if err != nil {
		t.Fatalf("agreeing binding+secretEnv must be OK: %v", err)
	}
	if got["DATABASE_URL"] != "secret://acme/api/prod/DATABASE_URL" {
		t.Errorf("unexpected ref: %q", got["DATABASE_URL"])
	}
}

func TestMergeBindingRefs_DisagreeingSecretEnvIsError(t *testing.T) {
	secretEnv := map[string]string{"DATABASE_URL": "secret://acme/api/prod/OTHER_KEY"}
	bindings := []model.ResolvedSecretBinding{{Key: "DATABASE_URL", AsEnv: "DATABASE_URL", Required: true}}
	_, err := mergeBindingRefs(secretEnv, bindings, "acme", "api", "prod")
	if err == nil {
		t.Fatal("expected error when a binding and secretEnv bind the same AsEnv to different refs")
	}
	if !strings.Contains(err.Error(), "different references") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestPlanJobs_MapsSecretBindingsIntoPlanSecretRefs is the end-to-end assertion
// that a profile's secretBindings land on PlanJob.SecretRefs for the right
// (project, env), deduped and sorted by the renderer.
func TestPlanJobs_MapsSecretBindingsIntoPlanSecretRefs(t *testing.T) {
	job := &model.JobSpec{Name: "deploy", Steps: []model.Step{{Name: "apply", Run: "echo hi"}}}
	info := &CompositionInfo{
		Type:       "terraform",
		DefaultJob: job,
		JobMap:     map[string]*model.JobSpec{"deploy": job},
		ExecutionProfiles: map[string]model.ExecutionProfile{
			"terraform-release": {
				Jobs: map[string]model.ProfileJobSpec{"deploy": {}},
				SecretBindings: map[string]model.SecretBinding{
					"AWS_ROLE_ARN": {Required: true},
					"TF_API_TOKEN": {Required: true},
				},
			},
		},
	}
	jp := NewJobPlanner(map[string]*CompositionInfo{"terraform": info})
	jp.Workspace = "acme"
	jp.Project = "api"

	instances := map[string][]*model.ComponentInstance{
		"prod": {{
			ComponentName: "api",
			Environment:   "prod",
			Type:          "terraform",
			ProfileName:   "terraform-release",
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
	if ji == nil {
		t.Fatal("no job instance produced")
	}
	if len(ji.SecretBindings) != 2 {
		t.Fatalf("expected 2 resolved bindings, got %d", len(ji.SecretBindings))
	}

	plan := render.NewRenderer().RenderPlan(model.Metadata{Name: "p"}, jobInstances, nil)
	if len(plan.Jobs) != 1 {
		t.Fatalf("expected 1 plan job, got %d", len(plan.Jobs))
	}
	refs := plan.Jobs[0].SecretRefs
	if len(refs) != 2 {
		t.Fatalf("expected 2 secret refs, got %d: %+v", len(refs), refs)
	}
	// Renderer sorts by AsEnv.
	if refs[0].AsEnv != "AWS_ROLE_ARN" || refs[0].Ref != "secret://acme/api/prod/AWS_ROLE_ARN" {
		t.Errorf("unexpected first ref: %+v", refs[0])
	}
	if refs[1].AsEnv != "TF_API_TOKEN" || refs[1].Ref != "secret://acme/api/prod/TF_API_TOKEN" {
		t.Errorf("unexpected second ref: %+v", refs[1])
	}
}

func TestPlanJobs_RequiredBindingUnmappableIsCompileError(t *testing.T) {
	job := &model.JobSpec{Name: "deploy", Steps: []model.Step{{Name: "apply", Run: "echo hi"}}}
	info := &CompositionInfo{
		Type:       "terraform",
		DefaultJob: job,
		JobMap:     map[string]*model.JobSpec{"deploy": job},
		ExecutionProfiles: map[string]model.ExecutionProfile{
			"terraform-release": {
				Jobs:           map[string]model.ProfileJobSpec{"deploy": {}},
				SecretBindings: map[string]model.SecretBinding{"AWS_ROLE_ARN": {Required: true}},
			},
		},
	}
	jp := NewJobPlanner(map[string]*CompositionInfo{"terraform": info})
	// No workspace/project scope resolvable.

	instances := map[string][]*model.ComponentInstance{
		"prod": {{
			ComponentName: "api", Environment: "prod", Type: "terraform",
			ProfileName: "terraform-release", ProfileSource: "subscription", Enabled: true,
		}},
	}
	if _, err := jp.PlanJobs(instances); err == nil {
		t.Fatal("expected compile error for required-but-unmappable binding")
	}
}
