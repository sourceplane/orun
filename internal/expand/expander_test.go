package expand

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/normalize"
)

func TestExpandSupportsSubscribedComponentsAlongsideSelectorFallback(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "subscription-test"},
		Environments: map[string]model.Environment{
			"development": {
				Selectors: model.EnvironmentSelectors{
					Components: []string{"legacy-*"},
				},
			},
			"staging": {
				Selectors: model.EnvironmentSelectors{
					Components: []string{"legacy-*"},
				},
			},
			"production": {
				Selectors: model.EnvironmentSelectors{
					Domains: []string{"platform"},
				},
			},
		},
		Components: []model.Component{
			{
				Name:   "legacy-api",
				Type:   "helm",
				Domain: "platform",
			},
			{
				Name:   "subscribed-api",
				Type:   "helm",
				Domain: "platform",
				Subscribe: model.ComponentSubscribe{
					Environments: []model.EnvironmentSubscription{
						{Name: "development"},
						{Name: "production"},
					},
				},
				SourcePath: "services/api/component.yaml",
			},
			{
				Name:   "subscribed-identity",
				Type:   "helm",
				Domain: "identity",
				Subscribe: model.ComponentSubscribe{
					Environments: []model.EnvironmentSubscription{
						{Name: "production"},
					},
				},
			},
		},
	}

	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent returned error: %v", err)
	}

	instances, err := NewExpander(normalized).Expand()
	if err != nil {
		t.Fatalf("Expand returned error: %v", err)
	}

	assertInstanceNames(t, instances["development"], "legacy-api", "subscribed-api")
	assertInstanceNames(t, instances["staging"], "legacy-api")
	assertInstanceNames(t, instances["production"], "subscribed-api")

	for _, instance := range instances["development"] {
		if instance.ComponentName == "subscribed-api" && instance.SourcePath != "services/api/component.yaml" {
			t.Fatalf("expected subscribed instance to keep source path, got %q", instance.SourcePath)
		}
	}
}

func assertInstanceNames(t *testing.T, instances []*model.ComponentInstance, expected ...string) {
	t.Helper()
	if len(instances) != len(expected) {
		t.Fatalf("expected %d instances, got %d", len(expected), len(instances))
	}
	for index, name := range expected {
		if instances[index].ComponentName != name {
			t.Fatalf("expected instance %d to be %q, got %q", index, name, instances[index].ComponentName)
		}
	}
}

func TestExpandMergesEnvironmentEnvVars(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "env-merge-test"},
		Environments: map[string]model.Environment{
			"dev": {
				Env: map[string]string{
					"AWS_REGION":       "us-east-1",
					"TF_LOG":           "WARN",
					"NAMESPACE_PREFIX": "dev-",
				},
				Selectors: model.EnvironmentSelectors{
					Components: []string{"*"},
				},
			},
		},
		Components: []model.Component{
			{
				Name: "api-platform",
				Type: "terraform",
				Subscribe: model.ComponentSubscribe{
					Environments: []model.EnvironmentSubscription{
						{
							Name: "dev",
							Env: map[string]string{
								"STACK_NAME":      "api-platform",
								"TF_VAR_replicas": "1",
							},
						},
					},
				},
			},
		},
	}

	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent returned error: %v", err)
	}

	instances, err := NewExpander(normalized).Expand()
	if err != nil {
		t.Fatalf("Expand returned error: %v", err)
	}

	devInstances := instances["dev"]
	if len(devInstances) != 1 {
		t.Fatalf("expected 1 dev instance, got %d", len(devInstances))
	}

	env := devInstances[0].Env
	expectations := map[string]string{
		"AWS_REGION":       "us-east-1",
		"TF_LOG":           "WARN",
		"NAMESPACE_PREFIX": "dev-",
		"STACK_NAME":       "api-platform",
		"TF_VAR_replicas":  "1",
	}

	for k, want := range expectations {
		if got := env[k]; got != want {
			t.Errorf("env[%q] = %q, want %q", k, got, want)
		}
	}
}

func TestExpandSubscriptionEnvOverridesIntentEnv(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "env-override-test"},
		Environments: map[string]model.Environment{
			"dev": {
				Env: map[string]string{
					"AWS_REGION": "us-east-1",
				},
				Selectors: model.EnvironmentSelectors{
					Components: []string{"*"},
				},
			},
		},
		Components: []model.Component{
			{
				Name: "api",
				Type: "terraform",
				Subscribe: model.ComponentSubscribe{
					Environments: []model.EnvironmentSubscription{
						{
							Name: "dev",
							Env: map[string]string{
								"AWS_REGION": "eu-west-1",
							},
						},
					},
				},
			},
		},
	}

	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent returned error: %v", err)
	}

	instances, err := NewExpander(normalized).Expand()
	if err != nil {
		t.Fatalf("Expand returned error: %v", err)
	}

	env := instances["dev"][0].Env
	if env["AWS_REGION"] != "eu-west-1" {
		t.Errorf("expected subscription env to override intent env, got %q", env["AWS_REGION"])
	}
}

func TestExpandEnvTemplateInterpolation(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "env-template-test"},
		Environments: map[string]model.Environment{
			"staging": {
				Env: map[string]string{
					"NAMESPACE": "{{ .environment }}-{{ .component }}",
				},
				Selectors: model.EnvironmentSelectors{
					Components: []string{"*"},
				},
			},
		},
		Components: []model.Component{
			{
				Name:   "web",
				Type:   "helm",
				Domain: "apps",
			},
		},
	}

	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent returned error: %v", err)
	}

	instances, err := NewExpander(normalized).Expand()
	if err != nil {
		t.Fatalf("Expand returned error: %v", err)
	}

	env := instances["staging"][0].Env
	if env["NAMESPACE"] != "staging-web" {
		t.Errorf("expected interpolated env value, got %q", env["NAMESPACE"])
	}
}

func TestExpandNoEnvFieldProducesEmptyMap(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "no-env-test"},
		Environments: map[string]model.Environment{
			"dev": {
				Selectors: model.EnvironmentSelectors{
					Components: []string{"*"},
				},
			},
		},
		Components: []model.Component{
			{
				Name: "svc",
				Type: "terraform",
			},
		},
	}

	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent returned error: %v", err)
	}

	instances, err := NewExpander(normalized).Expand()
	if err != nil {
		t.Fatalf("Expand returned error: %v", err)
	}

	env := instances["dev"][0].Env
	if len(env) != 0 {
		t.Errorf("expected empty env map when no env declared, got %v", env)
	}
}
