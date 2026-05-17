package preset

import (
	"fmt"
	"sort"

	"github.com/sourceplane/orun/internal/model"
)

// MergeResult holds the merged intent and provenance metadata.
type MergeResult struct {
	Intent     *model.Intent
	Provenance map[string][]model.PresetProvenance
}

// MergePresets applies resolved presets to the intent using deterministic
// field-specific merge rules. Presets are applied in declaration order,
// then the original repo intent is overlaid on top (repo always wins for defaults).
func MergePresets(intent *model.Intent, presets []*ResolvedPreset) (*MergeResult, error) {
	if len(presets) == 0 {
		return &MergeResult{Intent: intent, Provenance: make(map[string][]model.PresetProvenance)}, nil
	}

	provenance := make(map[string][]model.PresetProvenance)
	merged := shallowCopyIntent(intent)

	for _, rp := range presets {
		spec := rp.Preset.Spec
		prov := rp.Provenance

		if err := mergeEnv(merged, spec.Env, prov, provenance); err != nil {
			return nil, err
		}
		mergeDiscoveryRoots(merged, spec.Discovery.Roots, prov, provenance)
		mergeGroups(merged, spec.Groups, prov, provenance)
		if err := mergeEnvironments(merged, spec.Environments, prov, provenance); err != nil {
			return nil, err
		}
		mergeTriggerBindings(merged, spec.Automation.TriggerBindings, prov, provenance)
	}

	return &MergeResult{Intent: merged, Provenance: provenance}, nil
}

func shallowCopyIntent(intent *model.Intent) *model.Intent {
	cp := *intent
	cp.Extends = nil

	if intent.Env != nil {
		cp.Env = make(map[string]string, len(intent.Env))
		for k, v := range intent.Env {
			cp.Env[k] = v
		}
	}

	if intent.Discovery.Roots != nil {
		cp.Discovery.Roots = make([]string, len(intent.Discovery.Roots))
		copy(cp.Discovery.Roots, intent.Discovery.Roots)
	}

	if intent.Groups != nil {
		cp.Groups = make(map[string]model.Group, len(intent.Groups))
		for k, v := range intent.Groups {
			cp.Groups[k] = copyGroup(v)
		}
	}

	if intent.Environments != nil {
		cp.Environments = make(map[string]model.Environment, len(intent.Environments))
		for k, v := range intent.Environments {
			cp.Environments[k] = copyEnvironment(v)
		}
	}

	if intent.Automation.TriggerBindings != nil {
		cp.Automation.TriggerBindings = make(map[string]model.TriggerBinding, len(intent.Automation.TriggerBindings))
		for k, v := range intent.Automation.TriggerBindings {
			cp.Automation.TriggerBindings[k] = v
		}
	}

	return &cp
}

func mergeEnv(intent *model.Intent, presetEnv map[string]string, prov model.PresetProvenance, provMap map[string][]model.PresetProvenance) error {
	if len(presetEnv) == 0 {
		return nil
	}
	if intent.Env == nil {
		intent.Env = make(map[string]string)
	}

	keys := sortedKeys(presetEnv)
	for _, k := range keys {
		if _, exists := intent.Env[k]; !exists {
			intent.Env[k] = presetEnv[k]
			provMap["env."+k] = append(provMap["env."+k], prov)
		}
	}
	return nil
}

func mergeDiscoveryRoots(intent *model.Intent, presetRoots []string, prov model.PresetProvenance, provMap map[string][]model.PresetProvenance) {
	if len(presetRoots) == 0 {
		return
	}

	existing := make(map[string]struct{}, len(intent.Discovery.Roots))
	for _, r := range intent.Discovery.Roots {
		existing[r] = struct{}{}
	}

	for _, root := range presetRoots {
		if _, exists := existing[root]; !exists {
			intent.Discovery.Roots = append(intent.Discovery.Roots, root)
			existing[root] = struct{}{}
			provMap["discovery.roots"] = append(provMap["discovery.roots"], prov)
		}
	}
}

