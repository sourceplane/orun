package preset

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/sourceplane/orun/internal/model"
	"gopkg.in/yaml.v3"
)

// ResolvedPreset pairs a parsed IntentPreset with its provenance.
type ResolvedPreset struct {
	Preset     model.IntentPreset
	Provenance model.PresetProvenance
}

// LoadPresetsForIntent resolves each extends ref against the already-resolved
// composition source directories and returns parsed presets in declaration order.
func LoadPresetsForIntent(intent *model.Intent, sourceRoots map[string]string) ([]*ResolvedPreset, error) {
	if len(intent.Extends) == 0 {
		return nil, nil
	}

	results := make([]*ResolvedPreset, 0, len(intent.Extends))

	for _, ref := range intent.Extends {
		root, exists := sourceRoots[ref.Source]
		if !exists {
			available := make([]string, 0, len(sourceRoots))
			for name := range sourceRoots {
				available = append(available, name)
			}
			return nil, fmt.Errorf("extends references unknown source %q; declared sources: %v", ref.Source, available)
		}

		stack, err := loadStackFromRoot(root)
		if err != nil {
			return nil, fmt.Errorf("failed to load stack.yaml from source %q: %w", ref.Source, err)
		}

		presetEntry := findPresetEntry(stack, ref.Preset)
		if presetEntry == nil {
			available := make([]string, 0, len(stack.Spec.IntentPresets))
			for _, p := range stack.Spec.IntentPresets {
				available = append(available, p.Name)
			}
			return nil, fmt.Errorf("source %q does not publish preset %q; available: %v", ref.Source, ref.Preset, available)
		}

		presetPath := filepath.Join(root, presetEntry.Path)
		preset, err := loadPresetFile(presetPath)
		if err != nil {
			return nil, fmt.Errorf("failed to load preset %q from source %q: %w", ref.Preset, ref.Source, err)
		}

		if preset.Kind != "IntentPreset" {
			return nil, fmt.Errorf("preset file %s has kind %q, expected IntentPreset", presetEntry.Path, preset.Kind)
		}

		results = append(results, &ResolvedPreset{
			Preset: *preset,
			Provenance: model.PresetProvenance{
				Source: ref.Source,
				Preset: ref.Preset,
			},
		})
	}

	return results, nil
}

func loadStackFromRoot(root string) (*model.Stack, error) {
	path := filepath.Join(root, "stack.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", path, err)
	}

	var stack model.Stack
	if err := yaml.Unmarshal(data, &stack); err != nil {
		return nil, fmt.Errorf("failed to parse stack.yaml: %w", err)
	}

	return &stack, nil
}

func findPresetEntry(stack *model.Stack, name string) *model.StackIntentPreset {
	for i := range stack.Spec.IntentPresets {
		if stack.Spec.IntentPresets[i].Name == name {
			return &stack.Spec.IntentPresets[i]
		}
	}
	return nil
}

func loadPresetFile(path string) (*model.IntentPreset, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read preset file: %w", err)
	}

	var preset model.IntentPreset
	if err := yaml.Unmarshal(data, &preset); err != nil {
		return nil, fmt.Errorf("failed to parse preset YAML: %w", err)
	}

	return &preset, nil
}
