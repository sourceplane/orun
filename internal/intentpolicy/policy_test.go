package intentpolicy

import (
	"errors"
	"strings"
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func mustParse(t *testing.T, source string, raw map[string]interface{}) Layer {
	t.Helper()
	set, v := Parse(source, raw)
	if len(v) > 0 {
		t.Fatalf("Parse(%s): unexpected violations: %v", source, v)
	}
	return Layer{Source: source, Set: set}
}

func TestParseAcceptsTypedKeysAndStringBooleans(t *testing.T) {
	set, v := Parse("environment:production", map[string]interface{}{
		"requireApproval":     "true",
		"requireCleanGitTree": false,
		"requireProfile":      []interface{}{"release", "*-release"},
		"pinnedParameters": map[string]interface{}{
			"*":         map[string]interface{}{"region": "us-east-1"},
			"terraform": map[string]interface{}{"terraformVersion": "1.9.8"},
		},
	})
	if len(v) > 0 {
		t.Fatalf("unexpected violations: %v", v)
	}
	if !set.RequireApproval || set.RequireCleanGitTree {
		t.Fatalf("booleans parsed wrong: %+v", set)
	}
	if len(set.RequireProfile) != 2 || set.PinnedParameters["terraform"]["terraformVersion"] != "1.9.8" {
		t.Fatalf("structured values parsed wrong: %+v", set)
	}
}

func TestParseRejectsUnknownAndIllTypedPolicies(t *testing.T) {
	_, v := Parse("group:platform", map[string]interface{}{
		"requireAproval":   true,
		"requireApproval":  "yes",
		"requireProfile":   []interface{}{},
		"pinnedParameters": map[string]interface{}{"*": map[string]interface{}{"path": "./x"}},
	})
	got := map[string]string{}
	for _, x := range v {
		got[x.Policy] = x.Message
	}
	for _, key := range []string{"requireAproval", "requireApproval", "requireProfile", "pinnedParameters"} {
		if _, ok := got[key]; !ok {
			t.Errorf("expected a violation for %s, got %v", key, v)
		}
	}
	if !strings.Contains(got["requireAproval"], "unknown policy") {
		t.Errorf("unknown key message = %q", got["requireAproval"])
	}
}

func TestPinnedParameterOverrideByComponentIsAViolation(t *testing.T) {
	env := mustParse(t, "environment:production", map[string]interface{}{
		"pinnedParameters": map[string]interface{}{"terraform": map[string]interface{}{"terraformVersion": "1.9.8"}},
	})
	params := map[string]interface{}{"terraformVersion": "1.10.0"}
	_, v := Enforce([]Layer{env}, Instance{
		Component: "network", Environment: "production", Type: "terraform",
		ComponentParameters: map[string]interface{}{"terraformVersion": "1.10.0"},
		Parameters:          params,
	})
	if len(v) != 1 || v[0].Policy != "pinnedParameters.terraformVersion" || v[0].Source != "environment:production" {
		t.Fatalf("want one pinnedParameters violation, got %v", v)
	}
	if !strings.Contains(v[0].Message, `component sets terraformVersion = "1.10.0"`) {
		t.Fatalf("message = %q", v[0].Message)
	}
}

func TestPinnedParameterOverrideBySubscriptionIsAViolation(t *testing.T) {
	group := mustParse(t, "group:platform", map[string]interface{}{
		"pinnedParameters": map[string]interface{}{"*": map[string]interface{}{"replicas": 3}},
	})
	_, v := Enforce([]Layer{group}, Instance{
		Component: "api", Environment: "staging", Type: "helm",
		SubscriptionParameters: map[string]interface{}{"replicas": 1},
		Parameters:             map[string]interface{}{"replicas": 1},
	})
	if len(v) != 1 || !strings.Contains(v[0].Message, "subscription sets replicas = 1") {
		t.Fatalf("want a subscription override violation, got %v", v)
	}
}

func TestPinnedParameterAppliedWhenUnsetAndRecorded(t *testing.T) {
	group := mustParse(t, "group:platform", map[string]interface{}{
		"pinnedParameters": map[string]interface{}{
			"*":    map[string]interface{}{"region": "us-east-1", "namespace": "{{ .environment }}-apps"},
			"helm": map[string]interface{}{"region": "eu-west-1"},
		},
	})
	params := map[string]interface{}{"region": "from-defaults"}
	eff, v := Enforce([]Layer{group}, Instance{
		Component: "api", Environment: "staging", Type: "helm",
		ComponentParameters: map[string]interface{}{"region": "eu-west-1"}, // same value: allowed
		Parameters:          params,
		Interpolate:         func(s string) string { return strings.ReplaceAll(s, "{{ .environment }}", "staging") },
	})
	if len(v) != 0 {
		t.Fatalf("unexpected violations: %v", v)
	}
	if params["region"] != "eu-west-1" || params["namespace"] != "staging-apps" {
		t.Fatalf("pins not applied (type-specific beats *, values interpolated): %v", params)
	}
	if eff == nil || eff.PinnedParameters["region"] != "eu-west-1" || eff.Sources["pinnedParameters"][0] != "group:platform" {
		t.Fatalf("effective policies not recorded: %+v", eff)
	}
}

func TestConflictingPinsAcrossLayersAreAViolation(t *testing.T) {
	group := mustParse(t, "group:platform", map[string]interface{}{
		"pinnedParameters": map[string]interface{}{"*": map[string]interface{}{"region": "us-east-1"}},
	})
	env := mustParse(t, "environment:production", map[string]interface{}{
		"pinnedParameters": map[string]interface{}{"*": map[string]interface{}{"region": "eu-west-1"}},
	})
	_, v := Enforce([]Layer{group, env}, Instance{Component: "api", Environment: "production", Type: "helm", Parameters: map[string]interface{}{}})
	if len(v) != 1 || !strings.Contains(v[0].Message, "conflicts with group:platform") {
		t.Fatalf("want a conflict violation, got %v", v)
	}
}

func TestRequireProfile(t *testing.T) {
	env := mustParse(t, "environment:production", map[string]interface{}{"requireProfile": "release*"})
	inst := Instance{Component: "api", Environment: "production", Type: "helm", ProfileChecked: true, Parameters: map[string]interface{}{}}

	inst.ProfileName = "release"
	if _, v := Enforce([]Layer{env}, inst); len(v) != 0 {
		t.Fatalf("matching profile must pass: %v", v)
	}
	inst.ProfileName = "verify"
	if _, v := Enforce([]Layer{env}, inst); len(v) != 1 || v[0].Policy != KeyRequireProfile {
		t.Fatalf("non-matching profile must fail: %v", v)
	}
	inst.ProfileName = ""
	if _, v := Enforce([]Layer{env}, inst); len(v) != 1 || !strings.Contains(v[0].Message, "resolves no execution profile") {
		t.Fatalf("no profile must fail: %v", v)
	}
	inst.ProfileChecked = false
	eff, v := Enforce([]Layer{env}, inst)
	if len(v) != 0 || eff == nil || len(eff.RequireProfile) != 1 {
		t.Fatalf("without a registry the policy is recorded but not checked: %v %+v", v, eff)
	}
}

func TestProfilePoliciesUnionWithIntentLayers(t *testing.T) {
	env := mustParse(t, "environment:production", map[string]interface{}{"requireApproval": true})
	eff, v := Enforce([]Layer{env}, Instance{
		Component: "net", Environment: "production", Type: "terraform",
		ProfileChecked: true, ProfileName: "release", ProfileRef: "terraform.release",
		ProfilePolicies: &model.ProfilePolicies{RequireApproval: true, RequireCleanGitTree: true, RequirePinnedTerraformVersion: true},
		Parameters:      map[string]interface{}{"terraformVersion": "1.9.8"},
	})
	if len(v) != 0 {
		t.Fatalf("unexpected violations: %v", v)
	}
	if !eff.RequireApproval || !eff.RequireCleanGitTree || !eff.RequirePinnedTerraformVersion {
		t.Fatalf("profile policies not carried: %+v", eff)
	}
	if got := strings.Join(eff.Sources["requireApproval"], ","); got != "environment:production,profile:terraform.release" {
		t.Fatalf("requireApproval sources = %q", got)
	}
}

func TestRequirePinnedTerraformVersion(t *testing.T) {
	profile := &model.ProfilePolicies{RequirePinnedTerraformVersion: true}
	run := func(params map[string]interface{}, pol *model.ProfilePolicies, layers ...Layer) []Violation {
		_, v := Enforce(layers, Instance{
			Component: "net", Environment: "production", Type: "terraform",
			ProfileName: "release", ProfilePolicies: pol, Parameters: params,
		})
		return v
	}
	for _, ok := range []string{"1.9.8", "v1.9.8", "1.10.0-rc1"} {
		if v := run(map[string]interface{}{"terraformVersion": ok}, profile); len(v) != 0 {
			t.Errorf("%q must pass: %v", ok, v)
		}
	}
	for _, bad := range []interface{}{"~> 1.9", ">= 1.5", "latest", "1.9", "1.x", 1.9} {
		if v := run(map[string]interface{}{"terraformVersion": bad}, profile); len(v) != 1 {
			t.Errorf("%v must fail, got %v", bad, v)
		}
	}
	if v := run(map[string]interface{}{}, profile); len(v) != 1 || !strings.Contains(v[0].Message, "not set") {
		t.Errorf("a profile requiring a pin must fail when terraformVersion is unset: %v", v)
	}
	env := mustParse(t, "environment:production", map[string]interface{}{"requirePinnedTerraformVersion": true})
	if v := run(map[string]interface{}{}, nil, env); len(v) != 0 {
		t.Errorf("an intent-level policy does not apply to a component without terraformVersion: %v", v)
	}
	if v := run(map[string]interface{}{"terraformVersion": "~> 1.9"}, nil, env); len(v) != 1 {
		t.Errorf("an intent-level policy applies to a component with terraformVersion: %v", v)
	}
}

func TestNoPoliciesYieldsNil(t *testing.T) {
	env := mustParse(t, "environment:dev", map[string]interface{}{"requireApproval": "false"})
	eff, v := Enforce([]Layer{env}, Instance{Component: "api", Environment: "dev", Parameters: map[string]interface{}{}})
	if eff != nil || len(v) != 0 {
		t.Fatalf("want nil effective policies, got %+v %v", eff, v)
	}
}

func TestNewErrorIsStructuredAndSorted(t *testing.T) {
	err := NewError([]Violation{
		{Policy: "requireProfile", Source: "environment:prod", Component: "b", Environment: "prod", Message: "x"},
		{Policy: "requireProfile", Source: "environment:prod", Component: "a", Environment: "prod", Message: "y"},
	})
	var perr *Error
	if !errors.As(err, &perr) || perr.Violations[0].Component != "a" {
		t.Fatalf("want sorted *Error, got %#v", err)
	}
	if !strings.HasPrefix(err.Error(), "policy check failed (2 violations):") {
		t.Fatalf("error text = %q", err.Error())
	}
	if NewError(nil) != nil {
		t.Fatalf("no violations must be a nil error")
	}
}
