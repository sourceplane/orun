package composition

import (
	"fmt"
	"strings"

	"github.com/sourceplane/orun/internal/model"
)

// ResolvedProfile holds the result of profile resolution for a component/environment pair.
type ResolvedProfile struct {
	Ref    string // e.g. "terraform.verify"
	Name   string // e.g. "verify"
	Source string // "subscription", "subscription-rule", "composition-default", or "legacy-none"
}

// MatchedProfileRule captures which rule was selected for plan traceability.
type MatchedProfileRule struct {
	RuleIndex  int
	Profile    string
	TriggerRef string
}

// ResolveProfileWithRules evaluates profileRules against matched triggers,
// then delegates to ResolveProfileRef with the effective profile.
func ResolveProfileWithRules(
	componentType string,
	composition *Composition,
	subscription *model.EnvironmentSubscription,
	matchedTriggers []string,
) (ResolvedProfile, *MatchedProfileRule, error) {
	if subscription == nil {
		resolved, err := ResolveProfileRef(componentType, composition, subscription)
		return resolved, nil, err
	}

	effectiveProfile, matchedRule := evaluateProfileRules(subscription, matchedTriggers)

	if effectiveProfile != "" {
		overridden := *subscription
		overridden.Profile = effectiveProfile
		resolved, err := ResolveProfileRef(componentType, composition, &overridden)
		if err != nil {
			return ResolvedProfile{}, nil, err
		}
		resolved.Source = "subscription-rule"
		return resolved, matchedRule, nil
	}

	resolved, err := ResolveProfileRef(componentType, composition, subscription)
	return resolved, nil, err
}

func evaluateProfileRules(sub *model.EnvironmentSubscription, matchedTriggers []string) (string, *MatchedProfileRule) {
	if len(sub.ProfileRules) == 0 || len(matchedTriggers) == 0 {
		return "", nil
	}

	triggerSet := make(map[string]struct{}, len(matchedTriggers))
	for _, t := range matchedTriggers {
		triggerSet[t] = struct{}{}
	}

	for i, rule := range sub.ProfileRules {
		if _, ok := triggerSet[rule.When.TriggerRef]; ok {
			return rule.Profile, &MatchedProfileRule{
				RuleIndex:  i,
				Profile:    rule.Profile,
				TriggerRef: rule.When.TriggerRef,
			}
		}
	}
	return "", nil
}

// ResolveProfileRef resolves a profile reference for a component/environment subscription
// against the composition's execution profiles.
func ResolveProfileRef(componentType string, composition *Composition, subscription *model.EnvironmentSubscription) (ResolvedProfile, error) {
	if len(composition.ExecutionProfiles) == 0 {
		return ResolvedProfile{Source: "legacy-none"}, nil
	}

	rawProfile := ""
	source := ""

	if subscription != nil && subscription.Profile != "" {
		rawProfile = subscription.Profile
		source = "subscription"
	} else {
		rawProfile = composition.DefaultProfile
		source = "composition-default"
	}

	if rawProfile == "" {
		return ResolvedProfile{}, fmt.Errorf("composition %s has executionProfiles but no defaultProfile or subscription profile", composition.Name)
	}

	var typePrefix, profileName string
	if strings.Contains(rawProfile, ".") {
		parts := strings.SplitN(rawProfile, ".", 2)
		typePrefix = parts[0]
		profileName = parts[1]
		if typePrefix != componentType {
			return ResolvedProfile{}, fmt.Errorf("component has type %s but references profile %q (type prefix mismatch)", componentType, rawProfile)
		}
	} else {
		profileName = rawProfile
		typePrefix = componentType
	}

	if _, exists := composition.ExecutionProfiles[profileName]; !exists {
		return ResolvedProfile{}, fmt.Errorf("profile %q does not exist in composition %s executionProfiles", profileName, composition.Name)
	}

	return ResolvedProfile{
		Ref:    typePrefix + "." + profileName,
		Name:   profileName,
		Source: source,
	}, nil
}
