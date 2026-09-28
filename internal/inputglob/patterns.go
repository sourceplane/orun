package inputglob

import (
	"bytes"
	"encoding/json"
	"fmt"

	"gopkg.in/yaml.v3"
)

// Patterns is the authored `spec.inputs` list, shared by both component.yaml
// parsers (internal/model and internal/catalogmodel) so they decode it
// identically.
//
// Before input globs existed, some component.yaml files carried a legacy
// `spec.inputs` MAPPING — the plan engine's old name for `parameters`, long
// ignored. That form still decodes, to an empty list, so those files keep
// loading unchanged; only the list form declares globs. Any other shape is an
// error.
type Patterns []string

// UnmarshalYAML accepts a sequence of strings (the globs), a mapping (the
// legacy, ignored form) or null.
func (p *Patterns) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.SequenceNode:
		var out []string
		if err := node.Decode(&out); err != nil {
			return fmt.Errorf("spec.inputs: %w", err)
		}
		*p = out
		return nil
	case yaml.MappingNode:
		*p = nil // legacy spec.inputs parameters block: ignored
		return nil
	case yaml.ScalarNode:
		if node.Tag == "!!null" {
			*p = nil
			return nil
		}
	}
	return fmt.Errorf("spec.inputs: line %d: expected a list of glob patterns", node.Line)
}

// UnmarshalJSON mirrors UnmarshalYAML for the catalog's YAML→JSON decode.
func (p *Patterns) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	switch {
	case len(trimmed) == 0 || bytes.Equal(trimmed, []byte("null")):
		*p = nil
		return nil
	case trimmed[0] == '{':
		*p = nil // legacy spec.inputs parameters block: ignored
		return nil
	case trimmed[0] == '[':
		var out []string
		if err := json.Unmarshal(trimmed, &out); err != nil {
			return fmt.Errorf("spec.inputs: %w", err)
		}
		*p = out
		return nil
	}
	return fmt.Errorf("spec.inputs: expected a list of glob patterns")
}

// JSONSchemaOverride is the component.yaml JSON Schema fragment: a list of
// strings, or (legacy, ignored) an object. Consumed by
// internal/catalogmodel/schema/gen.
func (Patterns) JSONSchemaOverride() map[string]any {
	return map[string]any{
		"oneOf": []any{
			map[string]any{
				"type":        "array",
				"items":       map[string]any{"type": "string"},
				"description": "Repository-root-relative glob patterns (doublestar, ** crosses directories) whose changes select this component under --changed.",
			},
			map[string]any{
				"type":        "object",
				"description": "Legacy spec.inputs parameters block; accepted and ignored.",
			},
		},
	}
}
