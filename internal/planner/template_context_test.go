package planner

import (
	"strings"
	"testing"
	"text/template"

	"github.com/sourceplane/orun/internal/model"
)

func TestTemplateContextBuildLegacyFlatKeys(t *testing.T) {
	tctx := &TemplateContext{
		CompInst: &model.ComponentInstance{
			ComponentName: "api",
			Environment:   "dev",
			Type:          "helm",
			Parameters: map[string]interface{}{
				"namespace":    "default",
				"replicaCount": 3,
			},
		},
		JobID:   "api.dev.deploy",
		JobName: "deploy",
	}

	ctx := tctx.Build()

	if ctx["Component"] != "api" {
		t.Errorf("Component = %v, want api", ctx["Component"])
	}
	if ctx["Environment"] != "dev" {
		t.Errorf("Environment = %v, want dev", ctx["Environment"])
	}
	if ctx["Type"] != "helm" {
		t.Errorf("Type = %v, want helm", ctx["Type"])
	}
	if ctx["namespace"] != "default" {
		t.Errorf("namespace = %v, want default", ctx["namespace"])
	}
	if ctx["replicaCount"] != 3 {
		t.Errorf("replicaCount = %v, want 3", ctx["replicaCount"])
	}
}

func TestTemplateContextBuildOrunNamespace(t *testing.T) {
	tctx := &TemplateContext{
		CompInst: &model.ComponentInstance{
			ComponentName: "network-foundation",
			Environment:   "production",
			Type:          "terraform",
			Domain:        "platform-foundation",
			ProfileName:   "release",
		},
		JobID:   "network-foundation.production.validate",
		JobName: "validate",
	}

	ctx := tctx.Build()

	orun, ok := ctx["orun"].(map[string]interface{})
	if !ok {
		t.Fatal("expected orun to be a map")
	}

	comp := orun["component"].(map[string]interface{})
	if comp["name"] != "network-foundation" {
		t.Errorf("orun.component.name = %v, want network-foundation", comp["name"])
	}
	if comp["type"] != "terraform" {
		t.Errorf("orun.component.type = %v, want terraform", comp["type"])
	}
	if comp["domain"] != "platform-foundation" {
		t.Errorf("orun.component.domain = %v, want platform-foundation", comp["domain"])
	}

	env := orun["environment"].(map[string]interface{})
	if env["name"] != "production" {
		t.Errorf("orun.environment.name = %v, want production", env["name"])
	}

	composition := orun["composition"].(map[string]interface{})
	if composition["type"] != "terraform" {
		t.Errorf("orun.composition.type = %v, want terraform", composition["type"])
	}

	profile := orun["profile"].(map[string]interface{})
	if profile["name"] != "release" {
		t.Errorf("orun.profile.name = %v, want release", profile["name"])
	}

	job := orun["job"].(map[string]interface{})
	if job["id"] != "network-foundation.production.validate" {
		t.Errorf("orun.job.id = %v, want network-foundation.production.validate", job["id"])
	}
	if job["name"] != "validate" {
		t.Errorf("orun.job.name = %v, want validate", job["name"])
	}
}

func TestTemplateContextBuildParametersNamespace(t *testing.T) {
	tctx := &TemplateContext{
		CompInst: &model.ComponentInstance{
			ComponentName: "api",
			Environment:   "dev",
			Type:          "helm",
			Parameters: map[string]interface{}{
				"namespace":    "default",
				"replicaCount": 3,
				"chartPath":    "charts/api",
			},
		},
		JobID:   "api.dev.deploy",
		JobName: "deploy",
	}

	ctx := tctx.Build()

	params, ok := ctx["parameters"].(map[string]interface{})
	if !ok {
		t.Fatal("expected parameters to be a map")
	}
	if params["namespace"] != "default" {
		t.Errorf("parameters.namespace = %v, want default", params["namespace"])
	}
	if params["replicaCount"] != 3 {
		t.Errorf("parameters.replicaCount = %v, want 3", params["replicaCount"])
	}
	if params["chartPath"] != "charts/api" {
		t.Errorf("parameters.chartPath = %v, want charts/api", params["chartPath"])
	}
}

