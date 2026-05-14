package trigger

import (
	"strings"

	"github.com/sourceplane/orun/internal/model"
)

// MatchTrigger returns true if the normalized event matches the trigger binding criteria.
func MatchTrigger(binding model.TriggerBinding, event *model.NormalizedEvent) bool {
	on := binding.On

	if on.Provider != event.Provider {
		return false
	}
	if on.Event != event.Event {
		return false
	}
	if len(on.Actions) > 0 && !containsString(on.Actions, event.Action) {
		return false
	}
	if len(on.Branches) > 0 {
		if event.Branch == "" || !MatchAnyPattern(event.Branch, on.Branches) {
			return false
		}
	}
	if len(on.BaseBranches) > 0 {
		if event.BaseBranch == "" || !MatchAnyPattern(event.BaseBranch, on.BaseBranches) {
			return false
		}
	}
	if len(on.Tags) > 0 {
		if event.Tag == "" || !MatchAnyPattern(event.Tag, on.Tags) {
			return false
		}
	}
	return true
}

// MatchAnyPattern checks if a value matches any of the provided glob patterns.
func MatchAnyPattern(value string, patterns []string) bool {
	if value == "" {
		return false
	}
	for _, pattern := range patterns {
		if matchGlob(pattern, value) {
			return true
		}
	}
	return false
}

// matchGlob supports simple glob patterns: *, prefix*, */suffix, exact match.
func matchGlob(pattern, value string) bool {
	if pattern == "*" {
		return value != ""
	}

	// Handle patterns like release/* (single segment wildcard)
	if strings.Contains(pattern, "*") {
		parts := strings.SplitN(pattern, "*", 2)
		prefix := parts[0]
		suffix := parts[1]
		if !strings.HasPrefix(value, prefix) {
			return false
		}
		if suffix != "" && !strings.HasSuffix(value, suffix) {
			return false
		}
		return true
	}

	return pattern == value
}

func containsString(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
