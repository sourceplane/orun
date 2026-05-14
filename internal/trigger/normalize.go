package trigger

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sourceplane/orun/internal/model"
)

// NormalizeGitHubEvent converts a raw GitHub event payload into a NormalizedEvent.
func NormalizeGitHubEvent(eventName string, raw map[string]any) *model.NormalizedEvent {
	ev := &model.NormalizedEvent{
		Provider: "github",
		Event:    eventName,
		Raw:      raw,
	}

	switch eventName {
	case "pull_request", "pull_request_target":
		ev.Action = getString(raw, "action")
		ev.RefType = "pull_request"
		if pr, ok := raw["pull_request"].(map[string]any); ok {
			if base, ok := pr["base"].(map[string]any); ok {
				ev.BaseBranch = getString(base, "ref")
				ev.BaseSHA = getString(base, "sha")
			}
			if head, ok := pr["head"].(map[string]any); ok {
				ev.Branch = getString(head, "ref")
				ev.HeadSHA = getString(head, "sha")
			}
		}
		ev.Repository = getNestedString(raw, "repository", "full_name")
		ev.Actor = getNestedString(raw, "sender", "login")

	case "push":
		ev.Ref = getString(raw, "ref")
		ev.BaseSHA = getString(raw, "before")
		ev.HeadSHA = getString(raw, "after")
		ev.Repository = getNestedString(raw, "repository", "full_name")
		ev.Actor = getNestedString(raw, "sender", "login")

		switch {
		case strings.HasPrefix(ev.Ref, "refs/tags/"):
			ev.RefType = "tag"
			ev.Tag = strings.TrimPrefix(ev.Ref, "refs/tags/")
		case strings.HasPrefix(ev.Ref, "refs/heads/"):
			ev.RefType = "branch"
			ev.Branch = strings.TrimPrefix(ev.Ref, "refs/heads/")
		default:
			ev.RefType = "unknown"
		}

	case "workflow_dispatch":
		ev.Ref = getString(raw, "ref")
		if strings.HasPrefix(ev.Ref, "refs/heads/") {
			ev.RefType = "branch"
			ev.Branch = strings.TrimPrefix(ev.Ref, "refs/heads/")
		}
		ev.Repository = getNestedString(raw, "repository", "full_name")
		ev.Actor = getNestedString(raw, "sender", "login")

	default:
		ev.RefType = "unknown"
	}

	return ev
}

// ParseEventFile reads and normalizes a provider event file.
func ParseEventFile(provider string, eventData []byte) (*model.NormalizedEvent, error) {
	switch provider {
	case "github":
		var raw map[string]any
		if err := json.Unmarshal(eventData, &raw); err != nil {
			return nil, fmt.Errorf("failed to parse event file as JSON: %w", err)
		}
		eventName := inferGitHubEventName(raw)
		return NormalizeGitHubEvent(eventName, raw), nil
	default:
		return nil, fmt.Errorf("unsupported event provider: %s", provider)
	}
}

// ParseEventFileWithName reads and normalizes a provider event file when the event name is known.
func ParseEventFileWithName(provider, eventName string, eventData []byte) (*model.NormalizedEvent, error) {
	switch provider {
	case "github":
		var raw map[string]any
		if err := json.Unmarshal(eventData, &raw); err != nil {
			return nil, fmt.Errorf("failed to parse event file as JSON: %w", err)
		}
		return NormalizeGitHubEvent(eventName, raw), nil
	default:
		return nil, fmt.Errorf("unsupported event provider: %s", provider)
	}
}

func inferGitHubEventName(raw map[string]any) string {
	if _, ok := raw["pull_request"]; ok {
		return "pull_request"
	}
	if _, hasBefore := raw["before"]; hasBefore {
		if _, hasAfter := raw["after"]; hasAfter {
			return "push"
		}
	}
	if _, ok := raw["workflow"]; ok {
		return "workflow_dispatch"
	}
	return "unknown"
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func getNestedString(m map[string]any, keys ...string) string {
	current := m
	for i, key := range keys {
		if i == len(keys)-1 {
			return getString(current, key)
		}
		if nested, ok := current[key].(map[string]any); ok {
			current = nested
		} else {
			return ""
		}
	}
	return ""
}
