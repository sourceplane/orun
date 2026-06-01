// Command genschema produces internal/catalogmodel/schema/component-yaml.schema.json
// from the catalogmodel.ComponentYAML Go type via reflection.
//
// Invoked by `go generate ./internal/catalogmodel/...`. Output is sorted,
// 2-space indented, terminated by a trailing newline so `git diff --exit-code`
// is the verification gate (`make verify-generated`).
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

const (
	schemaID          = "https://orun.io/schemas/component-yaml.schema.json"
	schemaTitle       = "ComponentYAML"
	schemaDescription = "Authored component.yaml shape per specs/orun-component-catalog/data-model.md §6."
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: genschema <output-path>")
		os.Exit(2)
	}
	out := os.Args[1]

	root := schemaForType(reflect.TypeOf(catalogmodel.ComponentYAML{}))
	root["$schema"] = "http://json-schema.org/draft-07/schema#"
	root["$id"] = schemaID
	root["title"] = schemaTitle
	root["description"] = schemaDescription

	buf, err := marshalSorted(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "genschema: marshal: %v\n", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "genschema: mkdir: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(out, append(buf, '\n'), 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "genschema: write: %v\n", err)
		os.Exit(1)
	}
}

// schemaOverrider lets a type supply its own JSON Schema fragment instead of
// the reflection-derived one. Used for polymorphic authoring shapes (e.g. a
// subscribe environment that may be a bare string or an object) that a plain
// struct cannot express.
type schemaOverrider interface {
	JSONSchemaOverride() map[string]any
}

// openSchema marks a struct that should accept undeclared properties
// (additionalProperties: true). The authored component.yaml mirrors the plan
// engine, which silently ignores unknown keys (yaml.Unmarshal drops them); an
// open schema keeps the catalog from rejecting legacy or plan-engine-only
// fields it does not interpret, while still type-checking the declared ones.
type openSchema interface {
	OpenSchema() bool
}

func schemaForType(t reflect.Type) map[string]any {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	// A value or pointer of t may carry a hand-written schema override.
	if ov, ok := reflect.New(t).Interface().(schemaOverrider); ok {
		return ov.JSONSchemaOverride()
	}
	switch t.Kind() {
	case reflect.Interface:
		// `any` / interface{} accepts any JSON value. An empty schema is
		// the draft-07 "anything goes" form — used for passthrough fields
		// like spec.parameters whose values are author-defined.
		return map[string]any{}
	case reflect.String:
		return map[string]any{"type": "string"}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number"}
	case reflect.Slice, reflect.Array:
		return map[string]any{"type": "array", "items": schemaForType(t.Elem())}
	case reflect.Map:
		if t.Key().Kind() != reflect.String {
			panic(fmt.Sprintf("genschema: non-string map key %s", t.Key()))
		}
		return map[string]any{
			"type":                 "object",
			"additionalProperties": schemaForType(t.Elem()),
		}
	case reflect.Struct:
		return schemaForStruct(t)
	default:
		panic(fmt.Sprintf("genschema: unsupported kind %s", t.Kind()))
	}
}

func schemaForStruct(t reflect.Type) map[string]any {
	props := map[string]any{}
	required := []string{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name, omitempty := parseJSONTag(tag, f.Name)
		props[name] = schemaForType(f.Type)
		if !omitempty {
			required = append(required, name)
		}
	}
	sort.Strings(required)
	additional := any(false)
	if os, ok := reflect.New(t).Interface().(openSchema); ok && os.OpenSchema() {
		additional = true
	}
	out := map[string]any{
		"type":                 "object",
		"properties":           props,
		"additionalProperties": additional,
	}
	if len(required) > 0 {
		out["required"] = required
	}
	return out
}

func parseJSONTag(tag, fallback string) (string, bool) {
	if tag == "" {
		return fallback, false
	}
	parts := strings.Split(tag, ",")
	name := parts[0]
	if name == "" {
		name = fallback
	}
	omitempty := false
	for _, p := range parts[1:] {
		if p == "omitempty" {
			omitempty = true
		}
	}
	return name, omitempty
}

// marshalSorted emits JSON with object keys sorted lexicographically at every
// level, 2-space indent.
func marshalSorted(v any) ([]byte, error) {
	canonical, err := canonicalEncode(v)
	if err != nil {
		return nil, err
	}
	var pretty bytes.Buffer
	if err := json.Indent(&pretty, canonical, "", "  "); err != nil {
		return nil, err
	}
	return pretty.Bytes(), nil
}

func canonicalEncode(v any) ([]byte, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var decoded any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		return nil, err
	}
	return canonicalEmit(decoded)
}

func canonicalEmit(v any) ([]byte, error) {
	switch x := v.(type) {
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var buf bytes.Buffer
		buf.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, _ := json.Marshal(k)
			buf.Write(kb)
			buf.WriteByte(':')
			vb, err := canonicalEmit(x[k])
			if err != nil {
				return nil, err
			}
			buf.Write(vb)
		}
		buf.WriteByte('}')
		return buf.Bytes(), nil
	case []any:
		var buf bytes.Buffer
		buf.WriteByte('[')
		for i, item := range x {
			if i > 0 {
				buf.WriteByte(',')
			}
			ib, err := canonicalEmit(item)
			if err != nil {
				return nil, err
			}
			buf.Write(ib)
		}
		buf.WriteByte(']')
		return buf.Bytes(), nil
	default:
		return json.Marshal(x)
	}
}
