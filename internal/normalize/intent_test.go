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

func TestPromotionDefaultsApplied(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Environments: map[string]model.Environment{
			"dev": {},
			"staging": {
				Promotion: model.EnvironmentPromotion{
					DependsOn: []model.PromotionDependency{{
						Environment: "dev",
					}},
				},
			},
		},
		Components: []model.Component{
			{Name: "api", Type: "terraform"},
		},
	}

	normalized, err := NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	dep := normalized.Environments["staging"].Promotion.DependsOn[0]
	if dep.Strategy != "same-component" {
		t.Errorf("expected default strategy same-component, got %s", dep.Strategy)
	}
	if dep.Condition != "success" {
		t.Errorf("expected default condition success, got %s", dep.Condition)
	}
	if dep.Satisfy != "same-plan-or-previous-success" {
		t.Errorf("expected default satisfy same-plan-or-previous-success, got %s", dep.Satisfy)
	}
	if dep.Match.Revision != "source" {
		t.Errorf("expected default match.revision source, got %s", dep.Match.Revision)
	}
}

func TestPromotionRejectsSelfReference(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Environments: map[string]model.Environment{
			"dev": {
				Promotion: model.EnvironmentPromotion{
					DependsOn: []model.PromotionDependency{{
						Environment: "dev",
					}},
				},
			},
		},
		Components: []model.Component{
			{Name: "api", Type: "terraform"},
		},
	}

	_, err := NormalizeIntent(intent)
	if err == nil {
		t.Fatal("expected error for self-referencing promotion")
	}
	if !strings.Contains(err.Error(), "cannot reference itself") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPromotionRejectsNonExistentEnvironment(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Environments: map[string]model.Environment{
			"dev": {
				Promotion: model.EnvironmentPromotion{
					DependsOn: []model.PromotionDependency{{
						Environment: "unknown",
					}},
				},
			},
		},
		Components: []model.Component{
			{Name: "api", Type: "terraform"},
		},
	}

	_, err := NormalizeIntent(intent)
	if err == nil {
		t.Fatal("expected error for non-existent environment reference")
	}
	if !strings.Contains(err.Error(), "non-existent environment") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPromotionDetectsCycle(t *testing.T) {
	intent := &model.Intent{
		Metadata: model.Metadata{Name: "test"},
		Environments: map[string]model.Environment{
			"dev": {
				Promotion: model.EnvironmentPromotion{
					DependsOn: []model.PromotionDependency{{
						Environment: "staging",
					}},
				},
			},
			"staging": {
				Promotion: model.EnvironmentPromotion{
					DependsOn: []model.PromotionDependency{{
						Environment: "dev",
					}},
				},
			},
		},
		Components: []model.Component{
			{Name: "api", Type: "terraform"},
		},
	}

	_, err := NormalizeIntent(intent)
	if err == nil {
		t.Fatal("expected error for promotion cycle")
	}
	if !strings.Contains(err.Error(), "cycle detected") {
		t.Fatalf("unexpected error: %v", err)
	}
}
