package trigger

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/model"
)

// lookupPath walks a dot-separated path through the raw event payload and
// returns the value at its end. ok is false when a segment is missing or an
// intermediate value is not an object.
func lookupPath(raw map[string]any, path string) (any, bool) {
	if raw == nil || path == "" {
		return nil, false
	}
	current := any(raw)
	for _, part := range strings.Split(path, ".") {
		m, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}
		val, exists := m[part]
		if !exists {
			return nil, false
		}
		current = val
	}
	return current, true
}

// ResolveList resolves a dot path to a list of names: a comma-separated
// string (items trimmed, empty items dropped) or a JSON list of strings. A
// missing path, null, a value of another type, or an empty result is an
// error naming the path — a selected plan never falls back to "everything".
func ResolveList(raw map[string]any, path string) ([]string, error) {
	val, ok := lookupPath(raw, path)
	if !ok || val == nil {
		return nil, fmt.Errorf("event has no value at %q", path)
	}
	var out []string
	switch v := val.(type) {
	case string:
		for _, item := range strings.Split(v, ",") {
			if item = strings.TrimSpace(item); item != "" {
				out = append(out, item)
			}
		}
	case []any:
		for i, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("event value at %q: item %d is not a string", path, i)
			}
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	default:
		return nil, fmt.Errorf("event value at %q must be a string or a list of strings", path)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("event value at %q is empty", path)
	}
	return dedupe(out), nil
}

func dedupe(items []string) []string {
	seen := make(map[string]struct{}, len(items))
	out := make([]string, 0, len(items))
	for _, s := range items {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

// selectionPaths returns the components and environments paths of the first
// matched binding (by name) that declares them.
func selectionPaths(intent *model.Intent, matchedNames []string) (binding, components, environments string) {
	names := append([]string(nil), matchedNames...)
	sort.Strings(names)
	for _, name := range names {
		b := intent.Automation.TriggerBindings[name]
		if b.Plan.Components != "" || b.Plan.Environments != "" {
			return name, b.Plan.Components, b.Plan.Environments
		}
	}
	return "", "", ""
}

// resolveSelection reads a selected plan's components and environments from
// the event, and checks every environment is one the matched bindings
// activate.
func resolveSelection(intent *model.Intent, matchedNames []string, event *model.NormalizedEvent, activated []string) ([]string, []string, error) {
	name, compPath, envPath := selectionPaths(intent, matchedNames)
	if name == "" {
		return nil, nil, fmt.Errorf("trigger binding %q plans a selected scope but sets no plan.components / plan.environments", strings.Join(matchedNames, ", "))
	}
	components, err := ResolveList(event.Raw, compPath)
	if err != nil {
		return nil, nil, fmt.Errorf("trigger binding %q: plan.components: %w", name, err)
	}
	envs, err := ResolveList(event.Raw, envPath)
	if err != nil {
		return nil, nil, fmt.Errorf("trigger binding %q: plan.environments: %w", name, err)
	}
	active := make(map[string]struct{}, len(activated))
	for _, e := range activated {
		active[e] = struct{}{}
	}
	for _, e := range envs {
		if _, ok := active[e]; !ok {
			return nil, nil, fmt.Errorf("environment %s is not activated by trigger %s", e, name)
		}
	}
	return components, envs, nil
}

// autoPushDisabled reports whether any matched binding sets plan.autopush: false.
func autoPushDisabled(intent *model.Intent, matchedNames []string) bool {
	for _, name := range matchedNames {
		if ap := intent.Automation.TriggerBindings[name].Plan.AutoPush; ap != nil && !*ap {
			return true
		}
	}
	return false
}

// requireRuleTriggers returns the matched bindings that set plan.requireRule,
// by name.
func requireRuleTriggers(intent *model.Intent, matchedNames []string) []string {
	var out []string
	for _, name := range matchedNames {
		if intent.Automation.TriggerBindings[name].Plan.RequireRule {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}
