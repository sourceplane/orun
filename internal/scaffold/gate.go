package scaffold

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
	yaml "gopkg.in/yaml.v3"

	"github.com/sourceplane/orun/internal/catalogmodel"
	"github.com/sourceplane/orun/internal/model"
)

// componentKind is the kind every generated component.yaml must declare.
const componentKind = "Component"

// gateComponentYAML runs the component-depth output gate (design §10) on one
// generated component.yaml: it MUST (1) pass the permissive plan-engine parser
// (model.ComponentManifest) and (2) pass the strict catalog parser
// (catalogmodel.ComponentYAML via the embedded draft-07 schema). Running BOTH
// on every generated manifest is the parser-parity discipline (S-4): a scaffold
// that only satisfies one parser is a failure, not a warning. Fail closed.
func gateComponentYAML(rel string, data []byte) error {
	if err := parsePermissive(rel, data); err != nil {
		return err
	}
	if err := parseStrict(rel, data); err != nil {
		return err
	}
	return nil
}

// parsePermissive runs the plan-engine parser (tolerant of unknown fields),
// mirroring internal/loader's inline yaml.Unmarshal + kind/name checks.
func parsePermissive(rel string, data []byte) error {
	var manifest model.ComponentManifest
	if err := yaml.Unmarshal(data, &manifest); err != nil {
		return gateErr("gate: %s failed the plan-engine parser: %v", rel, err)
	}
	if manifest.Kind != componentKind {
		return gateErr("gate: %s must have kind %q (plan-engine parser), got %q", rel, componentKind, manifest.Kind)
	}
	if manifest.Metadata.Name == "" {
		return gateErr("gate: %s must set metadata.name (plan-engine parser)", rel)
	}
	return nil
}

// parseStrict validates against the strict catalog schema and decodes into the
// typed shape — the same path catalogresolve.loadAuthored takes, replicated
// here because that function is unexported.
func parseStrict(rel string, data []byte) error {
	schema, err := strictSchema()
	if err != nil {
		return gateErr("gate: %s strict schema unavailable: %v", rel, err)
	}
	var generic any
	if err := yaml.Unmarshal(data, &generic); err != nil {
		return gateErr("gate: %s strict parser yaml decode: %v", rel, err)
	}
	jsonBytes, err := json.Marshal(generic)
	if err != nil {
		return gateErr("gate: %s strict parser yaml→json: %v", rel, err)
	}
	var validatable any
	if err := json.Unmarshal(jsonBytes, &validatable); err != nil {
		return gateErr("gate: %s strict parser json decode: %v", rel, err)
	}
	if err := schema.Validate(validatable); err != nil {
		return gateErr("gate: %s failed the strict catalog parser: %v", rel, err)
	}
	var typed catalogmodel.ComponentYAML
	if err := json.Unmarshal(jsonBytes, &typed); err != nil {
		return gateErr("gate: %s strict parser typed decode: %v", rel, err)
	}
	return nil
}

var (
	strictSchemaOnce sync.Once
	strictSchemaVal  *jsonschema.Schema
	strictSchemaErr  error
)

func strictSchema() (*jsonschema.Schema, error) {
	strictSchemaOnce.Do(func() {
		c := jsonschema.NewCompiler()
		c.Draft = jsonschema.Draft7
		const url = "orun://scaffold/component.yaml.schema.json"
		if err := c.AddResource(url, strings.NewReader(string(catalogmodel.ComponentYAMLSchema))); err != nil {
			strictSchemaErr = fmt.Errorf("add embedded schema: %w", err)
			return
		}
		s, err := c.Compile(url)
		if err != nil {
			strictSchemaErr = fmt.Errorf("compile embedded schema: %w", err)
			return
		}
		strictSchemaVal = s
	})
	return strictSchemaVal, strictSchemaErr
}

// isComponentYAML reports whether a placed path is a component manifest the
// component-depth gate must check.
func isComponentYAML(rel string) bool {
	base := rel
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		base = rel[i+1:]
	}
	return base == "component.yaml" || base == "component.yml"
}
