package expand

import (
	"errors"
	"strings"
	"testing"

	compositionpkg "github.com/sourceplane/orun/internal/composition"
	"github.com/sourceplane/orun/internal/intentpolicy"
	"github.com/sourceplane/orun/internal/model"
	"github.com/sourceplane/orun/internal/normalize"
)

// policyIntent is a one-component intent: a terraform component in group
// "platform" subscribed to "production" with the given parameters.
func policyIntent(envPolicies, groupPolicies map[string]interface{}, params map[string]interface{}) *model.Intent {
	return &model.Intent{
		Metadata: model.Metadata{Name: "policy-test"},
		Groups:   map[string]model.Group{"platform": {Policies: groupPolicies}},
		Environments: map[string]model.Environment{
			"production": {Policies: envPolicies},
		},
		Components: []model.Component{{
			Name:       "network",
			Type:       "terraform",
			Domain:     "platform",
			Parameters: params,
			Subscribe: model.ComponentSubscribe{Environments: []model.EnvironmentSubscription{
				{Name: "production", Profile: "release"},
			}},
		}},
	}
}

func terraformRegistry(policies *model.ProfilePolicies) *compositionpkg.Registry {
	comp := &compositionpkg.Composition{
		Key:            "terraform",
		Name:           "terraform",
		DefaultProfile: "release",
		ExecutionProfiles: map[string]model.ExecutionProfile{
			"release": {Policies: policies, Jobs: map[string]model.ProfileJobSpec{"validate": {}}},
		},
	}
	return &compositionpkg.Registry{
		Types: map[string]*compositionpkg.Composition{"terraform": comp},
		ByKey: map[string]*compositionpkg.Composition{"terraform": comp},
	}
}

func expandPolicyIntent(t *testing.T, intent *model.Intent, registry *compositionpkg.Registry) (*model.ComponentInstance, error) {
	t.Helper()
	normalized, err := normalize.NormalizeIntent(intent)
	if err != nil {
		t.Fatalf("NormalizeIntent: %v", err)
	}
	e := NewExpander(normalized)
	if registry != nil {
		e = e.WithRegistry(registry)
	}
	instances, err := e.Expand()
	if err != nil {
		return nil, err
	}
	if len(instances["production"]) != 1 {
		t.Fatalf("want one production instance, got %v", instances)
	}
	return instances["production"][0], nil
}

func policyViolations(t *testing.T, err error) []intentpolicy.Violation {
	t.Helper()
	var perr *intentpolicy.Error
	if !errors.As(err, &perr) {
		t.Fatalf("want *intentpolicy.Error, got %v", err)
	}
	return perr.Violations
}

func TestExpandRejectsComponentOverridingPinnedParameter(t *testing.T) {
	intent := policyIntent(map[string]interface{}{
		"pinnedParameters": map[string]interface{}{"terraform": map[string]interface{}{"terraformVersion": "1.9.8"}},
	}, nil, map[string]interface{}{"terraformVersion": "1.10.0"})

	_, err := expandPolicyIntent(t, intent, nil)
	v := policyViolations(t, err)
	if len(v) != 1 || v[0].Component != "network" || v[0].Policy != "pinnedParameters.terraformVersion" {
		t.Fatalf("unexpected violations: %v", v)
	}
}

func TestExpandAppliesAndRecordsPinnedParameter(t *testing.T) {
	intent := policyIntent(nil, map[string]interface{}{
		"pinnedParameters": map[string]interface{}{"*": map[string]interface{}{"region": "us-east-1"}},
	}, map[string]interface{}{"stackName": "network"})

	inst, err := expandPolicyIntent(t, intent, nil)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if inst.Parameters["region"] != "us-east-1" {
		t.Fatalf("pinned value not applied: %v", inst.Parameters)
	}
	if inst.Policies == nil || inst.Policies.Sources["pinnedParameters"][0] != "group:platform" {
		t.Fatalf("effective policies not recorded: %+v", inst.Policies)
	}
}

func TestExpandRejectsUnknownPolicyKey(t *testing.T) {
	intent := policyIntent(map[string]interface{}{"requireAproval": true}, nil, nil)
	_, err := expandPolicyIntent(t, intent, nil)
	v := policyViolations(t, err)
	if len(v) != 1 || v[0].Policy != "requireAproval" || v[0].Source != "environment:production" {
		t.Fatalf("unexpected violations: %v", v)
	}
}

func TestExpandProfileRequirePinnedTerraformVersion(t *testing.T) {
	registry := terraformRegistry(&model.ProfilePolicies{RequirePinnedTerraformVersion: true})

	inst, err := expandPolicyIntent(t, policyIntent(nil, nil, map[string]interface{}{"terraformVersion": "1.9.8"}), registry)
	if err != nil {
		t.Fatalf("an exact version must pass: %v", err)
	}
	if !inst.Policies.RequirePinnedTerraformVersion || inst.Policies.Sources["requirePinnedTerraformVersion"][0] != "profile:terraform.release" {
		t.Fatalf("profile policy not recorded: %+v", inst.Policies)
	}

	_, err = expandPolicyIntent(t, policyIntent(nil, nil, map[string]interface{}{"terraformVersion": "~> 1.9"}), registry)
	v := policyViolations(t, err)
	if len(v) != 1 || v[0].Policy != "requirePinnedTerraformVersion" {
		t.Fatalf("a range must fail: %v", v)
	}
}

func TestExpandRecordsRuntimeProfilePolicies(t *testing.T) {
	registry := terraformRegistry(&model.ProfilePolicies{RequireCleanGitTree: true, RequireApproval: true})
	inst, err := expandPolicyIntent(t, policyIntent(nil, nil, nil), registry)
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	if !inst.Policies.RequireCleanGitTree || !inst.Policies.RequireApproval {
		t.Fatalf("runtime profile policies must be carried to the instance: %+v", inst.Policies)
	}
}

func TestExpandRequireProfile(t *testing.T) {
	registry := terraformRegistry(nil)
	if _, err := expandPolicyIntent(t, policyIntent(map[string]interface{}{"requireProfile": "release"}, nil, nil), registry); err != nil {
		t.Fatalf("matching profile must pass: %v", err)
	}
	_, err := expandPolicyIntent(t, policyIntent(map[string]interface{}{"requireProfile": []interface{}{"deploy"}}, nil, nil), registry)
	v := policyViolations(t, err)
	if len(v) != 1 || !strings.Contains(v[0].Message, `resolves profile "release"`) {
		t.Fatalf("non-matching profile must fail: %v", v)
	}
}
