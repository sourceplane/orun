package git

import (
	"fmt"
	"os/exec"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// IntentDiffMode classifies the kind of change detected in an intent file.
type IntentDiffMode string

const (
	IntentDiffNone       IntentDiffMode = "none"
	IntentDiffGlobal     IntentDiffMode = "global"
	IntentDiffComponents IntentDiffMode = "components"
)

// IntentDiffResult holds the outcome of a semantic intent comparison.
type IntentDiffResult struct {
	Mode            IntentDiffMode
	ChangedSections []string
	Added           []string
	Modified        []string
	Removed         []string
	Reason          string
}

// DiffIntent compares two YAML-encoded intent documents and determines whether
// the change is scoped to inline components only or affects the repo globally.
func DiffIntent(baseYAML, headYAML []byte) IntentDiffResult {
	var baseDoc, headDoc map[string]interface{}
	if err := yaml.Unmarshal(baseYAML, &baseDoc); err != nil {
		return IntentDiffResult{
			Mode:   IntentDiffGlobal,
			Reason: fmt.Sprintf("failed to parse base intent: %v", err),
		}
	}
	if err := yaml.Unmarshal(headYAML, &headDoc); err != nil {
		return IntentDiffResult{
			Mode:   IntentDiffGlobal,
			Reason: fmt.Sprintf("failed to parse head intent: %v", err),
		}
	}

	changedSections := findChangedSections(baseDoc, headDoc)

	if len(changedSections) > 0 {
		return IntentDiffResult{
			Mode:            IntentDiffGlobal,
			ChangedSections: changedSections,
			Reason:          "intent changed outside top-level components",
		}
	}

	baseComps := extractComponentMap(baseDoc)
	headComps := extractComponentMap(headDoc)

	if reflect.DeepEqual(baseComps, headComps) {
		return IntentDiffResult{
			Mode:   IntentDiffNone,
			Reason: "no semantic change detected (formatting/comments only)",
		}
	}

	var added, modified, removed []string
	for name := range headComps {
		if _, exists := baseComps[name]; !exists {
			added = append(added, name)
		}
	}
	for name := range baseComps {
		if _, exists := headComps[name]; !exists {
			removed = append(removed, name)
		}
	}
	for name, headComp := range headComps {
		if baseComp, exists := baseComps[name]; exists {
			if !reflect.DeepEqual(baseComp, headComp) {
				modified = append(modified, name)
			}
		}
	}

	sort.Strings(added)
	sort.Strings(modified)
	sort.Strings(removed)

	if len(added) == 0 && len(modified) == 0 && len(removed) == 0 {
		return IntentDiffResult{
			Mode:   IntentDiffNone,
			Reason: "components list reordered without content change",
		}
	}

	return IntentDiffResult{
		Mode:     IntentDiffComponents,
		Added:    added,
		Modified: modified,
		Removed:  removed,
		Reason:   "only inline components changed",
	}
}

// GetFileAtRef returns file contents at a given git ref.
func GetFileAtRef(ref, path string) ([]byte, error) {
	target := ref + ":" + path
	cmd := exec.Command("git", "show", target)
	output, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("git show %s: %w", target, err)
	}
	return output, nil
}

func withoutKey(doc map[string]interface{}, key string) map[string]interface{} {
	result := make(map[string]interface{}, len(doc))
	for k, v := range doc {
		if k != key {
			result[k] = v
		}
	}
	return result
}

func extractComponentMap(doc map[string]interface{}) map[string]map[string]interface{} {
	result := make(map[string]map[string]interface{})
	raw, ok := doc["components"]
	if !ok {
		return result
	}
	list, ok := raw.([]interface{})
	if !ok {
		return result
	}
	for _, item := range list {
		comp, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		name := componentName(comp)
		if name == "" {
			continue
		}
		result[name] = comp
	}
	return result
}

func componentName(comp map[string]interface{}) string {
	if name, ok := comp["name"]; ok {
		if s, ok := name.(string); ok && strings.TrimSpace(s) != "" {
			return s
		}
	}
	if meta, ok := comp["metadata"]; ok {
		if m, ok := meta.(map[string]interface{}); ok {
			if name, ok := m["name"]; ok {
				if s, ok := name.(string); ok {
					return s
				}
			}
		}
	}
	return ""
}

var ignoredIntentKeys = map[string]bool{
	"apiVersion": true,
	"kind":       true,
	"metadata":   true,
	"components": true,
}

func findChangedSections(baseDoc, headDoc map[string]interface{}) []string {
	allKeys := make(map[string]bool)
	for k := range baseDoc {
		allKeys[k] = true
	}
	for k := range headDoc {
		allKeys[k] = true
	}

	var changed []string
	for k := range allKeys {
		if ignoredIntentKeys[k] {
			continue
		}
		baseVal, baseHas := baseDoc[k]
		headVal, headHas := headDoc[k]
		if baseHas != headHas || !reflect.DeepEqual(baseVal, headVal) {
			changed = append(changed, k)
		}
	}
	sort.Strings(changed)
	return changed
}
