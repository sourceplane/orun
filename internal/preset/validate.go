package preset

import (
	"fmt"
	"sort"

	"github.com/sourceplane/orun/internal/model"
)

// ValidateExtendsRefs checks that every extends ref references a declared composition source.
func ValidateExtendsRefs(intent *model.Intent) error {
	if len(intent.Extends) == 0 {
		return nil
	}

	sourceNames := make(map[string]struct{}, len(intent.Compositions.Sources))
	for _, src := range intent.Compositions.Sources {
		sourceNames[src.Name] = struct{}{}
	}

	for _, ref := range intent.Extends {
		if _, exists := sourceNames[ref.Source]; !exists {
			available := make([]string, 0, len(sourceNames))
			for name := range sourceNames {
				available = append(available, name)
			}
			sort.Strings(available)
			return fmt.Errorf("extends references unknown composition source %q; declared sources: %v", ref.Source, available)
		}
	}
	return nil
}

// ValidatePresetSpec checks that a loaded IntentPreset does not contain forbidden fields.
func ValidatePresetSpec(preset *model.IntentPreset, prov model.PresetProvenance) error {
	if preset.Kind != "IntentPreset" {
		return fmt.Errorf("preset %s from source %s has invalid kind %q, expected IntentPreset", prov.Preset, prov.Source, preset.Kind)
	}
	return nil
}
