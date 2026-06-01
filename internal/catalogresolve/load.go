package catalogresolve

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/santhosh-tekuri/jsonschema/v5"
	"gopkg.in/yaml.v3"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// componentYAMLSchemaURL is the in-memory URL the embedded schema is
// compiled at. It does not have to be a real network URL — the
// santhosh-tekuri compiler only uses it for $ref resolution and error
// reporting, both of which we don't exercise here.
const componentYAMLSchemaURL = "memory://component-yaml.schema.json"

var (
	componentSchemaOnce sync.Once
	componentSchema     *jsonschema.Schema
	componentSchemaErr  error
)

// componentYAMLSchema lazily compiles the embedded component.yaml schema
// the first time the package needs it. Subsequent calls reuse the
// compiled instance — compilation is the only non-trivial cost in the
// load path.
func componentYAMLSchema() (*jsonschema.Schema, error) {
	componentSchemaOnce.Do(func() {
		c := jsonschema.NewCompiler()
		c.Draft = jsonschema.Draft7
		if err := c.AddResource(componentYAMLSchemaURL, strings.NewReader(string(catalogmodel.ComponentYAMLSchema))); err != nil {
			componentSchemaErr = &errInternal{Stage: "schema-compile", Err: fmt.Errorf("add embedded schema: %w", err)}
			return
		}
		s, err := c.Compile(componentYAMLSchemaURL)
		if err != nil {
			componentSchemaErr = &errInternal{Stage: "schema-compile", Err: fmt.Errorf("compile embedded schema: %w", err)}
			return
		}
		componentSchema = s
	})
	return componentSchema, componentSchemaErr
}

// loadAuthored reads workspace-relative path `rel` (resolved against
// workspaceRoot) and produces an AuthoredManifest with provenance pinned
// to that file. Schema-validation errors return *ErrManifestInvalid with
// the first failing JSON pointer.
//
// Inheritance is NOT applied here — that is the inherit.go responsibility.
func loadAuthored(workspaceRoot, rel string) (AuthoredManifest, error) {
	abs := filepath.Join(workspaceRoot, filepath.FromSlash(rel))
	raw, err := os.ReadFile(abs)
	if err != nil {
		return AuthoredManifest{}, &errInternal{Stage: "load-read", Err: err}
	}

	// Decode YAML → generic JSON-compatible interface{} for schema
	// validation. yaml.v3 returns nested map[string]interface{} by
	// default when keys are strings, which is what the validator wants;
	// we round-trip through JSON to guarantee fully-typed primitives
	// (e.g. yaml int64 → JSON float64) match draft-07 expectations.
	var generic any
	if err := yaml.Unmarshal(raw, &generic); err != nil {
		return AuthoredManifest{}, &ErrManifestInvalid{File: rel, Reason: fmt.Sprintf("yaml decode: %v", err)}
	}
	// nil generic only when the file is empty — the schema requires
	// `apiVersion`/`kind`/`metadata`/`spec`, so let validation produce
	// the canonical error message rather than special-casing here.
	jsonBytes, err := json.Marshal(generic)
	if err != nil {
		return AuthoredManifest{}, &errInternal{Stage: "load-yaml-to-json", Err: err}
	}
	var validatable any
	if err := json.Unmarshal(jsonBytes, &validatable); err != nil {
		return AuthoredManifest{}, &errInternal{Stage: "load-json-decode", Err: err}
	}

	schema, err := componentYAMLSchema()
	if err != nil {
		return AuthoredManifest{}, err
	}
	if validateErr := schema.Validate(validatable); validateErr != nil {
		return AuthoredManifest{}, manifestInvalidFrom(rel, validateErr)
	}

	// Schema-validated; now decode into the typed shape. The schema
	// guarantees this succeeds barring an exotic reflect mismatch — any
	// failure here is treated as an internal bug rather than an
	// authoring error.
	var typed catalogmodel.ComponentYAML
	if err := json.Unmarshal(jsonBytes, &typed); err != nil {
		return AuthoredManifest{}, &errInternal{Stage: "load-typed-decode", Err: err}
	}

	prov := authoredProvenance(rel, &typed)

	return AuthoredManifest{
		SourceFile:    rel,
		Component:     typed,
		Provenance:    prov,
		UnknownFields: unknownFields(validatable),
	}, nil
}

