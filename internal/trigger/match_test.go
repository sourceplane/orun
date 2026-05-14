package trigger

import (
	"testing"

	"github.com/sourceplane/orun/internal/model"
)

func TestMatchTrigger_PullRequest(t *testing.T) {
	binding := model.TriggerBinding{
		On: model.TriggerMatch{
			Provider:     "github",
			Event:        "pull_request",
			Actions:      []string{"opened", "synchronize"},
			BaseBranches: []string{"main"},
		},
	}

	tests := []struct {
		name     string
		event    *model.NormalizedEvent
		expected bool
	}{
		{
			name: "matches opened on main",
			event: &model.NormalizedEvent{
				Provider:   "github",
				Event:      "pull_request",
				Action:     "opened",
				BaseBranch: "main",
			},
			expected: true,
		},
		{
			name: "matches synchronize on main",
			event: &model.NormalizedEvent{
				Provider:   "github",
				Event:      "pull_request",
				Action:     "synchronize",
				BaseBranch: "main",
			},
			expected: true,
		},
		{
			name: "rejects closed action",
			event: &model.NormalizedEvent{
				Provider:   "github",
				Event:      "pull_request",
				Action:     "closed",
				BaseBranch: "main",
			},
			expected: false,
		},
		{
			name: "rejects wrong base branch",
			event: &model.NormalizedEvent{
				Provider:   "github",
				Event:      "pull_request",
				Action:     "opened",
				BaseBranch: "develop",
			},
			expected: false,
		},
		{
			name: "rejects wrong provider",
			event: &model.NormalizedEvent{
				Provider:   "gitlab",
				Event:      "pull_request",
				Action:     "opened",
				BaseBranch: "main",
			},
			expected: false,
		},
		{
			name: "rejects wrong event",
			event: &model.NormalizedEvent{
				Provider:   "github",
				Event:      "push",
				Action:     "opened",
				BaseBranch: "main",
			},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MatchTrigger(binding, tc.event)
			if got != tc.expected {
				t.Errorf("MatchTrigger() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestMatchTrigger_PushBranch(t *testing.T) {
	binding := model.TriggerBinding{
		On: model.TriggerMatch{
			Provider: "github",
			Event:    "push",
			Branches: []string{"main"},
		},
	}

	tests := []struct {
		name     string
		event    *model.NormalizedEvent
		expected bool
	}{
		{
			name:     "matches push to main",
			event:    &model.NormalizedEvent{Provider: "github", Event: "push", Branch: "main"},
			expected: true,
		},
		{
			name:     "rejects push to develop",
			event:    &model.NormalizedEvent{Provider: "github", Event: "push", Branch: "develop"},
			expected: false,
		},
		{
			name:     "rejects empty branch",
			event:    &model.NormalizedEvent{Provider: "github", Event: "push", Branch: ""},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MatchTrigger(binding, tc.event)
			if got != tc.expected {
				t.Errorf("MatchTrigger() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestMatchTrigger_PushTag(t *testing.T) {
	binding := model.TriggerBinding{
		On: model.TriggerMatch{
			Provider: "github",
			Event:    "push",
			Tags:     []string{"v*"},
		},
	}

	tests := []struct {
		name     string
		event    *model.NormalizedEvent
		expected bool
	}{
		{
			name:     "matches v1.2.0",
			event:    &model.NormalizedEvent{Provider: "github", Event: "push", Tag: "v1.2.0"},
			expected: true,
		},
		{
			name:     "matches v0.1.0-rc1",
			event:    &model.NormalizedEvent{Provider: "github", Event: "push", Tag: "v0.1.0-rc1"},
			expected: true,
		},
		{
			name:     "rejects non-v tag",
			event:    &model.NormalizedEvent{Provider: "github", Event: "push", Tag: "release-1.0"},
			expected: false,
		},
		{
			name:     "rejects empty tag",
			event:    &model.NormalizedEvent{Provider: "github", Event: "push", Tag: ""},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MatchTrigger(binding, tc.event)
			if got != tc.expected {
				t.Errorf("MatchTrigger() = %v, want %v", got, tc.expected)
			}
		})
	}
}

func TestMatchAnyPattern(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		patterns []string
		expected bool
	}{
		{"exact match", "main", []string{"main"}, true},
		{"prefix wildcard", "v1.2.0", []string{"v*"}, true},
		{"path wildcard", "release/1.0", []string{"release/*"}, true},
		{"star matches any", "anything", []string{"*"}, true},
		{"no match", "develop", []string{"main", "release/*"}, false},
		{"empty value", "", []string{"*"}, false},
		{"empty patterns", "main", []string{}, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := MatchAnyPattern(tc.value, tc.patterns)
			if got != tc.expected {
				t.Errorf("MatchAnyPattern(%q, %v) = %v, want %v", tc.value, tc.patterns, got, tc.expected)
			}
		})
	}
}

func TestMatchTrigger_NoFilters(t *testing.T) {
	binding := model.TriggerBinding{
		On: model.TriggerMatch{
			Provider: "github",
			Event:    "push",
		},
	}

	event := &model.NormalizedEvent{Provider: "github", Event: "push", Branch: "any-branch"}
	if !MatchTrigger(binding, event) {
		t.Error("expected match when no filters are set")
	}
}