func mergeGroups(intent *model.Intent, presetGroups map[string]model.Group, prov model.PresetProvenance, provMap map[string][]model.PresetProvenance) {
	if len(presetGroups) == 0 {
		return
	}
	if intent.Groups == nil {
		intent.Groups = make(map[string]model.Group)
	}

	groupNames := sortedGroupKeys(presetGroups)
	for _, name := range groupNames {
		presetGroup := presetGroups[name]
		existing, exists := intent.Groups[name]
		if !exists {
			intent.Groups[name] = copyGroup(presetGroup)
			provMap["groups."+name] = append(provMap["groups."+name], prov)
			continue
		}

		if existing.ParameterDefaults == nil {
			existing.ParameterDefaults = make(map[string]map[string]interface{})
		}
		for typeName, params := range presetGroup.ParameterDefaults {
			if _, has := existing.ParameterDefaults[typeName]; !has {
				existing.ParameterDefaults[typeName] = make(map[string]interface{}, len(params))
			}
			for k, v := range params {
				if _, has := existing.ParameterDefaults[typeName][k]; !has {
					existing.ParameterDefaults[typeName][k] = v
					provMap[fmt.Sprintf("groups.%s.parameterDefaults.%s.%s", name, typeName, k)] = append(provMap[fmt.Sprintf("groups.%s.parameterDefaults.%s.%s", name, typeName, k)], prov)
				}
			}
		}

		if existing.Policies == nil {
			existing.Policies = make(map[string]interface{})
		}
		for k, v := range presetGroup.Policies {
			existing.Policies[k] = v
			provMap[fmt.Sprintf("groups.%s.policies.%s", name, k)] = append(provMap[fmt.Sprintf("groups.%s.policies.%s", name, k)], prov)
		}

		if existing.Path == "" && presetGroup.Path != "" {
			existing.Path = presetGroup.Path
		}

		intent.Groups[name] = existing
	}
}

func mergeEnvironments(intent *model.Intent, presetEnvs map[string]model.Environment, prov model.PresetProvenance, provMap map[string][]model.PresetProvenance) error {
	if len(presetEnvs) == 0 {
		return nil
	}
	if intent.Environments == nil {
		intent.Environments = make(map[string]model.Environment)
	}

	envNames := sortedEnvKeys(presetEnvs)
	for _, name := range envNames {
		presetEnv := presetEnvs[name]
		existing, exists := intent.Environments[name]
		if !exists {
			intent.Environments[name] = copyEnvironment(presetEnv)
			provMap["environments."+name] = append(provMap["environments."+name], prov)
			continue
		}

		if existing.ParameterDefaults == nil {
			existing.ParameterDefaults = make(map[string]map[string]interface{})
		}
		for typeName, params := range presetEnv.ParameterDefaults {
			if _, has := existing.ParameterDefaults[typeName]; !has {
				existing.ParameterDefaults[typeName] = make(map[string]interface{}, len(params))
			}
			for k, v := range params {
				if _, has := existing.ParameterDefaults[typeName][k]; !has {
					existing.ParameterDefaults[typeName][k] = v
					provMap[fmt.Sprintf("environments.%s.parameterDefaults.%s.%s", name, typeName, k)] = append(provMap[fmt.Sprintf("environments.%s.parameterDefaults.%s.%s", name, typeName, k)], prov)
				}
			}
		}

		if existing.Policies == nil {
			existing.Policies = make(map[string]interface{})
		}
		for k, v := range presetEnv.Policies {
			existing.Policies[k] = v
			provMap[fmt.Sprintf("environments.%s.policies.%s", name, k)] = append(provMap[fmt.Sprintf("environments.%s.policies.%s", name, k)], prov)
		}

		if existing.Env == nil {
			existing.Env = make(map[string]string)
		}
		envKeys := sortedKeys(presetEnv.Env)
		for _, k := range envKeys {
			if _, has := existing.Env[k]; !has {
				existing.Env[k] = presetEnv.Env[k]
			}
		}

		if len(presetEnv.Activation.TriggerRefs) > 0 {
			refSet := make(map[string]struct{}, len(existing.Activation.TriggerRefs))
			for _, r := range existing.Activation.TriggerRefs {
				refSet[r] = struct{}{}
			}
			for _, r := range presetEnv.Activation.TriggerRefs {
				if _, has := refSet[r]; !has {
					existing.Activation.TriggerRefs = append(existing.Activation.TriggerRefs, r)
					provMap[fmt.Sprintf("environments.%s.activation.triggerRefs", name)] = append(provMap[fmt.Sprintf("environments.%s.activation.triggerRefs", name)], prov)
				}
			}
		}

		if existing.Selectors.Components == nil && len(presetEnv.Selectors.Components) > 0 {
			existing.Selectors.Components = make([]string, len(presetEnv.Selectors.Components))
			copy(existing.Selectors.Components, presetEnv.Selectors.Components)
		}
		if existing.Selectors.Domains == nil && len(presetEnv.Selectors.Domains) > 0 {
			existing.Selectors.Domains = make([]string, len(presetEnv.Selectors.Domains))
			copy(existing.Selectors.Domains, presetEnv.Selectors.Domains)
		}

		if existing.Path == "" && presetEnv.Path != "" {
			existing.Path = presetEnv.Path
		}

		intent.Environments[name] = existing
	}
	return nil
}