// manifestInvalidFrom maps a santhosh-tekuri validation error into a
// typed ErrManifestInvalid carrying the first failing JSON pointer.
func manifestInvalidFrom(rel string, validateErr error) error {
	out := &ErrManifestInvalid{File: rel, Reason: validateErr.Error()}
	var ve *jsonschema.ValidationError
	if errors.As(validateErr, &ve) {
		// Walk to the deepest leaf cause to produce the most specific
		// pointer ("/spec/type" rather than "").
		leaf := deepestCause(ve)
		if leaf != nil {
			out.Pointer = leaf.InstanceLocation
			out.Reason = strings.TrimSpace(leaf.Message)
			if out.Reason == "" {
				out.Reason = validateErr.Error()
			}
		}
	}
	return out
}

func deepestCause(ve *jsonschema.ValidationError) *jsonschema.ValidationError {
	if ve == nil {
		return nil
	}
	if len(ve.Causes) == 0 {
		return ve
	}
	// Pick the first cause with a non-empty message — santhosh-tekuri
	// already orders causes by location depth.
	for _, c := range ve.Causes {
		if leaf := deepestCause(c); leaf != nil {
			return leaf
		}
	}
	return ve
}

// authoredProvenance walks the typed manifest and emits a Provenance
// entry for every authored field that the resolver later attributes.
// Pointers follow RFC 6901; the leading slash form is used.
func authoredProvenance(file string, c *catalogmodel.ComponentYAML) map[string]Provenance {
	out := map[string]Provenance{}
	put := func(field, ptr string) {
		out[field] = Provenance{File: file, Pointer: ptr}
	}
	if c.APIVersion != "" {
		put("apiVersion", "/apiVersion")
	}
	if c.Kind != "" {
		put("kind", "/kind")
	}
	put("metadata.name", "/metadata/name")
	if c.Metadata.Title != "" {
		put("metadata.title", "/metadata/title")
	}
	if c.Metadata.Description != "" {
		put("metadata.description", "/metadata/description")
	}
	for k := range c.Metadata.Labels {
		put("metadata.labels."+k, "/metadata/labels/"+escapeJSONPointerToken(k))
	}
	for k := range c.Metadata.Annotations {
		put("metadata.annotations."+k, "/metadata/annotations/"+escapeJSONPointerToken(k))
	}
	if c.Spec.Type != "" {
		put("spec.type", "/spec/type")
	}
	if c.Spec.Lifecycle != "" {
		put("spec.lifecycle", "/spec/lifecycle")
	}
	if c.Spec.Owner != "" {
		put("spec.owner", "/spec/owner")
	}
	if c.Spec.System != "" {
		put("spec.system", "/spec/system")
	}
	if c.Spec.Domain != "" {
		put("spec.domain", "/spec/domain")
	}
	if c.Spec.Path != "" {
		put("spec.path", "/spec/path")
	}
	// spec.labels fold into the resolved metadata.labels map. Record each
	// key under its resolved field path; metadata.labels keys (emitted
	// above) take precedence and overwrite the entry when both are set.
	for k := range c.Spec.Labels {
		if _, ok := c.Metadata.Labels[k]; ok {
			continue
		}
		put("metadata.labels."+k, "/spec/labels/"+escapeJSONPointerToken(k))
	}
	for k := range c.Spec.Parameters {
		put("spec.parameters."+k, "/spec/parameters/"+escapeJSONPointerToken(k))
	}
	for k := range c.Spec.Env {
		put("spec.env."+k, "/spec/env/"+escapeJSONPointerToken(k))
	}
	if c.Spec.Subscribe != nil {
		for i, e := range c.Spec.Subscribe.Environments {
			if e.Name == "" {
				continue
			}
			put("spec.subscribe.environments."+e.Name,
				fmt.Sprintf("/spec/subscribe/environments/%d", i))
		}
	}
	if c.Spec.DependsOn != nil {
		put("spec.dependsOn", "/spec/dependsOn")
	}
	if c.Spec.ProvidesAPIs != nil {
		put("spec.providesApis", "/spec/providesApis")
	}
	if c.Spec.ConsumesAPIs != nil {
		put("spec.consumesApis", "/spec/consumesApis")
	}
	for envName := range c.Spec.Environments {
		put("spec.environments."+envName,
			"/spec/environments/"+escapeJSONPointerToken(envName))
	}
	return out
}

// escapeJSONPointerToken escapes per RFC 6901 §3.
func escapeJSONPointerToken(s string) string {
	s = strings.ReplaceAll(s, "~", "~0")
	s = strings.ReplaceAll(s, "/", "~1")
	return s
}
