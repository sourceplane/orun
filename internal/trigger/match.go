package trigger

import (
	"strings"

	"github.com/sourceplane/orun/internal/model"
)

// MatchResult holds the result of matching a CI event against automation triggers.
type MatchResult struct {
	Trigger *model.AutomationTrigger
	Profile string
}

// Match finds the first trigger that matches the given event context.
// Returns nil if no trigger matches. First match wins.
func Match(triggers []model.AutomationTrigger, event *model.EventContext) *MatchResult {
	if event == nil {
		return nil
	}
	for i := range triggers {
		t := &triggers[i]
		if matchesTrigger(t, event) {
			return &MatchResult{
				Trigger: t,
				Profile: t.Plan.Profile,
			}
		}
	}
	return nil
}

// MatchByName finds a trigger by its declared name.
// Returns nil if no trigger with that name exists.
func MatchByName(triggers []model.AutomationTrigger, name string) *MatchResult {
	for i := range triggers {
		t := &triggers[i]
		if t.Name == name {
			return &MatchResult{
				Trigger: t,
				Profile: t.Plan.Profile,
			}
		}
	}
	return nil
}

func matchesTrigger(t *model.AutomationTrigger, event *model.EventContext) bool {
	if t.On.Provider != "" && !strings.EqualFold(t.On.Provider, event.Provider) {
		return false
	}

	if !strings.EqualFold(t.On.Event, event.Event) {
		return false
	}

	if len(t.On.Actions) > 0 && !matchesAny(t.On.Actions, event.Action) {
		return false
	}

	if len(t.On.Branches) > 0 && !matchesAnyPattern(t.On.Branches, event.Branch) {
		return false
	}

	if len(t.On.Tags) > 0 && !matchesAnyTagPattern(t.On.Tags, event.Ref) {
		return false
	}

	return true
}

func matchesAny(patterns []string, value string) bool {
	for _, p := range patterns {
		if strings.EqualFold(p, value) {
			return true
		}
	}
	return false
}

func matchesAnyPattern(patterns []string, value string) bool {
	for _, p := range patterns {
		if matchesGlob(p, value) {
			return true
		}
	}
	return false
}

func matchesAnyTagPattern(patterns []string, ref string) bool {
	tag := strings.TrimPrefix(ref, "refs/tags/")
	for _, p := range patterns {
		if matchesGlob(p, tag) {
			return true
		}
	}
	return false
}

func matchesGlob(pattern, value string) bool {
	if pattern == "*" {
		return true
	}
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(value, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == value
}
