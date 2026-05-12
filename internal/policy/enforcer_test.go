package policy

import "testing"

func TestEnforceAllowsWhenNoCapabilities(t *testing.T) {
	e := NewEnforcer()
	result := e.Enforce(nil, map[string]interface{}{})
	if !result.Allowed {
		t.Fatal("expected allowed when no capabilities")
	}
}

func TestEnforceDeniesBlockedCapability(t *testing.T) {
	e := NewEnforcer()
	policies := map[string]interface{}{
		"deniedCapabilities": []interface{}{"terraform.apply"},
	}
	result := e.Enforce([]string{"terraform.apply"}, policies)
	if result.Allowed {
		t.Fatal("expected denied")
	}
	if result.Reason == "" {
		t.Fatal("expected a reason")
	}
}

func TestEnforceAllowsPermittedCapability(t *testing.T) {
	e := NewEnforcer()
	policies := map[string]interface{}{
		"allowedCapabilities": []interface{}{"terraform.plan", "terraform.fmt"},
	}
	result := e.Enforce([]string{"terraform.plan"}, policies)
	if !result.Allowed {
		t.Fatal("expected allowed")
	}
}

func TestEnforceDeniesUnlistedCapability(t *testing.T) {
	e := NewEnforcer()
	policies := map[string]interface{}{
		"allowedCapabilities": []interface{}{"terraform.plan"},
	}
	result := e.Enforce([]string{"terraform.apply"}, policies)
	if result.Allowed {
		t.Fatal("expected denied for unlisted capability")
	}
}

func TestEnforceRequiresApproval(t *testing.T) {
	e := NewEnforcer()
	policies := map[string]interface{}{
		"approvalRequiredCapabilities": []interface{}{"terraform.apply"},
	}
	result := e.Enforce([]string{"terraform.apply"}, policies)
	if !result.Allowed {
		t.Fatal("expected allowed with approval")
	}
	if !result.RequiresApproval {
		t.Fatal("expected requiresApproval")
	}
}

func TestEnforceDeniedTakesPrecedenceOverApproval(t *testing.T) {
	e := NewEnforcer()
	policies := map[string]interface{}{
		"deniedCapabilities":           []interface{}{"terraform.apply"},
		"approvalRequiredCapabilities": []interface{}{"terraform.apply"},
	}
	result := e.Enforce([]string{"terraform.apply"}, policies)
	if result.Allowed {
		t.Fatal("expected denied even with approval")
	}
}

func TestEnforceNoPolicies(t *testing.T) {
	e := NewEnforcer()
	result := e.Enforce([]string{"terraform.plan"}, map[string]interface{}{})
	if !result.Allowed {
		t.Fatal("expected allowed with no policies")
	}
}
