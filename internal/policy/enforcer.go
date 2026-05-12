package policy

import "fmt"

// EnforcementResult describes the outcome of a policy check.
type EnforcementResult struct {
	Allowed          bool
	RequiresApproval bool
	Reason           string
}

// Enforcer validates job capabilities against environment policies.
type Enforcer struct{}

// NewEnforcer creates a new policy enforcer.
func NewEnforcer() *Enforcer { return &Enforcer{} }

// Enforce checks whether the given capabilities are permitted by the policies.
func (e *Enforcer) Enforce(capabilities []string, policies map[string]interface{}) EnforcementResult {
	if len(capabilities) == 0 {
		return EnforcementResult{Allowed: true}
	}

	denied := extractStringSlice(policies, "deniedCapabilities")
	allowed := extractStringSlice(policies, "allowedCapabilities")
	approval := extractStringSlice(policies, "approvalRequiredCapabilities")

	for _, cap := range capabilities {
		if containsStr(denied, cap) {
			return EnforcementResult{
				Allowed: false,
				Reason:  fmt.Sprintf("capability %q denied by policy", cap),
			}
		}
	}

	if len(allowed) > 0 {
		for _, cap := range capabilities {
			if !containsStr(allowed, cap) {
				return EnforcementResult{
					Allowed: false,
					Reason:  fmt.Sprintf("capability %q not in allowedCapabilities", cap),
				}
			}
		}
	}

	for _, cap := range capabilities {
		if containsStr(approval, cap) {
			return EnforcementResult{
				Allowed:          true,
				RequiresApproval: true,
				Reason:           fmt.Sprintf("capability %q requires approval", cap),
			}
		}
	}

	return EnforcementResult{Allowed: true}
}

func extractStringSlice(m map[string]interface{}, key string) []string {
	val, ok := m[key]
	if !ok {
		return nil
	}

	switch v := val.(type) {
	case []interface{}:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return v
	default:
		return nil
	}
}

func containsStr(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
