package catalogresolve

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
	"sync"

	"github.com/sourceplane/orun/internal/catalogmodel"
)

// unknownFields walks a raw, schema-validated authored manifest and returns
// RFC 6901 pointers to keys the catalog does not model. The authoring schema
// is intentionally open (additionalProperties: true) so these keys validate;
// reporting them keeps the tolerance from silently hiding author typos or
// quietly-dropped legacy fields.
//
// Linting is bounded to the structured levels an author edits by hand and
// where the known vocabulary is closed: the document root, `metadata`, `spec`,
// and each object entry of `spec.subscribe.environments`. Free-form maps
// (`spec.parameters`, `spec.labels`, `spec.env`, and per-environment maps) and
// the `spec.dependsOn` list are not linted — their keys or values are
// author-defined or span a wider plan-engine vocabulary than the catalog
// models, so flagging them would produce false positives.
//
// `raw` is the manifest as decoded for schema validation (yaml → JSON-shaped
// any). A non-map root yields no findings — the schema has already rejected
// that case before this runs.
func unknownFields(raw any) []string {
	root, ok := raw.(map[string]any)
	if !ok {
		return nil
	}
	var out []string
	out = appendUnknown(out, root, knownKeys(reflect.TypeOf(catalogmodel.ComponentYAML{})), "")

	if meta, ok := root["metadata"].(map[string]any); ok {
		out = appendUnknown(out, meta, knownKeys(reflect.TypeOf(catalogmodel.ComponentYAMLMetadata{})), "/metadata")
	}
	if spec, ok := root["spec"].(map[string]any); ok {
		out = appendUnknown(out, spec, knownKeys(reflect.TypeOf(catalogmodel.ComponentYAMLSpec{})), "/spec")
		out = append(out, unknownSubscribeFields(spec)...)
	}

	sort.Strings(out)
	return out
}

// unknownSubscribeFields lints each object entry of spec.subscribe.environments
// against the known per-environment vocabulary. Bare-string entries (the
// shorthand form) carry no keys and are skipped.
func unknownSubscribeFields(spec map[string]any) []string {
	sub, ok := spec["subscribe"].(map[string]any)
	if !ok {
		return nil
	}
	envs, ok := sub["environments"].([]any)
	if !ok {
		return nil
	}
	known := knownKeys(reflect.TypeOf(catalogmodel.ComponentYAMLSubscribeEnvironment{}))
	var out []string
	for i, e := range envs {
		obj, ok := e.(map[string]any)
		if !ok {
			continue
		}
		base := fmt.Sprintf("/spec/subscribe/environments/%d", i)
		out = appendUnknown(out, obj, known, base)
	}
	return out
}

// appendUnknown adds a pointer for every key in `m` absent from `known`.
func appendUnknown(out []string, m map[string]any, known map[string]bool, base string) []string {
	for k := range m {
		if known[k] {
			continue
		}
		out = append(out, base+"/"+escapeJSONPointerToken(k))
	}
	return out
}

// knownKeys returns the set of JSON object keys a struct type declares,
// derived from its `json` tags (so the lint vocabulary tracks the schema,
// which is generated from the same tags). Unexported and json:"-" fields are
// excluded. Results are cached per type.
func knownKeys(t reflect.Type) map[string]bool {
	if v, ok := knownKeysCache.Load(t); ok {
		return v.(map[string]bool)
	}
	out := map[string]bool{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		if tag == "-" {
			continue
		}
		name := strings.Split(tag, ",")[0]
		if name == "" {
			name = f.Name
		}
		out[name] = true
	}
	knownKeysCache.Store(t, out)
	return out
}

var knownKeysCache sync.Map