func TestTemplateContextBuildEnvNamespace(t *testing.T) {
	tctx := &TemplateContext{
		CompInst: &model.ComponentInstance{
			ComponentName: "api",
			Environment:   "dev",
			Type:          "helm",
			Env: map[string]string{
				"AWS_REGION": "us-east-1",
				"TF_LOG":     "WARN",
			},
		},
		JobID:   "api.dev.deploy",
		JobName: "deploy",
	}

	ctx := tctx.Build()

	env, ok := ctx["env"].(map[string]interface{})
	if !ok {
		t.Fatal("expected env to be a map")
	}
	if env["AWS_REGION"] != "us-east-1" {
		t.Errorf("env.AWS_REGION = %v, want us-east-1", env["AWS_REGION"])
	}
	if env["TF_LOG"] != "WARN" {
		t.Errorf("env.TF_LOG = %v, want WARN", env["TF_LOG"])
	}
}

func TestTemplateContextBuildEmptyFields(t *testing.T) {
	tctx := &TemplateContext{
		CompInst: &model.ComponentInstance{
			ComponentName: "api",
			Environment:   "dev",
			Type:          "helm",
		},
		JobID:   "api.dev.deploy",
		JobName: "deploy",
	}

	ctx := tctx.Build()

	orun := ctx["orun"].(map[string]interface{})
	comp := orun["component"].(map[string]interface{})
	if comp["domain"] != "" {
		t.Errorf("orun.component.domain = %v, want empty string", comp["domain"])
	}

	profile := orun["profile"].(map[string]interface{})
	if profile["name"] != "" {
		t.Errorf("orun.profile.name = %v, want empty string", profile["name"])
	}

	params := ctx["parameters"].(map[string]interface{})
	if len(params) != 0 {
		t.Errorf("expected empty parameters map, got %v", params)
	}

	env := ctx["env"].(map[string]interface{})
	if len(env) != 0 {
		t.Errorf("expected empty env map, got %v", env)
	}
}

func TestTemplateContextEndToEndRendering(t *testing.T) {
	tctx := &TemplateContext{
		CompInst: &model.ComponentInstance{
			ComponentName: "network-foundation",
			Environment:   "dev",
			Type:          "terraform",
			Domain:        "platform",
			ProfileName:   "pull-request",
			Parameters: map[string]interface{}{
				"terraformDir":     "infra/network",
				"terraformVersion": "1.9.8",
			},
			Env: map[string]string{
				"AWS_REGION": "us-west-2",
			},
		},
		JobID:   "network-foundation.dev.validate",
		JobName: "validate",
	}

	ctx := tctx.Build()

	tests := []struct {
		name     string
		tmplStr  string
		expected string
	}{
		{"legacy flat parameter", "terraform -chdir={{.terraformDir}} validate", "terraform -chdir=infra/network validate"},
		{"legacy Component", "{{.Component}}", "network-foundation"},
		{"legacy Environment", "{{.Environment}}", "dev"},
		{"legacy Type", "{{.Type}}", "terraform"},
		{"namespaced parameter", "{{.parameters.terraformDir}}", "infra/network"},
		{"namespaced orun component name", "{{.orun.component.name}}", "network-foundation"},
		{"namespaced orun environment name", "{{.orun.environment.name}}", "dev"},
		{"namespaced orun composition type", "{{.orun.composition.type}}", "terraform"},
		{"namespaced orun profile name", "{{.orun.profile.name}}", "pull-request"},
		{"namespaced orun job id", "{{.orun.job.id}}", "network-foundation.dev.validate"},
		{"namespaced orun job name", "{{.orun.job.name}}", "validate"},
		{"namespaced orun component domain", "{{.orun.component.domain}}", "platform"},
		{"namespaced env", "{{.env.AWS_REGION}}", "us-west-2"},
		{"mixed usage", "component={{.orun.component.name}} dir={{.parameters.terraformDir}} region={{.env.AWS_REGION}}", "component=network-foundation dir=infra/network region=us-west-2"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tmpl, err := template.New(tt.name).Parse(tt.tmplStr)
			if err != nil {
				t.Fatalf("failed to parse template: %v", err)
			}
			var buf strings.Builder
			if err := tmpl.Execute(&buf, ctx); err != nil {
				t.Fatalf("failed to execute template: %v", err)
			}
			if buf.String() != tt.expected {
				t.Errorf("got %q, want %q", buf.String(), tt.expected)
			}
		})
	}
}
