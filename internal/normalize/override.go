package normalize

import (
	"fmt"
	"strings"

	"github.com/sourceplane/orun/internal/model"
)

// EnforceOverridePolicy validates that intent-level overrides comply with the stack's override policy.
// Returns nil if no policy is set (all overrides allowed) or if all overrides are valid.
func EnforceOverridePolicy(intent *model.Intent, policy *model.StackOverridePolicySpec, stackProfiles map[string]model.ExecutionProfile) error {
	if policy == nil {
		return nil
	}

	if !strings.EqualFold(policy.Default, "deny") {
		return nil
	}

	// Check intent-level profile overrides
	for profileName, intentProfile := range intent.Execution.Profiles {
		if _, isStackProfile := stackProfiles[profileName]; !isStackProfile {
			continue
		}

		for compositionType, controls := range intentProfile.Controls {
			for controlKey := range controls {
				path := fmt.Sprintf("profiles.%s.controls.%s.%s", profileName, compositionType, controlKey)

				if isDenied(policy.Deny, path) {
					return fmt.Errorf("override policy violation: %s is explicitly denied by the stack", path)
				}

				if !isProfileControlAllowed(policy, profileName, compositionType, controlKey) {
					return fmt.Errorf("override policy violation: intent overrides %s which is not allowed by the stack", path)
				}
			}
		}
	}

	// Check environment defaults and policies
	for envName, env := range intent.Environments {
		if env.Defaults != nil {
			for key := range env.Defaults {
				if !isEnvironmentDefaultAllowed(policy, key) {
					return fmt.Errorf("override policy violation: environment %q sets default %q which is not allowed by the stack", envName, key)
				}
			}
		}

		if env.Policies != nil {
			for key := range env.Policies {
				if !isEnvironmentPolicyAllowed(policy, key) {
					return fmt.Errorf("override policy violation: environment %q sets policy %q which is not allowed by the stack", envName, key)
				}
			}
		}
	}

	return nil
}

func isDenied(denyList []string, path string) bool {
	for _, pattern := range denyList {
		if matchesOverridePath(pattern, path) {
			return true
		}
	}
	return false
}

func matchesOverridePath(pattern, path string) bool {
	patternParts := strings.Split(pattern, ".")
	pathParts := strings.Split(path, ".")

	if len(patternParts) != len(pathParts) {
		return false
	}

	for i, pp := range patternParts {
		if pp == "*" {
			continue
		}
		if pp != pathParts[i] {
			return false
		}
	}
	return true
}

func isProfileControlAllowed(policy *model.StackOverridePolicySpec, profileName, compositionType, controlKey string) bool {
	if policy.Allow.Intent.Profiles == nil {
		return false
	}

	profilePolicy, ok := policy.Allow.Intent.Profiles[profileName]
	if !ok {
		return false
	}

	if profilePolicy.Controls == nil {
		return false
	}

	compositionControls, ok := profilePolicy.Controls[compositionType]
	if !ok {
		return false
	}

	_, ok = compositionControls[controlKey]
	return ok
}

func isEnvironmentDefaultAllowed(policy *model.StackOverridePolicySpec, key string) bool {
	for _, allowed := range policy.Allow.Intent.Environments.Defaults {
		if allowed == key || matchesWildcardKey(allowed, key) {
			return true
		}
	}
	return false
}

func isEnvironmentPolicyAllowed(policy *model.StackOverridePolicySpec, key string) bool {
	for _, allowed := range policy.Allow.Intent.Environments.Policies {
		if allowed == key || matchesWildcardKey(allowed, key) {
			return true
		}
	}
	return false
}

func matchesWildcardKey(pattern, key string) bool {
	if strings.HasSuffix(pattern, ".*") {
		prefix := strings.TrimSuffix(pattern, ".*")
		return strings.HasPrefix(key, prefix+".")
	}
	if strings.HasSuffix(pattern, "*") {
		prefix := strings.TrimSuffix(pattern, "*")
		return strings.HasPrefix(key, prefix)
	}
	return pattern == key
}
