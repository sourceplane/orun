package expand

import (
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/normalize"
)

func secretIntent(componentSecretEnv, subscriptionSecretEnv map[string]string) *model.Intent {
	return &model.Intent{
		Metadata: model.Metadata{Name: "secret-env-test"},
		Environments: map[string]model.Environment{
			"dev": {
				Selectors: model.EnvironmentSelectors{Components: []string{"*"}},
				SecretEnv: map[string]string{
					"SHARED_TOKEN": "secret://acme/api/dev/SHARED_TOKEN",
				},
			},
		},
		Components: []model.Component{
			{
				Name:      "api",
				Type:      "helm",
				Domain:    "platform",
				SecretEnv: componentSecretEnv,
				Subscribe: model.ComponentSubscribe{
					Environments: []model.EnvironmentSubscription{
						{Name: "dev", SecretEnv: subscriptionSecretEnv},
					},
				},
			},
		},
	}
}

func expandSecretIntent(t *testing.T, intent *model.Intent) []*model.ComponentInstance {
	t.Helper()
	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent returned error: %v", err)
	}
	instances, err := NewExpander(normalized).Expand()
	if err != nil {
		t.Fatalf("Expand returned error: %v", err)
	}
	return instances["dev"]
}

func TestExpandMergesSecretEnvWithPrecedence(t *testing.T) {
	intent := secretIntent(
		map[string]string{
			"DATABASE_URL": "secret://acme/api/{{.environment}}/DATABASE_URL",
			"SHARED_TOKEN": "secret://acme/api/dev/COMPONENT_WINS",
		},
		map[string]string{
			"DATABASE_URL": "secret://acme/api/dev/SUBSCRIPTION_WINS",
		},
	)

	instances := expandSecretIntent(t, intent)
	if len(instances) != 1 {
		t.Fatalf("expected 1 instance, got %d", len(instances))
	}
	got := instances[0].SecretEnv

	if got["DATABASE_URL"] != "secret://acme/api/dev/SUBSCRIPTION_WINS" {
		t.Errorf("subscription layer should win: got %q", got["DATABASE_URL"])
	}
	if got["SHARED_TOKEN"] != "secret://acme/api/dev/COMPONENT_WINS" {
		t.Errorf("component layer should override environment: got %q", got["SHARED_TOKEN"])
	}
}

func TestExpandInterpolatesEnvironmentInSecretRefs(t *testing.T) {
	intent := secretIntent(map[string]string{
		"DATABASE_URL": "secret://acme/api/{{.environment}}/DATABASE_URL",
	}, nil)

	instances := expandSecretIntent(t, intent)
	got := instances[0].SecretEnv["DATABASE_URL"]
	if got != "secret://acme/api/dev/DATABASE_URL" {
		t.Errorf("expected interpolated env segment, got %q", got)
	}
}

func TestExpandRejectsLiteralInSecretEnv(t *testing.T) {
	intent := secretIntent(map[string]string{
		"DATABASE_URL": "postgres://user:hunter2@host/db", // a literal — the leak
	}, nil)

	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent returned error: %v", err)
	}
	_, err = NewExpander(normalized).Expand()
	if err == nil {
		t.Fatal("expected leak-guard error for a literal secretEnv value")
	}
	if !strings.Contains(err.Error(), "secretEnv DATABASE_URL") {
		t.Errorf("error should name the offending key, got: %v", err)
	}
	if strings.Contains(err.Error(), "hunter2") {
		t.Errorf("leak-guard error must not echo the literal value: %v", err)
	}
}

func TestExpandRejectsEnvShadowingSecretEnv(t *testing.T) {
	intent := secretIntent(map[string]string{
		"DATABASE_URL": "secret://acme/api/dev/DATABASE_URL",
	}, nil)
	intent.Components[0].Env = map[string]string{
		"DATABASE_URL": "postgres://plaintext",
	}

	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent returned error: %v", err)
	}
	_, err = NewExpander(normalized).Expand()
	if err == nil {
		t.Fatal("expected error when env and secretEnv share a key")
	}
	if !strings.Contains(err.Error(), "declared in both env and secretEnv") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestExpandNoSecretEnvYieldsNil(t *testing.T) {
	intent := secretIntent(nil, nil)
	intent.Environments["dev"] = model.Environment{
		Selectors: model.EnvironmentSelectors{Components: []string{"*"}},
	}

	instances := expandSecretIntent(t, intent)
	if instances[0].SecretEnv != nil {
		t.Errorf("expected nil SecretEnv when nothing is declared, got %v", instances[0].SecretEnv)
	}
}