func mergeTriggerBindings(intent *model.Intent, presetBindings map[string]model.TriggerBinding, prov model.PresetProvenance, provMap map[string][]model.PresetProvenance) {
	if len(presetBindings) == 0 {
		return
	}
	if intent.Automation.TriggerBindings == nil {
		intent.Automation.TriggerBindings = make(map[string]model.TriggerBinding)
	}

	bindingNames := sortedBindingKeys(presetBindings)
	for _, name := range bindingNames {
		if _, exists := intent.Automation.TriggerBindings[name]; !exists {
			intent.Automation.TriggerBindings[name] = presetBindings[name]
			provMap["automation.triggerBindings."+name] = append(provMap["automation.triggerBindings."+name], prov)
		}
	}
}

func copyGroup(g model.Group) model.Group {
	cp := model.Group{Path: g.Path}
	if g.ParameterDefaults != nil {
		cp.ParameterDefaults = make(map[string]map[string]interface{}, len(g.ParameterDefaults))
		for typeName, params := range g.ParameterDefaults {
			cp.ParameterDefaults[typeName] = make(map[string]interface{}, len(params))
			for k, v := range params {
				cp.ParameterDefaults[typeName][k] = v
			}
		}
	}
	if g.Policies != nil {
		cp.Policies = make(map[string]interface{}, len(g.Policies))
		for k, v := range g.Policies {
			cp.Policies[k] = v
		}
	}
	return cp
}

func copyEnvironment(e model.Environment) model.Environment {
	cp := model.Environment{
		Path:       e.Path,
		Activation: e.Activation,
		Promotion:  e.Promotion,
		Selectors:  e.Selectors,
	}
	if e.ParameterDefaults != nil {
		cp.ParameterDefaults = make(map[string]map[string]interface{}, len(e.ParameterDefaults))
		for typeName, params := range e.ParameterDefaults {
			cp.ParameterDefaults[typeName] = make(map[string]interface{}, len(params))
			for k, v := range params {
				cp.ParameterDefaults[typeName][k] = v
			}
		}
	}
	if e.Policies != nil {
		cp.Policies = make(map[string]interface{}, len(e.Policies))
		for k, v := range e.Policies {
			cp.Policies[k] = v
		}
	}
	if e.Env != nil {
		cp.Env = make(map[string]string, len(e.Env))
		for k, v := range e.Env {
			cp.Env[k] = v
		}
	}
	if e.Activation.TriggerRefs != nil {
		cp.Activation.TriggerRefs = make([]string, len(e.Activation.TriggerRefs))
		copy(cp.Activation.TriggerRefs, e.Activation.TriggerRefs)
	}
	if e.Selectors.Components != nil {
		cp.Selectors.Components = make([]string, len(e.Selectors.Components))
		copy(cp.Selectors.Components, e.Selectors.Components)
	}
	if e.Selectors.Domains != nil {
		cp.Selectors.Domains = make([]string, len(e.Selectors.Domains))
		copy(cp.Selectors.Domains, e.Selectors.Domains)
	}
	return cp
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedGroupKeys(m map[string]model.Group) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedEnvKeys(m map[string]model.Environment) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sortedBindingKeys(m map[string]model.TriggerBinding) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
