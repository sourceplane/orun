package normalize

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestValidateRejectsOrunPrefixInIntentRootEnv(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Env: map[string]string{
			"ORUN_PLAN_ID": "fake",
		},
		Environments: map[string]model.Environment{
			"dev": {},
		},
		Components: []model.Component{
			{Name: "api", Type: "terraform"},
		},
	}

	_, err := NormalizeIntent(intent)
	if err == nil {
		t.Fatal("expected error for ORUN_ prefix in intent root env")
	}
	if !strings.Contains(err.Error(), "reserved ORUN_ prefix") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateRejectsOrunPrefixInEnvironmentEnv(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Environments: map[string]model.Environment{
			"dev": {
				Env: map[string]string{
					"ORUN_EXEC_ID": "bad",
				},
			},
		},
		Components: []model.Component{
			{Name: "api", Type: "terraform"},
		},
	}

	_, err := NormalizeIntent(intent)
	if err == nil {
		t.Fatal("expected error for ORUN_ prefix in environment env")
	}
	if !strings.Contains(err.Error(), "reserved ORUN_ prefix") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateRejectsOrunPrefixInComponentRootEnv(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Environments: map[string]model.Environment{
			"dev": {},
		},
		Components: []model.Component{
			{
				Name: "api",
				Type: "terraform",
				Env: map[string]string{
					"ORUN_COMPONENT": "fake",
				},
			},
		},
	}

	_, err := NormalizeIntent(intent)
	if err == nil {
		t.Fatal("expected error for ORUN_ prefix in component root env")
	}
	if !strings.Contains(err.Error(), "reserved ORUN_ prefix") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateRejectsOrunPrefixInSubscriptionEnv(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Environments: map[string]model.Environment{
			"dev": {},
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
								"ORUN_JOB_ID": "fake",
							},
						},
					},
				},
			},
		},
	}

	_, err := NormalizeIntent(intent)
	if err == nil {
		t.Fatal("expected error for ORUN_ prefix in subscription env")
	}
	if !strings.Contains(err.Error(), "reserved ORUN_ prefix") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestValidateAcceptsNormalEnvKeys(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Env: map[string]string{
			"OWNER": "sourceplane",
		},
		Environments: map[string]model.Environment{
			"dev": {
				Env: map[string]string{
					"AWS_REGION": "us-east-1",
				},
			},
		},
		Components: []model.Component{
			{
				Name: "api",
				Type: "terraform",
				Env: map[string]string{
					"REPO": "my-repo",
				},
				Subscribe: model.ComponentSubscribe{
					Environments: []model.EnvironmentSubscription{
						{
							Name: "dev",
							Env: map[string]string{
								"STACK_NAME": "api-platform",
							},
						},
					},
				},
			},
		},
	}

	_, err := NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestNormalizeInitializesComponentEnv(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Environments: map[string]model.Environment{
			"dev": {},
		},
		Components: []model.Component{
			{Name: "api", Type: "terraform"},
		},
	}

	normalized, err := NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	comp := normalized.ComponentIndex["api"]
	if comp.Env == nil {
		t.Fatal("expected component Env to be initialized, got nil")
	}
}

func TestNormalizeCarriesIntentRootEnv(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Env: map[string]string{
			"GLOBAL": "value",
		},
		Environments: map[string]model.Environment{
			"dev": {},
		},
		Components: []model.Component{
			{Name: "api", Type: "terraform"},
		},
	}

	normalized, err := NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if normalized.Env["GLOBAL"] != "value" {
		t.Fatalf("expected intent root env to be carried, got %v", normalized.Env)
	}
}
